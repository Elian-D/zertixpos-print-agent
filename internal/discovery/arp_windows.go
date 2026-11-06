//go:build windows

package discovery

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"strings"
	"syscall"
)

// ARPTable lee `arp -a` (IP → MAC) sin abrir una ventana. Las columnas cambian de nombre según el
// idioma de Windows, así que se busca una IP y una MAC en cada línea.
func ARPTable() map[string]string {
	cmd := exec.Command("arp", "-a")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.Output()
	if err != nil {
		return map[string]string{}
	}

	table := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			continue
		}
		if mac := NormalizeMAC(fields[1]); mac != "" && mac != "ff:ff:ff:ff:ff:ff" {
			table[fields[0]] = mac
		}
	}
	return table
}
