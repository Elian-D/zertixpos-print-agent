//go:build !windows

// Package tray: el ícono de la bandeja es solo de Windows.
package tray

import "fmt"

func Run(version string) {
	fmt.Println("La bandeja solo existe en Windows. Usa `status` para ver el estado.")
}

func AlreadyRunning() bool { return false }
