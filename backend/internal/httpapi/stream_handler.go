package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
	"github.com/co-rtex/tradegrid/backend/internal/realtime"
)

// GET /api/v1/stream (docs/realtime.md, #71). A one-way WebSocket that sends
// small invalidation notices after each committed submission: hello first,
// then one exchange.changed per commit, and a heartbeat when the connection
// has been quiet. Clients re-fetch the REST snapshots; the socket never
// carries balances, books, or orders, and it never accepts an order.

// Stream contract values from docs/realtime.md.
const (
	streamPath            = "/api/v1/stream"
	streamProtocolVersion = 1

	// DefaultHeartbeatInterval is the idle time after which a heartbeat is
	// sent (hello's heartbeatSeconds).
	DefaultHeartbeatInterval = 15 * time.Second
	// DefaultPingInterval and DefaultPongTimeout are the WebSocket-level
	// liveness check.
	DefaultPingInterval = 30 * time.Second
	DefaultPongTimeout  = 10 * time.Second
	// DefaultWriteTimeout bounds every frame the server writes.
	DefaultWriteTimeout = 5 * time.Second

	// streamReadLimit is the largest frame a client may send. Clients send no
	// data messages, so this only has to fit control frames.
	streamReadLimit = 512
)

// DefaultAllowedOrigins are the Vite development origins allowed besides
// same-origin requests and requests without an Origin header.
var DefaultAllowedOrigins = []string{"http://localhost:5173", "http://127.0.0.1:5173"}

// Upgrade-failure codes from docs/realtime.md.
const (
	codeForbiddenOrigin exchange.Code = "FORBIDDEN_ORIGIN"
	codeUpgradeRequired exchange.Code = "UPGRADE_REQUIRED"
)

// StreamConfig configures a Stream. Zero durations and a nil origin list get
// the defaults above; tests shorten the intervals.
type StreamConfig struct {
	// Hub is the exchange's event hub (exchange.Config.Events). Required.
	Hub *realtime.Hub
	// Now stamps sentAt. Defaults to time.Now.
	Now func() time.Time

	HeartbeatInterval time.Duration
	PingInterval      time.Duration
	PongTimeout       time.Duration
	WriteTimeout      time.Duration

	// AllowedOrigins lists exact origins ("scheme://host:port") allowed in
	// addition to same-origin requests and requests without Origin.
	AllowedOrigins []string

	// beforeWrite, if set, runs before every message write. Tests use it to
	// stall a connection and fill its queue.
	beforeWrite func()
	// afterPong, if set, runs after each answered ping.
	afterPong func()
}

// Stream serves GET /api/v1/stream. Create one with NewStream, register it
// with WithStream, and call Close on shutdown: http.Server.Shutdown does not
// close WebSocket connections, so Close does.
type Stream struct {
	cfg     StreamConfig
	allowed map[string]bool

	mu     sync.Mutex
	closed bool
	conns  sync.WaitGroup
}

// NewStream returns a stream endpoint for cfg.Hub.
func NewStream(cfg StreamConfig) *Stream {
	if cfg.Hub == nil {
		panic("httpapi: NewStream needs a realtime.Hub")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = DefaultPingInterval
	}
	if cfg.PongTimeout <= 0 {
		cfg.PongTimeout = DefaultPongTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = DefaultWriteTimeout
	}
	if cfg.AllowedOrigins == nil {
		cfg.AllowedOrigins = DefaultAllowedOrigins
	}
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowed[strings.ToLower(o)] = true
	}
	return &Stream{cfg: cfg, allowed: allowed}
}

// Close closes the hub, which ends every subscription; each connection then
// closes with 1001 (going away). Close waits until every connection handler
// has returned or ctx is done. Later upgrade requests are refused with 503
// before upgrading. It is safe to call more than once.
func (s *Stream) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cfg.Hub.Close()

	done := make(chan struct{})
	go func() {
		s.conns.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stream: connections still open at shutdown: %w", ctx.Err())
	}
}

// track registers one connection handler, unless the stream is closed.
func (s *Stream) track() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns.Add(1)
	return true
}

// envelope is every message's JSON form.
type envelope struct {
	Type         string `json:"type"`
	Version      int    `json:"version"`
	AsOfSequence int64  `json:"asOfSequence"`
	SentAt       string `json:"sentAt"`
	Data         any    `json:"data"`
}

type helloData struct {
	HeartbeatSeconds int64 `json:"heartbeatSeconds"`
}

// ServeHTTP checks the request, upgrades it, and streams notifications until
// the client leaves, the subscription is dropped, or the stream closes.
func (s *Stream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorBody(w, http.StatusMethodNotAllowed, codeMethodNotAllowed,
			fmt.Sprintf("%s is not allowed on %s; allowed: GET", r.Method, streamPath), "")
		return
	}
	if !isWebSocketUpgrade(r) {
		w.Header().Set("Upgrade", "websocket")
		writeErrorBody(w, http.StatusUpgradeRequired, codeUpgradeRequired,
			streamPath+" is a WebSocket endpoint; send a WebSocket upgrade request", "")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin, r.Host) {
		writeErrorBody(w, http.StatusForbidden, codeForbiddenOrigin,
			fmt.Sprintf("origin %q is not allowed", origin), "")
		return
	}
	if !s.track() {
		// Shutting down: no new connection may start.
		writeErrorBody(w, http.StatusServiceUnavailable, exchange.CodeInternal, "server is shutting down", "")
		return
	}
	defer s.conns.Done()

	// InsecureSkipVerify only turns off the library's own Origin check (it is
	// not TLS-related): the contract's origin policy was applied above, and
	// the library's same-host rule would reject the allowed Vite origins.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		// Accept has already answered the client.
		log.Printf("httpapi: stream upgrade failed: %v", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(streamReadLimit)

	sub, seq := s.cfg.Hub.Subscribe()
	defer sub.Unsubscribe()
	s.run(conn, sub, seq)
}

// run serves one upgraded connection. The connection context ends when the
// client disconnects or sends data; every exit closes the socket.
func (s *Stream) run(conn *websocket.Conn, sub *realtime.Subscription, seq int64) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Reader: keeps control frames (pong, close) flowing and closes with 1003
	// on any data message. Its exit ends the connection context.
	go func() {
		defer cancel()
		if _, _, err := conn.Read(ctx); err == nil {
			conn.Close(websocket.StatusUnsupportedData, "clients send no data on this stream")
		}
	}()

	// Liveness: a ping every PingInterval; no pong within PongTimeout drops
	// the connection.
	go func() {
		ticker := time.NewTicker(s.cfg.PingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, stop := context.WithTimeout(ctx, s.cfg.PongTimeout)
				err := conn.Ping(pingCtx)
				stop()
				if err != nil {
					// The peer is unresponsive and would not answer a close
					// handshake either, so drop the connection at once.
					cancel()
					conn.CloseNow()
					return
				}
				if s.cfg.afterPong != nil {
					s.cfg.afterPong()
				}
			}
		}
	}()

	if err := s.write(ctx, conn, "hello", seq, helloData{HeartbeatSeconds: int64(s.cfg.HeartbeatInterval / time.Second)}); err != nil {
		return
	}
	lastSent := seq
	heartbeat := time.NewTimer(s.cfg.HeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub.Events():
			if !ok {
				s.closeForSubscription(conn, sub.Err())
				return
			}
			if err := s.write(ctx, conn, "exchange.changed", e.Sequence, e); err != nil {
				return
			}
			lastSent = e.Sequence
		case <-heartbeat.C:
			// Queued events go first: they reset the idle timer anyway, and a
			// heartbeat must never claim a sequence this connection has not
			// yet been sent.
			if len(sub.Events()) == 0 {
				if err := s.write(ctx, conn, "heartbeat", lastSent, struct{}{}); err != nil {
					return
				}
			}
		}
		// Anything sent resets the idle timer.
		if !heartbeat.Stop() {
			select {
			case <-heartbeat.C:
			default:
			}
		}
		heartbeat.Reset(s.cfg.HeartbeatInterval)
	}
}

// closeForSubscription closes the socket with the code for why the hub ended
// the subscription.
func (s *Stream) closeForSubscription(conn *websocket.Conn, err error) {
	switch {
	case errors.Is(err, realtime.ErrSlowConsumer):
		conn.Close(websocket.StatusTryAgainLater, "too slow; reconnect and re-fetch")
	case errors.Is(err, realtime.ErrClosed):
		conn.Close(websocket.StatusGoingAway, "server shutting down")
	default:
		conn.Close(websocket.StatusInternalError, "subscription ended")
	}
}

// write sends one message with the write timeout. A failed write closes the
// connection with 1011.
func (s *Stream) write(ctx context.Context, conn *websocket.Conn, typ string, seq int64, data any) error {
	if s.cfg.beforeWrite != nil {
		s.cfg.beforeWrite()
	}
	msg, err := json.Marshal(envelope{
		Type:         typ,
		Version:      streamProtocolVersion,
		AsOfSequence: seq,
		SentAt:       s.cfg.Now().UTC().Format(time.RFC3339Nano),
		Data:         data,
	})
	if err != nil {
		log.Printf("httpapi: encode %s: %v", typ, err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, s.cfg.WriteTimeout)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, msg); err != nil {
		if ctx.Err() == nil {
			conn.Close(websocket.StatusInternalError, "write failed")
		}
		return err
	}
	return nil
}

// originAllowed applies docs/realtime.md's origin policy: same-origin (the
// Origin host equals the request Host) or an explicitly allowed origin.
func (s *Stream) originAllowed(origin, host string) bool {
	if s.allowed[strings.ToLower(origin)] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// isWebSocketUpgrade reports whether r asks to upgrade to WebSocket.
func isWebSocketUpgrade(r *http.Request) bool {
	return headerHasToken(r.Header, "Connection", "upgrade") && headerHasToken(r.Header, "Upgrade", "websocket")
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}
