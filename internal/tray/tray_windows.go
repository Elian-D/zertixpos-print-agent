//go:build windows

// Package tray es el ícono junto al reloj. No imprime nada por sí mismo: le pregunta el estado al
// servicio (127.0.0.1:17890) cada pocos segundos y le pide "Imprimir prueba" o "Buscar impresoras".
package tray

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"

	"github.com/Elian-D/zertixpos-print-agent/internal/agent"
	"github.com/Elian-D/zertixpos-print-agent/internal/config"
	"github.com/Elian-D/zertixpos-print-agent/internal/icon"
	"github.com/Elian-D/zertixpos-print-agent/internal/install"
)

const maxPrinters = 10

// Run muestra el ícono hasta que se elige "Salir" (solo cierra el ícono; el servicio sigue).
func Run(version string) {
	systray.Run(func() { onReady(version) }, func() {})
}

func onReady(version string) {
	systray.SetIcon(icon.ICO(icon.Gray))
	systray.SetTitle("ZertixPOS")
	systray.SetTooltip("ZertixPOS - Agente de impresión")

	mStatus := systray.AddMenuItem("Conectando…", "")
	mStatus.Disable()
	mBusiness := systray.AddMenuItem("", "")
	mBusiness.Disable()
	mBusiness.Hide()
	mLast := systray.AddMenuItem("", "")
	mLast.Disable()
	mLast.Hide()
	systray.AddSeparator()

	mTest := systray.AddMenuItem("Imprimir prueba", "Manda un ticket de prueba a una impresora")
	slots := make([]*systray.MenuItem, maxPrinters)
	ids := make([]int64, maxPrinters)
	for i := range slots {
		slots[i] = mTest.AddSubMenuItem("", "")
		slots[i].Hide()
	}
	mNone := mTest.AddSubMenuItem("Sin impresoras: agrégalas en ZertixPOS", "")
	mNone.Disable()

	// Sin vincular (se desvinculó, se borró el equipo o se restableció el negocio): pide el código
	// en una ventana con permiso de administrador, sin comandos.
	mPair := systray.AddMenuItem("Vincular con un código…", "Pide el código en Configuración → Impresoras de ZertixPOS")
	mPair.Hide()

	mScan := systray.AddMenuItem("Buscar impresoras en la red", "La lista aparece en Configuración → Impresoras")
	mOpen := systray.AddMenuItem("Abrir ZertixPOS", "")
	mLog := systray.AddMenuItem("Ver registro", "")
	systray.AddSeparator()
	mVersion := systray.AddMenuItem("Versión "+version, "")
	mVersion.Disable()
	mQuit := systray.AddMenuItem("Cerrar este ícono", "El agente sigue imprimiendo en segundo plano")

	var mu sync.Mutex
	serverURL := ""

	refresh := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		st, err := agent.LocalStatus(ctx)

		mu.Lock()
		defer mu.Unlock()

		switch {
		case err != nil:
			systray.SetIcon(icon.ICO(icon.Red))
			mStatus.SetTitle("El servicio no está corriendo")
			systray.SetTooltip("ZertixPOS: el servicio del agente no está corriendo")
			mBusiness.Hide()
			mLast.Hide()
			mPair.Hide()
			for _, s := range slots {
				s.Hide()
			}
			return
		case !st.Paired:
			systray.SetIcon(icon.ICO(icon.Gray))
			mStatus.SetTitle("Sin vincular")
			systray.SetTooltip("ZertixPOS: sin vincular. Clic en \"Vincular con un código…\"")
		case st.Online && st.Doorbell:
			systray.SetIcon(icon.ICO(icon.Green))
			mStatus.SetTitle("Conectado")
			systray.SetTooltip("ZertixPOS: conectado a " + st.Business)
		case st.Online:
			systray.SetIcon(icon.ICO(icon.Amber))
			mStatus.SetTitle("Conectado (sin aviso instantáneo)")
			systray.SetTooltip("ZertixPOS: conectado, el timbre se está reconectando")
		default:
			systray.SetIcon(icon.ICO(icon.Gray))
			title := "Sin conexión con ZertixPOS"
			if st.LastError != "" {
				title = "Sin conexión: " + short(st.LastError, 60)
			}
			mStatus.SetTitle(title)
			systray.SetTooltip("ZertixPOS: sin conexión")
		}

		if st.Paired {
			mPair.Hide()
		} else {
			mPair.Show()
			mBusiness.Hide()
		}

		serverURL = st.ServerURL
		if st.Paired && st.Business != "" {
			mBusiness.SetTitle(fmt.Sprintf("%s · %s", st.Business, st.AgentName))
			mBusiness.Show()
		}
		if st.LastPrint != "" {
			mLast.SetTitle("Último ticket: " + st.LastPrint)
			mLast.Show()
		}

		printers := st.Printers
		sort.Slice(printers, func(i, j int) bool { return printers[i].Name < printers[j].Name })
		for i, s := range slots {
			if i < len(printers) {
				ids[i] = printers[i].ID
				s.SetTitle(printers[i].Name)
				s.Show()
			} else {
				s.Hide()
			}
		}
		if len(printers) == 0 {
			mNone.Show()
		} else {
			mNone.Hide()
		}
	}

	refresh()
	go func() {
		for range time.Tick(5 * time.Second) {
			refresh()
		}
	}()

	for i := range slots {
		go func(i int) {
			for range slots[i].ClickedCh {
				mu.Lock()
				id := ids[i]
				mu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				msg, err := agent.LocalPost(ctx, fmt.Sprintf("/test?printer=%d", id))
				cancel()
				if err != nil {
					notify("No se pudo imprimir: " + err.Error())
				} else {
					notify(msg)
				}
			}
		}(i)
	}

	go func() {
		for {
			select {
			case <-mPair.ClickedCh:
				// El servicio toma la vinculación nueva solo, en unos segundos.
				if err := install.Elevate([]string{"pair"}); err != nil {
					notify("No se pudo abrir la vinculación: " + err.Error())
				}
			case <-mScan.ClickedCh:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, _ = agent.LocalPost(ctx, "/scan")
				cancel()
				notify("Buscando impresoras. En un minuto aparecen en Configuración → Impresoras.")
			case <-mOpen.ClickedCh:
				mu.Lock()
				u := serverURL
				mu.Unlock()
				if u != "" {
					open(strings.TrimRight(u, "/") + "/app/config/printers")
				}
			case <-mLog.ClickedCh:
				open(config.Dir() + `\agent.log`)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// notify deja el resultado en el tooltip hasta el próximo refresco (5 s): un globo de Windows
// necesitaría otra dependencia, y lo que importa (el ticket) sale en la impresora.
func notify(msg string) {
	systray.SetTooltip(short("ZertixPOS: "+msg, 120))
}

func open(target string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// AlreadyRunning evita dos íconos (dos sesiones de la bandeja del mismo usuario).
func AlreadyRunning() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\ZertixPOSAgentTray`)
	h, _, err := procCreateMutex.Call(0, 0, uintptrOf(name))
	if h == 0 {
		return false
	}
	return err == syscall.ERROR_ALREADY_EXISTS
}
