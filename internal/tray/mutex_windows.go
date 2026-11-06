//go:build windows

package tray

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procCreateMutex = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateMutexW")

func uintptrOf(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }
