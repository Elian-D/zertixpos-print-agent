// Package icon dibuja el ícono de la bandeja en memoria (un .ico de 32×32): una impresora con un
// punto de color según el estado. Sin archivos de imagen en el repositorio.
package icon

import (
	"bytes"
	"encoding/binary"
)

// Colores del punto de estado (R, G, B).
var (
	Green = [3]byte{0x58, 0xC0, 0x3F} // conectado (el verde de ZertixPOS)
	Amber = [3]byte{0xF5, 0x9E, 0x0B} // conectando o sin timbre
	Gray  = [3]byte{0x94, 0xA3, 0xB8} // sin vincular o sin conexión
	Red   = [3]byte{0xEF, 0x44, 0x44} // error al imprimir
)

const size = 32

// ICO devuelve un .ico con la impresora y el punto del color dado.
func ICO(dot [3]byte) []byte {
	px := make([][4]byte, size*size) // BGRA, fila 0 arriba

	set := func(x, y int, rgb [3]byte, a byte) {
		if x >= 0 && x < size && y >= 0 && y < size {
			px[y*size+x] = [4]byte{rgb[2], rgb[1], rgb[0], a}
		}
	}
	rect := func(x0, y0, x1, y1 int, rgb [3]byte) {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				set(x, y, rgb, 0xFF)
			}
		}
	}

	dark := [3]byte{0x1E, 0x29, 0x3B}
	paper := [3]byte{0xFF, 0xFF, 0xFF}

	rect(9, 3, 22, 10, paper)  // papel que entra
	rect(4, 10, 27, 21, dark)  // cuerpo
	rect(9, 19, 22, 28, paper) // ticket que sale
	for _, y := range []int{22, 24, 26} {
		rect(11, y, 20, y, [3]byte{0xCB, 0xD5, 0xE1}) // líneas del ticket
	}

	// Punto de estado abajo a la derecha, con borde blanco.
	cx, cy := 24, 24
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := (x-cx)*(x-cx) + (y-cy)*(y-cy)
			switch {
			case d <= 30:
				set(x, y, dot, 0xFF)
			case d <= 49:
				set(x, y, paper, 0xFF)
			}
		}
	}

	return encode(px)
}

// encode arma el .ico: ICONDIR + una entrada + BITMAPINFOHEADER + píxeles (de abajo arriba) + máscara.
func encode(px [][4]byte) []byte {
	var img bytes.Buffer
	// BITMAPINFOHEADER: alto doble (color + máscara).
	binary.Write(&img, binary.LittleEndian, struct {
		Size, Width, Height                 int32
		Planes, BitCount                    uint16
		Compression, ImageSize              uint32
		XPels, YPels, ClrUsed, ClrImportant int32
	}{40, size, size * 2, 1, 32, 0, 0, 0, 0, 0, 0})
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			img.Write(px[y*size+x][:])
		}
	}
	// Máscara AND: todo 0 (la transparencia la da el alfa). 32 px = 4 bytes por fila.
	img.Write(make([]byte, size*4))

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, struct{ Reserved, Type, Count uint16 }{0, 1, 1})
	binary.Write(&out, binary.LittleEndian, struct {
		W, H, Colors, Reserved byte
		Planes, BitCount       uint16
		Bytes, Offset          uint32
	}{size, size, 0, 0, 1, 32, uint32(img.Len()), 6 + 16})
	out.Write(img.Bytes())
	return out.Bytes()
}
