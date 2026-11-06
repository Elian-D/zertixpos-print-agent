package icon

import (
	"encoding/binary"
	"testing"
)

func TestICOHeader(t *testing.T) {
	ico := ICO(Green)
	if binary.LittleEndian.Uint16(ico[2:]) != 1 || binary.LittleEndian.Uint16(ico[4:]) != 1 {
		t.Fatal("tiene que ser un .ico con una imagen")
	}
	if ico[6] != 32 || ico[7] != 32 {
		t.Fatal("la imagen tiene que ser de 32×32")
	}
	size := binary.LittleEndian.Uint32(ico[14:])
	offset := binary.LittleEndian.Uint32(ico[18:])
	if int(offset+size) != len(ico) {
		t.Fatalf("el tamaño declarado (%d + %d) no coincide con el archivo (%d)", offset, size, len(ico))
	}
}
