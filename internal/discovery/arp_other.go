//go:build !windows

package discovery

import (
	"bufio"
	"os"
	"strings"
)

// ARPTable lee /proc/net/arp (IP → MAC) en Linux.
func ARPTable() map[string]string {
	table := map[string]string{}
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return table
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Scan() // encabezado
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 4 {
			if mac := NormalizeMAC(fields[3]); mac != "" && mac != "00:00:00:00:00:00" {
				table[fields[0]] = mac
			}
		}
	}
	return table
}
