//go:build windows

package console

import "unsafe"

func uintptrOf(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }
