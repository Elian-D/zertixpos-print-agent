package escpos

import (
	"bytes"
	"testing"
)

func TestEncodeCP850(t *testing.T) {
	got := Encode("áéíóúñÑ¿¡ÁÉÍÓÚ")
	want := []byte{0xA0, 0x82, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA8, 0xAD, 0xB5, 0x90, 0xD6, 0xE0, 0xE9}
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = % X, quería % X", got, want)
	}
	if string(Encode("€")) != "?" {
		t.Fatal("lo que no está en CP850 tiene que salir ?")
	}
}

func TestTestTicketCommands(t *testing.T) {
	data := TestTicket(Options{PrinterName: "Caja", Version: "0.1.0", QR: "https://zertixpos.com", OpenDrawer: true})

	if !bytes.HasPrefix(data, []byte{0x1B, '@', 0x1B, 't', 0x02}) {
		t.Fatal("tiene que empezar con ESC @ y ESC t 2 (CP850)")
	}
	if !bytes.HasSuffix(data, []byte{0x1B, 'd', 4, 0x1D, 'V', 0x42, 0x00}) {
		t.Fatal("tiene que terminar avanzando y con el corte parcial")
	}
	if !bytes.Contains(data, []byte{0x1B, 'p', 0x00, 0x19, 0xFA}) {
		t.Fatal("falta el pulso de la gaveta")
	}
	// QR: pL = largo + 3 = 24.
	if !bytes.Contains(data, append([]byte{0x1D, '(', 'k', 24, 0, 0x31, 0x50, 0x30}, "https://zertixpos.com"...)) {
		t.Fatal("falta el QR con su largo")
	}
}
