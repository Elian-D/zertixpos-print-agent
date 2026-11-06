// Package agent es el agente en marcha: avisa que está vivo cada 30 s, escucha el timbre de Reverb,
// toma los trabajos por REST, los manda a la impresora y confirma. Si el WebSocket se corta, el
// aviso de vida trae cuántos trabajos esperan y los imprime igual: un trabajo nunca se pierde.
package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Elian-D/zertixpos-print-agent/internal/api"
	"github.com/Elian-D/zertixpos-print-agent/internal/config"
	"github.com/Elian-D/zertixpos-print-agent/internal/discovery"
	"github.com/Elian-D/zertixpos-print-agent/internal/printer"
	"github.com/Elian-D/zertixpos-print-agent/internal/pusher"
	"github.com/Elian-D/zertixpos-print-agent/internal/update"
)

const (
	heartbeatEvery = 30 * time.Second
	discoverEvery  = 30 * time.Minute
	updateEvery    = 6 * time.Hour
)

// Agent es el proceso que imprime.
type Agent struct {
	Version string
	// AutoUpdate: solo el servicio de Windows se reemplaza a sí mismo.
	AutoUpdate bool
	// Restart lo llama la actualización para que el servicio arranque con el .exe nuevo.
	Restart func()

	mu       sync.RWMutex
	status   Status
	cfg      *config.Config
	client   *api.Client
	printers map[int64]api.Printer

	jobsMu   sync.Mutex // un solo recorrido de pendientes a la vez
	wake     chan struct{}
	scanNow  chan struct{}
	hostname string
}

// Status es lo que muestra la bandeja (y `status`).
type Status struct {
	Version       string        `json:"version"`
	Paired        bool          `json:"paired"`
	Business      string        `json:"business"`
	AgentName     string        `json:"agent_name"`
	ServerURL     string        `json:"server_url"`
	Online        bool          `json:"online"`         // el último aviso de vida respondió
	Doorbell      bool          `json:"doorbell"`       // el WebSocket está suscrito
	LastHeartbeat time.Time     `json:"last_heartbeat"` // cero si nunca
	LastError     string        `json:"last_error"`
	LastPrint     string        `json:"last_print"`
	Printers      []api.Printer `json:"printers"`
	Latest        string        `json:"latest_version"`
}

// New arma el agente.
func New(version string) *Agent {
	host, _ := os.Hostname()
	return &Agent{
		Version:  version,
		wake:     make(chan struct{}, 1),
		scanNow:  make(chan struct{}, 1),
		hostname: host,
		printers: map[int64]api.Printer{},
	}
}

// Status devuelve una copia del estado.
func (a *Agent) Status() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := a.status
	s.Version = a.Version
	s.Printers = make([]api.Printer, 0, len(a.printers))
	for _, p := range a.printers {
		s.Printers = append(s.Printers, p)
	}
	return s
}

// Printer devuelve una impresora conocida por su id.
func (a *Agent) Printer(id int64) (api.Printer, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.printers[id]
	return p, ok
}

// Wake pide recorrer los pendientes ya.
func (a *Agent) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// ScanNow pide buscar impresoras ya.
func (a *Agent) ScanNow() {
	select {
	case a.scanNow <- struct{}{}:
	default:
	}
}

func (a *Agent) setError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err == nil {
		a.status.LastError = ""
		return
	}
	a.status.LastError = err.Error()
}

// Run corre hasta que se cancela el contexto. Sin vinculación, espera a que aparezca agent.json
// (el comando `pair` lo escribe y el servicio lo toma solo).
func (a *Agent) Run(ctx context.Context) error {
	log.Printf("agente %s arrancando, datos en %s", a.Version, config.Dir())

	for {
		cfg, err := config.Load()
		if err != nil {
			log.Printf("no se pudo leer la vinculación: %v", err)
		}
		if cfg != nil && cfg.Paired() {
			a.mu.Lock()
			a.cfg = cfg
			a.client = api.New(cfg.ServerURL, cfg.Token, a.Version)
			a.status.Paired = true
			a.status.Business = cfg.Business
			a.status.AgentName = cfg.AgentName
			a.status.ServerURL = cfg.ServerURL
			a.mu.Unlock()
			break
		}
		a.mu.Lock()
		a.status.Paired = false
		a.status.LastError = "Sin vincular: abre el agente y escribe el código de 6 dígitos."
		a.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}

	log.Printf("vinculado a %s (%s) como %q", a.cfg.Business, a.cfg.ServerURL, a.cfg.AgentName)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); a.heartbeatLoop(ctx, cancel) }()
	go func() { defer wg.Done(); a.doorbellLoop(ctx) }()
	go func() { defer wg.Done(); a.discoveryLoop(ctx) }()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case <-a.wake:
			a.processPending(ctx)
		}
	}
}

// ===== Aviso de vida =====

func (a *Agent) heartbeatLoop(ctx context.Context, cancel context.CancelFunc) {
	var lastUpdateCheck time.Time
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()

	for {
		hb, err := a.client.Heartbeat(ctx, a.hostname)
		if errors.Is(err, api.ErrUnauthorized) {
			log.Printf("el servidor rechazó el token: el equipo se desvinculó")
			cfg, _ := config.Load()
			_ = config.Clear(cfg)
			a.mu.Lock()
			a.status.Paired = false
			a.status.Online = false
			a.status.LastError = api.ErrUnauthorized.Error()
			a.mu.Unlock()
			cancel()
			return
		}

		a.mu.Lock()
		if err != nil {
			a.status.Online = false
			a.status.LastError = err.Error()
		} else {
			a.status.Online = true
			a.status.LastHeartbeat = time.Now()
			a.status.AgentName = hb.Agent.Name
			a.status.Latest = hb.Release.Version
			if a.status.LastError != "" && !a.status.Doorbell {
				a.status.LastError = ""
			}
			a.printers = map[int64]api.Printer{}
			for _, p := range hb.Printers {
				a.printers[p.ID] = p
			}
			// El servidor puede cambiar la dirección del WebSocket (por ejemplo, otro dominio).
			if hb.WebSocket.URL != "" && hb.WebSocket != (api.WebSocket{URL: a.cfg.WebSocket.URL, Key: a.cfg.WebSocket.Key, Channel: a.cfg.WebSocket.Channel}) {
				a.cfg.WebSocket = config.WebSocket{URL: hb.WebSocket.URL, Key: hb.WebSocket.Key, Channel: hb.WebSocket.Channel}
				_ = config.Save(a.cfg)
			}
		}
		a.mu.Unlock()

		if err == nil && hb.Pending > 0 {
			a.Wake()
		}

		if err == nil && a.AutoUpdate && time.Since(lastUpdateCheck) > updateEvery && update.Newer(hb.Release.Version, a.Version) {
			lastUpdateCheck = time.Now()
			log.Printf("hay una versión nueva (%s), actualizando", hb.Release.Version)
			if uerr := update.Apply(ctx, hb.Release.URL); uerr != nil {
				log.Printf("no se pudo actualizar: %v", uerr)
			} else if a.Restart != nil {
				log.Printf("actualizado a %s, reiniciando", hb.Release.Version)
				a.Restart()
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ===== Timbre =====

func (a *Agent) doorbellLoop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		a.mu.RLock()
		ws := a.cfg.WebSocket
		a.mu.RUnlock()

		started := time.Now()
		err := pusher.Run(ctx, pusher.Options{
			URL:         ws.URL,
			Key:         ws.Key,
			Channel:     ws.Channel,
			Version:     a.Version,
			Auth:        a.client.Auth,
			RewriteAddr: api.LocalhostAddr,
		}, func() {
			log.Printf("timbre conectado (%s)", ws.URL)
			a.mu.Lock()
			a.status.Doorbell = true
			a.mu.Unlock()
			backoff = time.Second
			a.Wake() // al reconectar, lo que llegó mientras tanto
		}, func(event string, _ []byte) {
			if event == "print.job" {
				a.Wake()
			}
		})

		a.mu.Lock()
		a.status.Doorbell = false
		a.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, api.ErrUnauthorized) {
			return
		}
		log.Printf("timbre: %v (reintento en %s)", err, backoff)

		// Una conexión que duró bastante no cuenta como fallo seguido.
		if time.Since(started) > 5*time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

// ===== Trabajos =====

func (a *Agent) processPending(ctx context.Context) {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()

	ids, err := a.client.Pending(ctx)
	if err != nil {
		log.Printf("no se pudieron pedir los pendientes: %v", err)
		a.setError(err)
		return
	}

	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		job, err := a.client.Claim(ctx, id)
		if errors.Is(err, api.ErrNotPending) {
			continue
		}
		if err != nil {
			log.Printf("trabajo %d: no se pudo tomar: %v", id, err)
			continue
		}

		perr := a.print(ctx, job)
		msg := ""
		if perr != nil {
			msg = perr.Error()
			log.Printf("trabajo %d (%s) en %q: falló: %v", job.ID, job.Type, job.Printer.Name, perr)
		} else {
			log.Printf("trabajo %d (%s) impreso en %q", job.ID, job.Type, job.Printer.Name)
			a.mu.Lock()
			a.status.LastPrint = time.Now().Format("15:04:05") + " · " + job.Printer.Name
			a.mu.Unlock()
		}
		a.setError(perr)

		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := a.client.Complete(cctx, job.ID, perr == nil, msg); err != nil {
			log.Printf("trabajo %d: no se pudo confirmar: %v", job.ID, err)
		}
		cancel()
	}
}

func (a *Agent) print(ctx context.Context, job *api.Job) error {
	data, err := base64.StdEncoding.DecodeString(job.Content)
	if err != nil {
		return errors.New("el contenido del trabajo no es base64")
	}
	if len(data) == 0 {
		return errors.New("el trabajo llegó vacío")
	}

	p := job.Printer
	err = printer.Print(printer.Target{Name: p.Name, Type: p.Type, Host: p.Host, Port: p.Port, WindowsName: p.WindowsName}, data)
	if err == nil || p.Type != "network" || p.MAC == "" {
		return err
	}

	// No responde en su IP: puede que el router se la haya cambiado. Se busca por la MAC.
	log.Printf("la impresora %q no responde en %s, buscándola por su MAC %s", p.Name, p.Host, p.MAC)
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if host := discovery.FindByMAC(sctx, p.MAC); host != "" && host != p.Host {
		log.Printf("la impresora %q ahora está en %s", p.Name, host)
		if rerr := printer.Network(host, p.Port, data); rerr == nil {
			// El servidor actualiza la IP al recibir lo encontrado (misma MAC).
			go a.reportDiscovered(context.Background())
			return nil
		}
	}
	return err
}

// ===== Búsqueda =====

func (a *Agent) discoveryLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}
	t := time.NewTicker(discoverEvery)
	defer t.Stop()
	for {
		a.reportDiscovered(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.scanNow:
		}
	}
}

func (a *Agent) reportDiscovered(ctx context.Context) {
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	found := append(discovery.ScanNetwork(sctx), discovery.Windows()...)
	if err := a.client.Discovered(sctx, found); err != nil {
		log.Printf("no se pudo informar lo encontrado: %v", err)
		return
	}
	log.Printf("búsqueda: %d impresoras encontradas", len(found))
}
