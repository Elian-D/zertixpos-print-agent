// Package api habla con ZertixPOS por REST: vincular, avisar que está vivo, tomar y confirmar
// trabajos, informar impresoras encontradas y firmar la suscripción al timbre.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ErrUnauthorized: el token ya no sirve (el equipo se desvinculó en ZertixPOS).
var ErrUnauthorized = errors.New("el equipo no está vinculado: pide un código nuevo en Configuración → Impresoras")

// Printer es una impresora tal como la manda el servidor.
type Printer struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // network | usb
	Host        string `json:"host"`
	Port        int    `json:"port"`
	WindowsName string `json:"windows_name"`
	MAC         string `json:"mac"`
}

// Release es la versión publicada del agente.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// WebSocket es cómo abrir el timbre.
type WebSocket struct {
	URL     string `json:"url"`
	Key     string `json:"key"`
	Channel string `json:"channel"`
}

// PairResult es la respuesta de canjear el código.
type PairResult struct {
	Agent struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"agent"`
	Business  string    `json:"business"`
	ServerURL string    `json:"server_url"`
	Token     string    `json:"token"`
	WebSocket WebSocket `json:"websocket"`
	Release   Release   `json:"release"`
}

// Heartbeat es la respuesta de "estoy vivo".
type Heartbeat struct {
	Agent struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"agent"`
	Printers  []Printer `json:"printers"`
	Pending   int       `json:"pending"`
	WebSocket WebSocket `json:"websocket"`
	Release   Release   `json:"release"`
}

// Job es un trabajo tomado: el ESC/POS en base64 y a dónde mandarlo.
type Job struct {
	ID      int64   `json:"id"`
	Type    string  `json:"type"`
	Content string  `json:"content"`
	Printer Printer `json:"printer"`
}

// Found es una impresora encontrada en la red o en Windows.
type Found struct {
	Type        string `json:"type"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	MAC         string `json:"mac,omitempty"`
	WindowsName string `json:"windows_name,omitempty"`
	Name        string `json:"name,omitempty"`
}

// Client es un cliente HTTP con el token del agente.
type Client struct {
	BaseURL string
	Token   string
	Version string
	HTTP    *http.Client
}

// New arma el cliente. Los nombres *.localhost se resuelven a 127.0.0.1 aunque el sistema no lo
// haga (Sail en desarrollo: http://demo.localhost:8082).
func New(baseURL, token, version string) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, LocalhostAddr(addr))
		},
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		Version: version,
		HTTP:    &http.Client{Timeout: 20 * time.Second, Transport: transport},
	}
}

// LocalhostAddr cambia "algo.localhost:puerto" por "127.0.0.1:puerto".
func LocalhostAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

// Locate pregunta en el dominio central a qué negocio pertenece el código.
func (c *Client) Locate(ctx context.Context, code string) (string, error) {
	var out struct {
		ServerURL string `json:"server_url"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/print-agent/locate", map[string]string{"code": code}, &out, false); err != nil {
		return "", err
	}
	return out.ServerURL, nil
}

// Pair canjea el código por el token en el subdominio del negocio.
func (c *Client) Pair(ctx context.Context, code, hostname string) (*PairResult, error) {
	var out PairResult
	err := c.do(ctx, http.MethodPost, "/api/print-agent/pair", map[string]string{
		"code": code, "hostname": hostname, "version": c.Version,
	}, &out, false)
	return &out, err
}

// Heartbeat avisa que está vivo y trae sus impresoras, los pendientes y la versión publicada.
func (c *Client) Heartbeat(ctx context.Context, hostname string) (*Heartbeat, error) {
	var out Heartbeat
	err := c.do(ctx, http.MethodPost, "/api/print-agent/heartbeat", map[string]string{
		"version": c.Version, "hostname": hostname,
	}, &out, true)
	return &out, err
}

// Pending lista los trabajos que esperan.
func (c *Client) Pending(ctx context.Context) ([]int64, error) {
	var out struct {
		Jobs []struct {
			ID int64 `json:"id"`
		} `json:"jobs"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/print-agent/jobs", nil, &out, true); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(out.Jobs))
	for _, j := range out.Jobs {
		ids = append(ids, j.ID)
	}
	return ids, nil
}

// ErrNotPending: otro proceso ya tomó el trabajo, o venció.
var ErrNotPending = errors.New("el trabajo ya no está pendiente")

// IsNotFound dice si el servidor respondió 404 (ruta inexistente o código no encontrado).
func IsNotFound(err error) bool {
	var h *HTTPError
	return errors.As(err, &h) && h.Status == http.StatusNotFound
}

// Claim toma un trabajo.
func (c *Client) Claim(ctx context.Context, id int64) (*Job, error) {
	var out Job
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/print-agent/jobs/%d/claim", id), map[string]any{}, &out, true)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusConflict {
		return nil, ErrNotPending
	}
	return &out, err
}

// Complete confirma un trabajo impreso o fallido.
func (c *Client) Complete(ctx context.Context, id int64, ok bool, errMsg string) error {
	body := map[string]any{"ok": ok}
	if errMsg != "" {
		body["error"] = errMsg
	}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/print-agent/jobs/%d/complete", id), body, nil, true)
}

// Discovered informa lo encontrado. Devuelve las impresoras a las que el servidor les cambió la IP.
func (c *Client) Discovered(ctx context.Context, found []Found) error {
	if found == nil {
		found = []Found{}
	}
	return c.do(ctx, http.MethodPost, "/api/print-agent/discovered", map[string]any{"printers": found}, nil, true)
}

// Auth firma la suscripción al canal privado del timbre.
func (c *Client) Auth(ctx context.Context, socketID, channel string) (string, error) {
	var out struct {
		Auth string `json:"auth"`
	}
	err := c.do(ctx, http.MethodPost, "/api/print-agent/broadcasting/auth", map[string]string{
		"socket_id": socketID, "channel_name": channel,
	}, &out, true)
	return out.Auth, err
}

// HTTPError es una respuesta que no fue 2xx, con el mensaje del servidor.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("el servidor respondió %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any, auth bool) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zertixpos-print-agent/"+c.Version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("no se pudo conectar con %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode == http.StatusUnauthorized && auth {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var msg struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &msg)
		if resp.StatusCode == http.StatusTooManyRequests && msg.Message == "" {
			msg.Message = "demasiados intentos: espera un minuto"
		}
		return &HTTPError{Status: resp.StatusCode, Message: msg.Message}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("respuesta inesperada del servidor: %w", err)
		}
	}
	return nil
}
