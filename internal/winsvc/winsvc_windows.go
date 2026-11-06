//go:build windows

// Package winsvc registra y corre el agente como servicio de Windows: arranca con la PC, sin
// ventana y aunque nadie inicie sesión. Si se cae (o se actualiza), Windows lo vuelve a levantar.
package winsvc

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Name es el nombre interno del servicio.
const Name = "ZertixPOSAgent"

// DisplayName es lo que se ve en services.msc.
const DisplayName = "ZertixPOS - Agente de impresión"

// IsService dice si el proceso lo arrancó el administrador de servicios.
func IsService() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

type handler struct {
	run func(ctx context.Context) error
}

func (h *handler) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			cancel()
			if err != nil && err != context.Canceled {
				// Salida con error: Windows aplica la recuperación (reiniciar).
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// Run corre como servicio hasta que Windows lo detiene.
func Run(run func(ctx context.Context) error) error {
	return svc.Run(Name, &handler{run: run})
}

// Install registra el servicio (inicio automático y reinicio si se cae) y lo arranca.
func Install(exePath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("no se pudo abrir el administrador de servicios (¿como administrador?): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err == nil {
		// Ya existe (reinstalación o actualización a mano): se detiene y se apunta al .exe nuevo.
		stop(s)
		cfg, cerr := s.Config()
		if cerr == nil {
			cfg.BinaryPathName = fmt.Sprintf(`"%s" service`, exePath)
			cfg.StartType = mgr.StartAutomatic
			_ = s.UpdateConfig(cfg)
		}
	} else {
		s, err = m.CreateService(Name, exePath, mgr.Config{
			DisplayName:      DisplayName,
			Description:      "Imprime los tickets de ZertixPOS en las impresoras térmicas del negocio.",
			StartType:        mgr.StartAutomatic,
			DelayedAutoStart: false,
		}, "service")
		if err != nil {
			return fmt.Errorf("no se pudo crear el servicio: %w", err)
		}
	}
	defer s.Close()

	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 24*60*60)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)

	if err := s.Start(); err != nil {
		return fmt.Errorf("el servicio se registró pero no arrancó: %w", err)
	}
	return nil
}

// Uninstall lo detiene y lo borra.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return nil // no estaba
	}
	defer s.Close()
	stop(s)
	return s.Delete()
}

// Installed dice si el servicio está registrado.
func Installed() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

func stop(s *mgr.Service) {
	st, err := s.Control(svc.Stop)
	if err != nil {
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	for st.State != svc.Stopped && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return
		}
	}
}
