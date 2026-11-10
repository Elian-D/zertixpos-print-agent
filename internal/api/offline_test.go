package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 0.1.1: al detenerse, el agente avisa con su token para que el TPV deje de mandarle tickets.
func TestOfflineSendsTheToken(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/print-agent/offline" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secreto" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	if err := New(srv.URL, "secreto", "0.1.1").Offline(context.Background()); err != nil || !called {
		t.Fatalf("offline = %v (llamado: %v)", err, called)
	}
}
