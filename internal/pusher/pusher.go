// Package pusher es un cliente mínimo del protocolo de Pusher (el que habla Reverb), solo para lo
// que necesita el agente: conectarse, suscribirse a su canal privado y recibir el timbre.
//
// Protocolo 7: https://pusher.com/docs/channels/library_auth_reference/pusher-websockets-protocol/
package pusher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Message es un mensaje del protocolo. En los mensajes del servidor `data` llega como texto JSON.
type Message struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Authorizer firma la suscripción al canal privado (lo hace ZertixPOS con el token del agente).
type Authorizer func(ctx context.Context, socketID, channel string) (string, error)

// Options dice a dónde conectarse.
type Options struct {
	URL     string // ws://localhost:8085 o wss://negocio.zertixpos.com/ws
	Key     string
	Channel string // private-tenant.X.print-agent.Y
	Version string
	Auth    Authorizer
	// Dial cambia la dirección antes de conectar (*.localhost en desarrollo).
	RewriteAddr func(string) string
}

// Run se conecta y llama a onEvent con cada evento del canal hasta que la conexión se cae o el
// contexto se cancela. onConnected corre al quedar suscrito (para pedir los pendientes).
func Run(ctx context.Context, opt Options, onConnected func(), onEvent func(event string, data []byte)) error {
	if opt.URL == "" || opt.Key == "" || opt.Channel == "" {
		return errors.New("faltan los datos del WebSocket")
	}

	u, err := url.Parse(strings.TrimRight(opt.URL, "/") + "/app/" + url.PathEscape(opt.Key))
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("protocol", "7")
	q.Set("client", "zertixpos-agent")
	q.Set("version", opt.Version)
	q.Set("flash", "false")
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if opt.RewriteAddr != nil {
				addr = opt.RewriteAddr(addr)
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
		},
	}

	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el WebSocket: %w", err)
	}
	defer conn.Close()

	// Cerrar la conexión cuando se cancela el contexto, para que ReadMessage salga.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			conn.Close()
		case <-done:
		}
	}()

	activity := 30 * time.Second
	subscribed := false

	for {
		// Sin nada en activity + margen, la conexión se da por muerta (el servidor manda ping).
		_ = conn.SetReadDeadline(time.Now().Add(activity + 30*time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("se cortó el WebSocket: %w", err)
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Event {
		case "pusher:connection_established":
			var est struct {
				SocketID        string `json:"socket_id"`
				ActivityTimeout int    `json:"activity_timeout"`
			}
			if err := decodeData(msg.Data, &est); err != nil {
				return fmt.Errorf("respuesta inesperada de Reverb: %w", err)
			}
			if est.ActivityTimeout > 0 {
				activity = time.Duration(est.ActivityTimeout) * time.Second
			}
			authCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			signature, err := opt.Auth(authCtx, est.SocketID, opt.Channel)
			cancel()
			if err != nil {
				return err
			}
			if err := send(conn, map[string]any{
				"event": "pusher:subscribe",
				"data":  map[string]string{"auth": signature, "channel": opt.Channel},
			}); err != nil {
				return err
			}
			// Ping propio cada `activity` para que un router no corte la conexión por inactiva.
			go pingLoop(ctx, done, conn, activity)

		case "pusher_internal:subscription_succeeded":
			if !subscribed {
				subscribed = true
				if onConnected != nil {
					onConnected()
				}
			}

		case "pusher:ping":
			if err := send(conn, map[string]any{"event": "pusher:pong", "data": map[string]any{}}); err != nil {
				return err
			}

		case "pusher:pong":
			// respuesta a nuestro ping

		case "pusher:error":
			var perr struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			}
			_ = decodeData(msg.Data, &perr)
			return fmt.Errorf("Reverb respondió un error %d: %s", perr.Code, perr.Message)

		default:
			if msg.Channel == opt.Channel && onEvent != nil {
				var data []byte
				var s string
				if json.Unmarshal(msg.Data, &s) == nil {
					data = []byte(s)
				} else {
					data = msg.Data
				}
				onEvent(msg.Event, data)
			}
		}
	}
}

func pingLoop(ctx context.Context, done chan struct{}, conn *websocket.Conn, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-t.C:
			if err := send(conn, map[string]any{"event": "pusher:ping", "data": map[string]any{}}); err != nil {
				return
			}
		}
	}
}

var writeMu = make(chan struct{}, 1)

// send serializa las escrituras: gorilla/websocket no admite dos escritores a la vez.
func send(conn *websocket.Conn, v any) error {
	writeMu <- struct{}{}
	defer func() { <-writeMu }()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(v)
}

// decodeData lee `data`, que el servidor manda como texto con JSON adentro (a veces como objeto).
func decodeData(raw json.RawMessage, v any) error {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return json.Unmarshal([]byte(s), v)
	}
	return json.Unmarshal(raw, v)
}
