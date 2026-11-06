//go:build !windows

package printer

import "errors"

// Spooler solo existe en Windows: en Linux el agente sirve para desarrollar y probar por IP.
func Spooler(printerName, docName string, data []byte) error {
	return errors.New("la impresión USB solo funciona en Windows")
}

// WindowsPrinters no aplica fuera de Windows.
func WindowsPrinters() ([]string, error) {
	return nil, nil
}
