//go:build windows

package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/Elian-D/zertixpos-print-agent/internal/config"
	"github.com/Elian-D/zertixpos-print-agent/internal/console"
	"github.com/Elian-D/zertixpos-print-agent/internal/winsvc"
)

// Dir es donde queda instalado el .exe.
func Dir() string {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, "ZertixPOS", "Agente")
}

// ExePath es el .exe instalado.
func ExePath() string {
	return filepath.Join(Dir(), "zertixpos-agent.exe")
}

const runKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
const runValue = "ZertixPOSAgentTray"

// IsAdmin dice si el proceso corre como administrador.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// Elevate vuelve a abrir este .exe como administrador con los mismos argumentos (Windows muestra
// el aviso de control de cuentas). El proceso actual termina.
func Elevate(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(quoteArgs(args))
	cwd, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	return windows.ShellExecute(0, verb, file, params, cwd, windows.SW_SHOWNORMAL)
}

// Installed dice si este .exe es el instalado (y no el descargado).
func Installed() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(exe), filepath.Clean(ExePath())) && winsvc.Installed()
}

// Run es la instalación completa: copiar el .exe, vincular, registrar el servicio y la bandeja.
func Run(ctx context.Context, central, version string) error {
	fmt.Println("Instalando el agente de impresión de ZertixPOS")
	fmt.Println(strings.Repeat("-", 46))

	// 1. Copiar a Archivos de programa.
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return fmt.Errorf("no se pudo crear %s: %w", Dir(), err)
	}
	if !strings.EqualFold(filepath.Clean(self), filepath.Clean(ExePath())) {
		// Si ya había uno instalado corriendo, se detiene antes de pisarlo.
		_ = winsvc.Uninstall()
		stopTray()
		if err := copyFile(self, ExePath()); err != nil {
			return fmt.Errorf("no se pudo copiar el agente a %s: %w", Dir(), err)
		}
	}
	fmt.Println("1. Copiado en", Dir())

	// 2. Vincular (si no estaba vinculado ya, por ejemplo al reinstalar).
	cfg, _ := config.Load()
	if cfg.Paired() {
		fmt.Printf("2. Ya estaba vinculado a %s (%s)\n", cfg.Business, cfg.ServerURL)
	} else {
		fmt.Println("2. En ZertixPOS: Configuración → Impresoras → Agregar equipo.")
		for attempt := 1; ; attempt++ {
			code := console.Ask("   Escribe el código de 6 dígitos: ")
			cfg, err = Pair(ctx, central, code, version)
			if err == nil {
				break
			}
			fmt.Println("   No se pudo vincular:", err)
			if attempt >= 5 {
				return errors.New("demasiados intentos: genera un código nuevo y vuelve a abrir el instalador")
			}
		}
		fmt.Printf("   Vinculado a %s como \"%s\"\n", cfg.Business, cfg.AgentName)
	}

	// 3. El servicio.
	if err := winsvc.Install(ExePath()); err != nil {
		return err
	}
	fmt.Println("3. Servicio de Windows registrado y en marcha (arranca solo con la PC)")

	// 4. La bandeja al iniciar sesión, para todos los usuarios, y ahora mismo.
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE)
	if err == nil {
		_ = k.SetStringValue(runValue, fmt.Sprintf(`"%s" tray`, ExePath()))
		k.Close()
	}
	startTray()
	fmt.Println("4. Ícono en la bandeja (junto al reloj)")

	// 5. En Configuración → Aplicaciones instaladas, para desinstalarlo sin comandos.
	RegisterUninstall(version)

	fmt.Println()
	fmt.Println("Listo. Agrega las impresoras en ZertixPOS: Configuración → Impresoras.")
	return nil
}

// Uninstall quita el servicio, la bandeja y el .exe instalado. La vinculación se borra si purge.
func Uninstall(purge bool) error {
	if err := winsvc.Uninstall(); err != nil {
		return err
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(runValue)
		k.Close()
	}
	stopTray()
	_ = registry.DeleteKey(registry.LOCAL_MACHINE, uninstallKey)
	if purge {
		_ = os.RemoveAll(config.Dir())
	}
	// El .exe en uso no se puede borrar: se programa para el próximo reinicio.
	path, _ := windows.UTF16PtrFromString(ExePath())
	_ = windows.MoveFileEx(path, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	return nil
}

// uninstallKey es la entrada de "Aplicaciones instaladas" (Agregar o quitar programas).
const uninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\ZertixPOSAgent`

// RegisterUninstall deja el agente en Configuración → Aplicaciones → Aplicaciones instaladas, con
// su versión y "Desinstalar" (que corre `uninstall --purge`). Lo llaman el instalador y el servicio
// al arrancar, así la versión se mantiene al día después de una actualización automática. Necesita
// permiso de administrador (el servicio corre como SYSTEM); si no lo tiene, no hace nada.
func RegisterUninstall(version string) {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	exe := ExePath()
	_ = k.SetStringValue("DisplayName", "ZertixPOS - Agente de impresión")
	_ = k.SetStringValue("DisplayVersion", version)
	_ = k.SetStringValue("Publisher", "ZertixPOS")
	_ = k.SetStringValue("DisplayIcon", exe)
	_ = k.SetStringValue("InstallLocation", Dir())
	_ = k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" uninstall --purge`, exe))
	_ = k.SetStringValue("URLInfoAbout", "https://zertixpos.com")
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", 8*1024) // KB
}

func startTray() {
	cmd := exec.Command(ExePath(), "tray")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

func stopTray() {
	cmd := exec.Command("taskkill", "/F", "/IM", "zertixpos-agent.exe", "/FI", fmt.Sprintf("PID ne %d", os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func quoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		quoted[i] = a
	}
	return strings.Join(quoted, " ")
}
