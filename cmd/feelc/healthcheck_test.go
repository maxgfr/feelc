package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCmdHealthcheck exercises the Docker HEALTHCHECK subcommand against stub servers: a ready server
// yields exit 0 (nil), a 503 yields an error, and an unreachable address yields an error.
func TestCmdHealthcheck(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			_, _ = w.Write([]byte("ready"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ready.Close()
	if err := cmdHealthcheck([]string{"--addr", strings.TrimPrefix(ready.URL, "http://")}); err != nil {
		t.Fatalf("healthcheck against a ready server: %v", err)
	}

	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer notReady.Close()
	if err := cmdHealthcheck([]string{"--addr", strings.TrimPrefix(notReady.URL, "http://")}); err == nil {
		t.Fatal("healthcheck against a 503 server should error")
	}

	if err := cmdHealthcheck([]string{"--addr", "127.0.0.1:1"}); err == nil {
		t.Fatal("healthcheck against an unreachable address should error")
	}
}
