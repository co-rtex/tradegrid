// Command server runs the TradeGrid backend HTTP server.
//
// It listens on 127.0.0.1 at the port in TRADEGRID_HTTP_PORT (default 8080)
// and stops cleanly on Ctrl-C (SIGINT) or SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/httpapi"
)

const (
	portEnv     = "TRADEGRID_HTTP_PORT"
	defaultPort = "8080"

	// shutdownTimeout bounds how long in-flight requests may take to finish
	// after a stop signal.
	shutdownTimeout = 5 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatalf("TradeGrid backend: %v", err)
	}
	log.Print("TradeGrid backend stopped")
}

// run reads the configuration, opens the listener, and serves until ctx is
// canceled.
func run(ctx context.Context) error {
	port, err := listenPort()
	if err != nil {
		return err
	}
	// Listen only on the loopback interface: the Sprint 2 demo is local, so
	// other machines on the network should not reach it.
	addr := net.JoinHostPort("127.0.0.1", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	log.Printf("TradeGrid backend listening on http://%s (press Ctrl-C to stop)", ln.Addr())
	return serve(ctx, ln, httpapi.NewRouter())
}

// listenPort returns the port from TRADEGRID_HTTP_PORT, or the default when
// it is unset or empty.
func listenPort() (string, error) {
	port := os.Getenv(portEnv)
	if port == "" {
		return defaultPort, nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%s must be a port number from 1 to 65535, got %q", portEnv, port)
	}
	return port, nil
}

// serve handles requests on ln until ctx is canceled, then shuts down
// gracefully: it stops accepting connections and waits up to shutdownTimeout
// for in-flight requests to finish. It closes ln before returning.
func serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler: handler,
		// Drop clients that open a connection but are slow to send headers.
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		// Serve returned before any shutdown was requested.
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Print("TradeGrid backend shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	// After Shutdown, Serve returns http.ErrServerClosed; anything else is a
	// real failure.
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
