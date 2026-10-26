package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/httpapi"
)

func TestListenPort(t *testing.T) {
	tests := []struct {
		value   string
		want    string
		wantErr bool
	}{
		{value: "", want: defaultPort},
		{value: "9090", want: "9090"},
		{value: "1", want: "1"},
		{value: "65535", want: "65535"},
		{value: "0", wantErr: true},
		{value: "65536", wantErr: true},
		{value: "-1", wantErr: true},
		{value: "http", wantErr: true},
		{value: "localhost:8080", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv(portEnv, tt.value)

			got, err := listenPort()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("listenPort() = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("listenPort(): %v", err)
			}
			if got != tt.want {
				t.Errorf("listenPort() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestServeAndShutdown runs the real server lifecycle on a random free port:
// it answers /health, and canceling the context stops it and frees the port.
func TestServeAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, httpapi.NewRouter()) }()

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d; body %q", resp.StatusCode, http.StatusOK, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil after cancel", err)
		}
	case <-time.After(shutdownTimeout + time.Second):
		t.Fatal("serve did not return after the context was canceled")
	}

	if conn, err := net.Dial("tcp", addr); err == nil {
		conn.Close()
		t.Errorf("%s still accepts connections after shutdown", addr)
	}
}
