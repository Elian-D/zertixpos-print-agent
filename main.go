// zertixpos-agent: el agente de impresión de ZertixPOS. Se instala en una PC del negocio, queda
// vinculado con un código de 6 dígitos y manda los tickets a las impresoras térmicas (de red por
// IP:9100 o USB por el spooler de Windows), sin pasar por el navegador.
//
// Ver README.md para compilar, instalar y probar.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Elian-D/zertixpos-print-agent/internal/agent"
	"github.com/Elian-D/zertixpos-print-agent/internal/config"
	"github.com/Elian-D/zertixpos-print-agent/internal/console"
	"github.com/Elian-D/zertixpos-print-agent/internal/discovery"
	"github.com/Elian-D/zertixpos-print-agent/internal/escpos"
	"github.com/Elian-D/zertixpos-print-agent/internal/install"
	"github.com/Elian-D/zertixpos-print-agent/internal/printer"
	"github.com/Elian-D/zertixpos-print-agent/internal/tray"
	"github.com/Elian-D/zertixpos-print-agent/internal/winsvc"
)

// version la pone el build: -ldflags "-X main.version=0.1.0".
var version = "dev"

func main() {
	// El administrador de servicios de Windows arranca el .exe con "service".
	if winsvc.IsService() {
		runService()
		return
	}

	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "service":
		runService()
	case "tray":
		if tray.AlreadyRunning() {
			return
		}
		tray.Run(version)
	case "":
		// Doble clic: instalado → la bandeja (o, sin vincular, pide el código); descargado → el
		// instalador, que también actualiza uno ya instalado y conserva su vinculación.
		if runtime.GOOS == "windows" && install.Installed() {
			if cfg, _ := config.Load(); !cfg.Paired() {
				withConsole(func() error { return cmdPair(nil) })
				return
			}
			if !tray.AlreadyRunning() {
				tray.Run(version)
			}
			return
		}
		withConsole(func() error { return cmdInstall(nil) })
	default:
		withConsole(func() error { return dispatch(cmd, args) })
	}
}

func dispatch(cmd string, args []string) error {
	switch cmd {
	case "install":
		return cmdInstall(args)
	case "uninstall":
		return cmdUninstall(args)
	case "pair":
		return cmdPair(args)
	case "run":
		return cmdRun(args)
	case "status":
		return cmdStatus()
	case "test-print":
		return cmdTestPrint(args)
	case "scan":
		return cmdScan()
	case "version", "--version", "-v":
		fmt.Println("zertixpos-agent", version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("comando desconocido: %s", cmd)
	}
}

func withConsole(fn func() error) {
	console.Open("ZertixPOS - Agente de impresión")
	err := fn()
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nError:", err)
	}
	console.Pause()
	if err != nil {
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`zertixpos-agent ` + version + ` — agente de impresión de ZertixPOS

Uso:
  zertixpos-agent                    Doble clic: instala o actualiza; ya instalado abre el ícono
                                     (o pide el código si quedó sin vincular)
  zertixpos-agent install [--server URL]
                                     Copia el agente, pide el código y registra el servicio
  zertixpos-agent uninstall [--purge]
                                     Quita el servicio y el ícono (--purge borra la vinculación)
  zertixpos-agent pair [CÓDIGO] [--server URL]
                                     Vincula con el código de 6 dígitos de ZertixPOS
  zertixpos-agent run [--server URL] Corre en primer plano (desarrollo, Linux)
  zertixpos-agent status             Estado del servicio
  zertixpos-agent test-print --host IP [--port 9100] [--width 80] [--qr URL] [--drawer]
  zertixpos-agent test-print --usb "Nombre en Windows" [--width 80]
                                     Imprime un ticket de prueba sin pasar por ZertixPOS
  zertixpos-agent scan               Busca impresoras en la red (puerto 9100) y en Windows
  zertixpos-agent version

--server es el dominio central (por defecto https://zertixpos.com). En Sail: http://localhost:8082
`)
}

// ===== Instalar y vincular =====

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	server := fs.String("server", "", "dominio central de ZertixPOS")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return errors.New("instalar es solo para Windows: en Linux usa `pair` y `run`")
	}
	if !install.IsAdmin() {
		fmt.Println("Hace falta permiso de administrador: Windows te lo va a pedir.")
		elevated := append([]string{"install"}, args...)
		if err := install.Elevate(elevated); err != nil {
			return fmt.Errorf("no se pudo pedir el permiso de administrador: %w", err)
		}
		os.Exit(0)
	}
	return install.Run(context.Background(), centralURL(*server), version)
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "borrar también la vinculación")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if runtime.GOOS == "windows" && !install.IsAdmin() {
		return install.Elevate(append([]string{"uninstall"}, args...))
	}
	if err := install.Uninstall(*purge); err != nil {
		return err
	}
	fmt.Println("Agente desinstalado.")
	return nil
}

func cmdPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	server := fs.String("server", "", "dominio central de ZertixPOS")
	if err := fs.Parse(reorder(args)); err != nil {
		return err
	}
	// En Windows la vinculación vive en ProgramData, que solo el administrador puede escribir.
	if runtime.GOOS == "windows" && !install.IsAdmin() {
		fmt.Println("Hace falta permiso de administrador: Windows te lo va a pedir.")
		if err := install.Elevate(append([]string{"pair"}, args...)); err != nil {
			return fmt.Errorf("no se pudo pedir el permiso de administrador: %w", err)
		}
		os.Exit(0)
	}
	code := fs.Arg(0)
	if code == "" {
		fmt.Println("En ZertixPOS: Configuración → Impresoras → Agregar equipo (o Volver a vincular).")
		code = console.Ask("Código de 6 dígitos: ")
	}
	cfg, err := install.Pair(context.Background(), centralURL(*server), code, version)
	if err != nil {
		return err
	}
	fmt.Printf("Vinculado a %s (%s) como \"%s\".\n", cfg.Business, cfg.ServerURL, cfg.AgentName)
	fmt.Println("Vinculación guardada en", config.Path())
	if winsvc.Installed() {
		fmt.Println("El servicio la toma solo en unos segundos.")
	}
	return nil
}

// centralURL: --server, o el de la vinculación guardada, o zertixpos.com.
func centralURL(flagValue string) string {
	if flagValue != "" {
		return strings.TrimRight(flagValue, "/")
	}
	if cfg, err := config.Load(); err == nil && cfg.CentralURL != "" {
		return cfg.CentralURL
	}
	return config.DefaultServer
}

// ===== Correr =====

func runService() {
	closeLog := setupLog(false)
	defer closeLog()
	// La versión en "Aplicaciones instaladas", al día después de una actualización automática.
	if install.Installed() {
		install.RegisterUninstall(version)
	}
	err := winsvc.Run(func(ctx context.Context) error {
		return runAgent(ctx, true)
	})
	if err != nil {
		log.Printf("servicio: %v", err)
	}
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	server := fs.String("server", "", "dominio central (solo si todavía no está vinculado)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	closeLog := setupLog(true)
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg, _ := config.Load(); !cfg.Paired() {
		code := console.Ask("Sin vincular. Código de 6 dígitos: ")
		if _, err := install.Pair(ctx, centralURL(*server), code, version); err != nil {
			return err
		}
	}

	fmt.Println("Agente en marcha. Ctrl+C para salir.")
	err := runAgent(ctx, false)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// runAgent corre el agente y su estado local. Si el servidor lo desvincula, vuelve a esperar
// la vinculación (el agente se reinicia por dentro), así el servicio nunca se detiene solo.
func runAgent(ctx context.Context, asService bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	restart := make(chan struct{}, 1)
	a := agent.New(version)
	a.AutoUpdate = asService && runtime.GOOS == "windows"
	a.Restart = func() {
		restart <- struct{}{}
		cancel()
	}

	// El estado para la bandeja (127.0.0.1:17890). Si el puerto está ocupado, se reintenta.
	go func() {
		for ctx.Err() == nil {
			if err := a.ServeLocal(ctx); err != nil {
				log.Printf("estado local: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()

	for ctx.Err() == nil {
		err := a.Run(ctx)
		select {
		case <-restart:
			// Actualizado: salir con error para que Windows arranque el .exe nuevo.
			return errors.New("actualizado: reinicio")
		default:
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("el agente se detuvo (%v): vuelve a esperar la vinculación", err)
		time.Sleep(2 * time.Second)
	}
	return ctx.Err()
}

// setupLog escribe en agent.log (y en pantalla con `run`). Rota a agent.log.1 al pasar 5 MB.
func setupLog(alsoStdout bool) func() {
	_ = os.MkdirAll(config.Dir(), 0o755)
	path := filepath.Join(config.Dir(), "agent.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return func() {}
	}
	var w io.Writer = f
	if alsoStdout {
		w = io.MultiWriter(f, os.Stdout)
	}
	log.SetOutput(w)
	log.SetFlags(log.LstdFlags)
	return func() { f.Close() }
}

// ===== Diagnóstico =====

func cmdStatus() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	st, err := agent.LocalStatus(ctx)
	if err != nil {
		cfg, _ := config.Load()
		fmt.Println("El agente no está corriendo en esta PC.")
		if cfg.Paired() {
			fmt.Printf("Vinculado a %s (%s) como \"%s\".\n", cfg.Business, cfg.ServerURL, cfg.AgentName)
		} else {
			fmt.Println("Sin vincular.")
		}
		return nil
	}

	yes := func(b bool) string {
		if b {
			return "sí"
		}
		return "no"
	}
	fmt.Println("Versión:          ", st.Version)
	fmt.Println("Vinculado:        ", yes(st.Paired))
	if st.Paired {
		fmt.Printf("Negocio:           %s (%s)\n", st.Business, st.ServerURL)
		fmt.Println("Equipo:           ", st.AgentName)
	}
	fmt.Println("Conectado:        ", yes(st.Online))
	fmt.Println("Aviso instantáneo:", yes(st.Doorbell))
	if !st.LastHeartbeat.IsZero() {
		fmt.Println("Último aviso:     ", st.LastHeartbeat.Format("15:04:05"))
	}
	if st.LastPrint != "" {
		fmt.Println("Último ticket:    ", st.LastPrint)
	}
	if st.LastError != "" {
		fmt.Println("Último error:     ", st.LastError)
	}
	fmt.Println("Impresoras:")
	if len(st.Printers) == 0 {
		fmt.Println("  (ninguna: agrégalas en Configuración → Impresoras)")
	}
	for _, p := range st.Printers {
		where := p.WindowsName
		if p.Type == "network" {
			where = fmt.Sprintf("%s:%d", p.Host, p.Port)
		}
		fmt.Printf("  #%d %s — %s\n", p.ID, p.Name, where)
	}
	return nil
}

func cmdTestPrint(args []string) error {
	fs := flag.NewFlagSet("test-print", flag.ContinueOnError)
	host := fs.String("host", "", "IP de la impresora de red")
	port := fs.Int("port", 9100, "puerto")
	usb := fs.String("usb", "", "nombre de la impresora en Windows")
	width := fs.Int("width", 80, "ancho del papel: 80 o 58")
	qr := fs.String("qr", "", "texto o URL para un QR de prueba")
	drawer := fs.Bool("drawer", false, "abrir la gaveta")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host == "" && *usb == "" {
		return errors.New("falta --host IP o --usb \"Nombre en Windows\"")
	}

	cols := 48
	if *width == 58 {
		cols = 32
	}
	target := printer.Target{Name: "Prueba", Type: "network", Host: *host, Port: *port}
	address := fmt.Sprintf("%s:%d", *host, *port)
	if *usb != "" {
		target = printer.Target{Name: *usb, Type: "usb", WindowsName: *usb}
		address = "USB · " + *usb
	}

	data := escpos.TestTicket(escpos.Options{
		PrinterName: target.Name,
		Address:     address,
		Version:     version,
		Columns:     cols,
		QR:          *qr,
		OpenDrawer:  *drawer,
	})
	fmt.Printf("Mandando %d bytes a %s…\n", len(data), address)
	if err := printer.Print(target, data); err != nil {
		return err
	}
	fmt.Println("Listo: la impresora aceptó el ticket.")
	return nil
}

func cmdScan() error {
	fmt.Println("Buscando impresoras con el puerto 9100 abierto…")
	for _, n := range discovery.Subnets() {
		fmt.Println("  red", n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	found := discovery.ScanNetwork(ctx)
	if len(found) == 0 {
		fmt.Println("Ninguna en la red.")
	}
	for _, f := range found {
		mac := f.MAC
		if mac == "" {
			mac = "MAC desconocida"
		}
		fmt.Printf("  %s:%d  (%s)\n", f.Host, f.Port, mac)
	}
	if runtime.GOOS == "windows" {
		fmt.Println("Impresoras de Windows:")
		for _, f := range discovery.Windows() {
			fmt.Println("  ", f.WindowsName)
		}
	}
	return nil
}

// reorder deja las banderas antes de los argumentos sueltos (`pair 123456 --server x`).
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}
