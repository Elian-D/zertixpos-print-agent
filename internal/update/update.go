// Package update reemplaza el .exe del agente por la versión publicada en ZertixPOS (un release
// de GitHub). Windows deja renombrar un .exe en uso, así que: se baja el nuevo al lado, el actual
// pasa a .old, el nuevo toma su nombre y el servicio se reinicia.
package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Newer dice si remote (0.2.0) es más nueva que local (0.1.3). Una "v" delante se ignora; "dev"
// nunca se actualiza sola.
func Newer(remote, local string) bool {
	if remote == "" || local == "" || local == "dev" {
		return false
	}
	r, l := parse(remote), parse(local)
	for i := 0; i < 3; i++ {
		if r[i] != l[i] {
			return r[i] > l[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, part := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// Apply baja el .exe de url y reemplaza el que está corriendo. Hay que reiniciar después.
func Apply(ctx context.Context, url string) error {
	if url == "" {
		return errors.New("el servidor no dijo de dónde bajar la versión nueva")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)

	newPath := exe + ".new"
	oldPath := exe + ".old"
	_ = os.Remove(newPath)
	_ = os.Remove(oldPath) // el .old de la actualización anterior

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("la descarga respondió %d", resp.StatusCode)
	}

	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 100<<20))
	f.Close()
	if err != nil {
		_ = os.Remove(newPath)
		return err
	}
	// Un .exe de Windows empieza con "MZ" y pesa varios MB: una página de error de GitHub no.
	if n < 1<<20 || !startsWithMZ(newPath) {
		_ = os.Remove(newPath)
		return errors.New("lo descargado no es un ejecutable")
	}

	if err := os.Rename(exe, oldPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("no se pudo apartar la versión actual: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		return fmt.Errorf("no se pudo poner la versión nueva: %w", err)
	}
	return nil
}

func startsWithMZ(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 2)
	_, err = io.ReadFull(f, head)
	return err == nil && head[0] == 'M' && head[1] == 'Z'
}
