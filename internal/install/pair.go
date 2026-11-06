// Package install vincula el agente con un negocio y, en Windows, lo instala como servicio con su
// ícono en la bandeja.
package install

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Elian-D/zertixpos-print-agent/internal/api"
	"github.com/Elian-D/zertixpos-print-agent/internal/config"
)

var codeRe = regexp.MustCompile(`^\d{6}$`)

// Pair canjea el código de 6 dígitos y guarda la vinculación:
//  1. En el dominio central (central) pregunta de qué negocio es el código.
//  2. En el subdominio del negocio lo canjea por el token.
func Pair(ctx context.Context, central, code, version string) (*config.Config, error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if !codeRe.MatchString(code) {
		return nil, fmt.Errorf("el código son 6 números")
	}
	if central == "" {
		central = config.DefaultServer
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	host, _ := os.Hostname()

	serverURL, err := api.New(central, "", version).Locate(ctx, code)
	if api.IsNotFound(err) {
		// --server puede ser el dominio del negocio (https://negocio.zertixpos.com, o un túnel en
		// pruebas): ahí no existe la ruta central, así que se canjea el código ahí mismo. Si
		// tampoco sirve, el error útil es el del central ("el código no es válido…").
		res, direct := api.New(central, "", version).Pair(ctx, code, host)
		if direct == nil {
			return save(central, res)
		}
		if !api.IsNotFound(direct) {
			return nil, direct
		}
	}
	if err != nil {
		return nil, err
	}

	res, err := api.New(serverURL, "", version).Pair(ctx, code, host)
	if err != nil {
		return nil, err
	}
	return save(central, res)
}

// save guarda la vinculación en agent.json.
func save(central string, res *api.PairResult) (*config.Config, error) {
	cfg, _ := config.Load()
	cfg.CentralURL = strings.TrimRight(central, "/")
	cfg.ServerURL = res.ServerURL
	cfg.Token = res.Token
	cfg.AgentID = res.Agent.ID
	cfg.AgentName = res.Agent.Name
	cfg.Business = res.Business
	cfg.WebSocket = config.WebSocket{URL: res.WebSocket.URL, Key: res.WebSocket.Key, Channel: res.WebSocket.Channel}
	if err := config.Save(cfg); err != nil {
		return nil, fmt.Errorf("se vinculó pero no se pudo guardar en %s: %w", config.Path(), err)
	}
	return cfg, nil
}
