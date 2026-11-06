// Package printer manda los bytes ESC/POS a la impresora: de red por TCP (puerto 9100, sin
// driver) o USB por el spooler de Windows en modo RAW.
package printer

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// Target es a dónde va el trabajo.
type Target struct {
	Name        string
	Type        string // network | usb
	Host        string
	Port        int
	WindowsName string
}

// Print manda los bytes a la impresora.
func Print(t Target, data []byte) error {
	switch t.Type {
	case "network":
		return Network(t.Host, t.Port, data)
	case "usb":
		if t.WindowsName == "" {
			return errors.New("la impresora USB no tiene nombre de Windows")
		}
		return Spooler(t.WindowsName, "ZertixPOS", data)
	default:
		return fmt.Errorf("tipo de impresora desconocido: %q", t.Type)
	}
}

// Network abre una conexión a host:puerto y escribe todo. La impresora imprime al recibir; no hay
// respuesta que esperar, así que "aceptado" es que el TCP tomó todos los bytes y cerró bien.
func Network(host string, port int, data []byte) error {
	if host == "" {
		return errors.New("la impresora no tiene IP")
	}
	if port == 0 {
		port = 9100
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("no se pudo conectar con la impresora %s: %w", addr, err)
	}
	defer conn.Close()

	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	for written := 0; written < len(data); {
		n, err := conn.Write(data[written:])
		if err != nil {
			return fmt.Errorf("la impresora %s cortó la conexión: %w", addr, err)
		}
		written += n
	}

	// Cerrar solo el lado de escritura y dar un momento para que el buffer salga.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	return nil
}

// Reachable dice si host:puerto acepta conexiones (para la búsqueda y para reintentar).
func Reachable(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
