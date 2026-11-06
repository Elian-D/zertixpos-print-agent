// Package escpos arma el ticket de prueba local del agente ("Imprimir prueba" de la bandeja y el
// comando test-print). Los tickets de verdad los arma ZertixPOS; el agente solo los manda.
package escpos

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

const (
	esc = 0x1B
	gs  = 0x1D
)

// cp850 son los caracteres del español que no son ASCII.
var cp850 = map[rune]byte{
	'á': 0xA0, 'é': 0x82, 'í': 0xA1, 'ó': 0xA2, 'ú': 0xA3, 'ñ': 0xA4, 'Ñ': 0xA5,
	'¿': 0xA8, '¡': 0xAD, 'Á': 0xB5, 'É': 0x90, 'Í': 0xD6, 'Ó': 0xE0, 'Ú': 0xE9,
	'ü': 0x81, 'Ü': 0x9A, '°': 0xF8,
}

// Encode pasa texto UTF-8 a CP850 (lo que no está sale "?").
func Encode(s string) []byte {
	var b bytes.Buffer
	for _, r := range s {
		switch {
		case r < 0x80:
			b.WriteByte(byte(r))
		case cp850[r] != 0:
			b.WriteByte(cp850[r])
		default:
			b.WriteByte('?')
		}
	}
	return b.Bytes()
}

// Options del ticket de prueba.
type Options struct {
	PrinterName string
	Address     string
	AgentName   string
	Version     string
	Columns     int    // 48 en 80 mm, 32 en 58 mm
	QR          string // opcional
	OpenDrawer  bool
}

// TestTicket arma el ticket de prueba con los mismos comandos que usa ZertixPOS.
func TestTicket(o Options) []byte {
	if o.Columns == 0 {
		o.Columns = 48
	}
	var b bytes.Buffer
	b.Write([]byte{esc, '@'})       // iniciar
	b.Write([]byte{esc, 't', 0x02}) // CP850

	b.Write([]byte{esc, 'a', 1})
	b.Write([]byte{esc, 'E', 1, gs, '!', 0x11})
	b.Write(Encode("PRUEBA\n"))
	b.Write([]byte{gs, '!', 0x00, esc, 'E', 0})
	b.Write(Encode("Agente de impresión ZertixPOS\n"))
	if o.PrinterName != "" {
		b.Write(Encode(o.PrinterName + "\n"))
	}
	if o.Address != "" {
		b.Write(Encode(o.Address + "\n"))
	}
	b.Write([]byte{esc, 'a', 0})
	b.Write(Encode(strings.Repeat("-", o.Columns) + "\n"))

	pair := func(l, r string) {
		pad := o.Columns - len([]rune(l)) - len([]rune(r))
		if pad < 1 {
			pad = 1
		}
		b.Write(Encode(l + strings.Repeat(" ", pad) + r + "\n"))
	}
	if o.AgentName != "" {
		pair("Equipo", o.AgentName)
	}
	pair("Versión del agente", o.Version)
	pair("Caracteres por línea", fmt.Sprint(o.Columns))
	pair("Fecha", time.Now().Format("02/01/2006 3:04 PM"))
	b.Write(Encode(strings.Repeat("1234567890", 5)[:o.Columns] + "\n"))
	b.Write(Encode("Acentos: á é í ó ú ñ Ñ ¿ ¡ Á É Í Ó Ú ü\n"))
	b.Write([]byte{esc, 'E', 1})
	b.Write(Encode("Negrita\n"))
	b.Write([]byte{esc, 'E', 0})
	b.Write(Encode(strings.Repeat("-", o.Columns) + "\n"))

	if o.QR != "" {
		b.Write([]byte{esc, 'a', 1})
		b.Write(QR(o.QR, 6))
		b.Write([]byte{'\n', esc, 'a', 0})
	}

	b.Write([]byte{esc, 'a', 1})
	b.Write(Encode("Si este ticket salió completo,\nla impresora está lista.\n"))
	b.Write([]byte{esc, 'a', 0})

	if o.OpenDrawer {
		b.Write([]byte{esc, 'p', 0x00, 0x19, 0xFA})
	}
	b.Write([]byte{esc, 'd', 4})         // avanzar 4 líneas
	b.Write([]byte{gs, 'V', 0x42, 0x00}) // corte parcial
	return b.Bytes()
}

// QR nativo, modelo 2, corrección M.
func QR(data string, module byte) []byte {
	var b bytes.Buffer
	n := len(data) + 3
	b.Write([]byte{gs, '(', 'k', 4, 0, 0x31, 0x41, 0x32, 0x00})
	b.Write([]byte{gs, '(', 'k', 3, 0, 0x31, 0x43, module})
	b.Write([]byte{gs, '(', 'k', 3, 0, 0x31, 0x45, 0x31})
	b.Write([]byte{gs, '(', 'k', byte(n % 256), byte(n / 256), 0x31, 0x50, 0x30})
	b.WriteString(data)
	b.Write([]byte{gs, '(', 'k', 3, 0, 0x31, 0x51, 0x30})
	return b.Bytes()
}
