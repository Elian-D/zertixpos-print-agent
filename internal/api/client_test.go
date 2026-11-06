package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalhostAddr(t *testing.T) {
	cases := map[string]string{
		"demo.localhost:8082":        "127.0.0.1:8082",
		"localhost:8085":             "127.0.0.1:8085",
		"suportec.zertixpos.com:443": "suportec.zertixpos.com:443",
		"192.168.1.5:9100":           "192.168.1.5:9100",
	}
	for in, want := range cases {
		if got := LocalhostAddr(in); got != want {
			t.Errorf("LocalhostAddr(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestClientSendsTokenAndMapsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/print-agent/heartbeat":
			if r.Header.Get("Authorization") != "Bearer secreto" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"pending": 2, "printers": []map[string]any{{"id": 1, "name": "Caja", "type": "network", "host": "10.0.0.5", "port": 9100}}})
		case "/api/print-agent/jobs/7/claim":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "El trabajo ya no está pendiente."})
		case "/api/print-agent/locate":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "El código no es válido o ya venció."})
		}
	}))
	defer srv.Close()

	ctx := context.Background()

	hb, err := New(srv.URL, "secreto", "0.1.0").Heartbeat(ctx, "PC")
	if err != nil || hb.Pending != 2 || hb.Printers[0].Host != "10.0.0.5" {
		t.Fatalf("heartbeat = %+v, %v", hb, err)
	}

	if _, err := New(srv.URL, "otro", "0.1.0").Heartbeat(ctx, "PC"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("un token malo tiene que dar ErrUnauthorized, dio %v", err)
	}

	if _, err := New(srv.URL, "secreto", "0.1.0").Claim(ctx, 7); !errors.Is(err, ErrNotPending) {
		t.Fatalf("un 409 tiene que dar ErrNotPending, dio %v", err)
	}

	_, err = New(srv.URL, "", "0.1.0").Locate(ctx, "123456")
	if err == nil || err.Error() != "El código no es válido o ya venció." {
		t.Fatalf("el mensaje del servidor tiene que llegar tal cual, llegó %v", err)
	}
}
