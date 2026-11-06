//go:build windows

package printer

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winspool              = windows.NewLazySystemDLL("winspool.drv")
	procOpenPrinter       = winspool.NewProc("OpenPrinterW")
	procClosePrinter      = winspool.NewProc("ClosePrinter")
	procStartDocPrinter   = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter     = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter  = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter    = winspool.NewProc("EndPagePrinter")
	procWritePrinter      = winspool.NewProc("WritePrinter")
	procEnumPrinters      = winspool.NewProc("EnumPrintersW")
	procGetDefaultPrinter = winspool.NewProc("GetDefaultPrinterW")
)

type docInfo1 struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

type printerInfo4 struct {
	PrinterName *uint16
	ServerName  *uint16
	Attributes  uint32
}

const (
	printerEnumLocal       = 0x00000002
	printerEnumConnections = 0x00000004
)

// Spooler manda los bytes tal cual (modo RAW) a una impresora instalada en Windows: el driver no
// los toca, así que el ESC/POS llega a la impresora como lo armó ZertixPOS.
func Spooler(printerName, docName string, data []byte) error {
	name, err := windows.UTF16PtrFromString(printerName)
	if err != nil {
		return err
	}

	var handle windows.Handle
	if r, _, e := procOpenPrinter.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&handle)), 0); r == 0 {
		return fmt.Errorf("Windows no encuentra la impresora %q: %w", printerName, e)
	}
	defer procClosePrinter.Call(uintptr(handle))

	doc, _ := windows.UTF16PtrFromString(docName)
	raw, _ := windows.UTF16PtrFromString("RAW")
	info := docInfo1{DocName: doc, Datatype: raw}
	if r, _, e := procStartDocPrinter.Call(uintptr(handle), 1, uintptr(unsafe.Pointer(&info))); r == 0 {
		return fmt.Errorf("no se pudo empezar el documento en %q: %w", printerName, e)
	}
	defer procEndDocPrinter.Call(uintptr(handle))

	if r, _, e := procStartPagePrinter.Call(uintptr(handle)); r == 0 {
		return fmt.Errorf("no se pudo empezar la página en %q: %w", printerName, e)
	}
	defer procEndPagePrinter.Call(uintptr(handle))

	var written uint32
	r, _, e := procWritePrinter.Call(uintptr(handle), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	if r == 0 {
		return fmt.Errorf("no se pudo escribir en %q: %w", printerName, e)
	}
	if int(written) != len(data) {
		return fmt.Errorf("la impresora %q aceptó %d de %d bytes", printerName, written, len(data))
	}
	return nil
}

// WindowsPrinters lista las impresoras instaladas en esta PC (locales y conectadas).
func WindowsPrinters() ([]string, error) {
	flags := uintptr(printerEnumLocal | printerEnumConnections)
	var needed, returned uint32
	procEnumPrinters.Call(flags, 0, 4, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil
	}

	buf := make([]byte, needed)
	if r, _, e := procEnumPrinters.Call(flags, 0, 4, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned))); r == 0 {
		return nil, fmt.Errorf("no se pudieron listar las impresoras de Windows: %w", e)
	}

	names := make([]string, 0, returned)
	size := unsafe.Sizeof(printerInfo4{})
	for i := uint32(0); i < returned; i++ {
		info := (*printerInfo4)(unsafe.Pointer(&buf[uintptr(i)*size]))
		names = append(names, windows.UTF16PtrToString(info.PrinterName))
	}
	return names, nil
}
