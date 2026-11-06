// Package discovery busca impresoras: en la red, las que tienen el puerto 9100 abierto en la subred
// de la PC (con su MAC, para volver a encontrarlas si el router les cambia la IP), y en Windows,
// las instaladas en esta PC.
package discovery

import (
	"context"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Elian-D/zertixpos-print-agent/internal/api"
	"github.com/Elian-D/zertixpos-print-agent/internal/printer"
)

// Port es el puerto RAW de las térmicas de red (JetDirect).
const Port = 9100

// Subnets son las redes IPv4 privadas de esta PC, como /24 (las de un local).
func Subnets() []*net.IPNet {
	var out []*net.IPNet
	seen := map[string]bool{}

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			// Una /16 de una red corporativa serían 65 mil direcciones: se barre la /24 de la PC.
			n := &net.IPNet{IP: ip.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}
			if !seen[n.String()] {
				seen[n.String()] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// ScanNetwork prueba el 9100 en cada dirección de las subredes, de a 64 a la vez.
func ScanNetwork(ctx context.Context) []api.Found {
	var hosts []string
	for _, n := range Subnets() {
		base := n.IP.To4()
		for i := 1; i < 255; i++ {
			ip := net.IPv4(base[0], base[1], base[2], byte(i))
			hosts = append(hosts, ip.String())
		}
	}

	var (
		mu    sync.Mutex
		found []string
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 64)
	)
	for _, h := range hosts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			if printer.Reachable(host, Port, 400*time.Millisecond) {
				mu.Lock()
				found = append(found, host)
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()

	sort.Slice(found, func(i, j int) bool { return ipLess(found[i], found[j]) })

	macs := ARPTable()
	out := make([]api.Found, 0, len(found))
	for _, host := range found {
		out = append(out, api.Found{Type: "network", Host: host, Port: Port, MAC: macs[host]})
	}
	return out
}

// Windows lista las impresoras instaladas en Windows (para las USB).
func Windows() []api.Found {
	names, _ := printer.WindowsPrinters()
	out := make([]api.Found, 0, len(names))
	for _, name := range names {
		if skipVirtual(name) {
			continue
		}
		out = append(out, api.Found{Type: "usb", WindowsName: name, Name: name})
	}
	return out
}

// FindByMAC busca en la red la IP actual de una impresora por su MAC.
func FindByMAC(ctx context.Context, mac string) string {
	mac = NormalizeMAC(mac)
	if mac == "" {
		return ""
	}
	for _, f := range ScanNetwork(ctx) {
		if f.MAC == mac {
			return f.Host
		}
	}
	return ""
}

var macRe = regexp.MustCompile(`(?i)([0-9a-f]{2}[:-]){5}[0-9a-f]{2}`)

// NormalizeMAC deja una MAC como 04:30:76:66:85:15.
func NormalizeMAC(mac string) string {
	m := macRe.FindString(mac)
	if m == "" {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(m, "-", ":"))
}

// Las impresoras de Windows que no son térmicas: PDF, XPS, OneNote, Fax.
func skipVirtual(name string) bool {
	n := strings.ToLower(name)
	for _, v := range []string{"pdf", "xps", "onenote", "fax", "send to"} {
		if strings.Contains(n, v) {
			return true
		}
	}
	return false
}

func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	for i := 0; i < 4; i++ {
		if ia[i] != ib[i] {
			return ia[i] < ib[i]
		}
	}
	return false
}
