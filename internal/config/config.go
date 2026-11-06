// Package config guarda la vinculación del agente: a qué negocio pertenece, su token y cómo
// conectarse a Reverb. Vive en un JSON en la carpeta de datos del agente.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// DefaultServer es el dominio central de ZertixPOS: ahí se pregunta de quién es un código.
const DefaultServer = "https://zertixpos.com"

// WebSocket es lo que el servidor manda para abrir el timbre (Reverb, protocolo de Pusher).
type WebSocket struct {
	URL     string `json:"url"`
	Key     string `json:"key"`
	Channel string `json:"channel"`
}

// Config es la vinculación guardada en agent.json.
type Config struct {
	// Dominio central, donde se canjea el código (solo para vincular).
	CentralURL string `json:"central_url"`
	// El subdominio del negocio, que recibe todo lo demás.
	ServerURL string    `json:"server_url,omitempty"`
	Token     string    `json:"token,omitempty"`
	AgentID   int64     `json:"agent_id,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	Business  string    `json:"business,omitempty"`
	WebSocket WebSocket `json:"websocket"`
}

// Paired dice si ya tiene token.
func (c *Config) Paired() bool {
	return c.ServerURL != "" && c.Token != ""
}

var mu sync.Mutex

// Dir es la carpeta de datos: %ProgramData%\ZertixPOS\Agente en Windows (la comparten el servicio
// y la bandeja), ~/.config/zertixpos-agent en Linux (para desarrollo). ZERTIX_AGENT_DIR la cambia.
func Dir() string {
	if d := os.Getenv("ZERTIX_AGENT_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "ZertixPOS", "Agente")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zertixpos-agent")
}

// Path es el archivo de la vinculación.
func Path() string {
	return filepath.Join(Dir(), "agent.json")
}

// Load lee la vinculación. Sin archivo devuelve una vacía apuntando al dominio central.
func Load() (*Config, error) {
	mu.Lock()
	defer mu.Unlock()

	cfg := &Config{CentralURL: DefaultServer}
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	if cfg.CentralURL == "" {
		cfg.CentralURL = DefaultServer
	}
	return cfg, nil
}

// Save la escribe de forma atómica (archivo temporal y renombrar).
func Save(cfg *Config) error {
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// Clear borra el token (al desvincular desde ZertixPOS el servidor responde 401).
func Clear(cfg *Config) error {
	cfg.ServerURL = ""
	cfg.Token = ""
	cfg.AgentID = 0
	cfg.AgentName = ""
	cfg.Business = ""
	cfg.WebSocket = WebSocket{}
	return Save(cfg)
}
