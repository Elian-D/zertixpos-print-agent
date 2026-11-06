//go:build windows

// Package console: el .exe se compila sin ventana (-H windowsgui) para que la bandeja y el
// servicio no abran una consola negra. Los comandos que escriben o preguntan algo (instalar,
// vincular, probar) piden una: la de la terminal desde donde se corrió o, con doble clic, una nueva.
//
// Desde PowerShell un .exe sin ventana no se espera: con `| Out-Host` al final PowerShell espera a
// que termine y muestra lo que escribe (la salida va por la tubería, que aquí se respeta).
package console

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
	procSetTitle      = kernel32.NewProc("SetConsoleTitleW")
)

const attachParentProcess = ^uintptr(0) // (DWORD)-1

var owned bool

// redirected dice si el handle ya va a una tubería o a un archivo (`| Out-Host`, `> log.txt`).
func redirected(std uint32) bool {
	h, err := windows.GetStdHandle(std)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return false
	}
	t, err := windows.GetFileType(h)
	return err == nil && (t == windows.FILE_TYPE_PIPE || t == windows.FILE_TYPE_DISK)
}

// Open conecta stdin/stdout/stderr a una consola. Devuelve true si la consola es nueva (doble clic):
// entonces al terminar hay que esperar un Enter para que no se cierre sin que se lea.
func Open(title string) bool {
	outRedirected := redirected(windows.STD_OUTPUT_HANDLE)

	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		if outRedirected {
			return false // sin consola pero con tubería: alcanza
		}
		if r, _, _ := procAllocConsole.Call(); r == 0 {
			return false
		}
		owned = true
	}
	if t, err := windows.UTF16PtrFromString(title); err == nil && owned {
		procSetTitle.Call(uintptrOf(t))
	}

	if !outRedirected {
		if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			os.Stdout = out
			os.Stderr = out
		}
	}
	if !redirected(windows.STD_INPUT_HANDLE) {
		if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			os.Stdin = in
		}
	}
	// En una consola heredada el cursor queda después del prompt: una línea en blanco lo acomoda.
	if !owned && !outRedirected {
		fmt.Println()
	}
	return owned
}

// Pause espera un Enter si la consola es nuestra.
func Pause() {
	if !owned {
		return
	}
	fmt.Print("\nPresiona Enter para cerrar...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// Ask pregunta y devuelve la respuesta sin espacios.
func Ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
