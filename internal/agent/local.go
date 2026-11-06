package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/Elian-D/zertixpos-print-agent/internal/escpos"
	"github.com/Elian-D/zertixpos-print-agent/internal/printer"
)

// LocalAddr es donde el servicio atiende a la bandeja. Solo 127.0.0.1: nada de afuera lo ve.
const LocalAddr = "127.0.0.1:17890"

// ServeLocal expone el estado y "Imprimir prueba" para la bandeja.
//
//	GET  /status            el estado en JSON
//	POST /test?printer=ID   imprime el ticket de prueba local en esa impresora
//	POST /scan              busca impresoras ya
func (a *Agent) ServeLocal(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.Status())
	})
	mux.HandleFunc("POST /test", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.URL.Query().Get("printer"), 10, 64)
		p, ok := a.Printer(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Esa impresora no está en este equipo."})
			return
		}
		address := p.WindowsName
		if p.Type == "network" {
			address = net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
		}
		data := escpos.TestTicket(escpos.Options{
			PrinterName: p.Name,
			Address:     address,
			AgentName:   a.Status().AgentName,
			Version:     a.Version,
		})
		if err := printer.Print(printer.Target{Name: p.Name, Type: p.Type, Host: p.Host, Port: p.Port, WindowsName: p.WindowsName}, data); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "Prueba enviada a " + p.Name})
	})
	mux.HandleFunc("POST /scan", func(w http.ResponseWriter, r *http.Request) {
		a.ScanNow()
		writeJSON(w, http.StatusAccepted, map[string]string{"message": "Buscando impresoras…"})
	})

	srv := &http.Server{Addr: LocalAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// LocalStatus lo usa la bandeja (y el comando `status`) para preguntarle al servicio.
func LocalStatus(ctx context.Context) (*Status, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+LocalAddr+"/status", nil)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var s Status
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// LocalPost manda una orden al servicio y devuelve su mensaje.
func LocalPost(ctx context.Context, path string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+LocalAddr+path, nil)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		return "", errors.New(out.Message)
	}
	return out.Message, nil
}
