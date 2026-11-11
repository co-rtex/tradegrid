package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
	"github.com/co-rtex/tradegrid/backend/internal/realtime"
)

// streamMsg is a decoded stream message.
type streamMsg struct {
	Type         string          `json:"type"`
	Version      int             `json:"version"`
	AsOfSequence int64           `json:"asOfSequence"`
	SentAt       string          `json:"sentAt"`
	Data         json.RawMessage `json:"data"`
}

type changedData struct {
	Symbol          string   `json:"symbol"`
	OrderID         string   `json:"orderId"`
	UserID          string   `json:"userId"`
	ExecutionCount  int      `json:"executionCount"`
	AffectedUserIDs []string `json:"affectedUserIds"`
}

type streamEnv struct {
	t      *testing.T
	hub    *realtime.Hub
	svc    *exchange.Service
	stream *Stream
	srv    *httptest.Server
	url    string
}

// newStreamEnv serves the real router with a stream over a fresh exchange
// whose hub feeds it, as cmd/server does.
func newStreamEnv(t *testing.T, mutate func(*StreamConfig)) *streamEnv {
	t.Helper()
	hub := realtime.New(0)
	cfg := StreamConfig{
		Hub: hub,
		Now: func() time.Time { return time.Date(2026, 10, 20, 15, 0, 5, 0, time.UTC) },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	stream := NewStream(cfg)
	hub = cfg.Hub
	svc := exchange.New(exchange.Config{Events: hub})
	srv := httptest.NewServer(NewRouter(svc, WithStream(stream)))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := stream.Close(ctx); err != nil {
			t.Errorf("close stream: %v", err)
		}
		srv.Close()
	})
	return &streamEnv{t: t, hub: hub, svc: svc, stream: stream, srv: srv,
		url: "ws" + strings.TrimPrefix(srv.URL, "http") + streamPath}
}

func (e *streamEnv) dial(header http.Header) *websocket.Conn {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, e.url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		e.t.Fatalf("dial: %v (response %v)", err, resp)
	}
	e.t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func (e *streamEnv) submit(user, symbol, side string, price, qty int64) exchange.SubmitResult {
	e.t.Helper()
	res, err := e.svc.Submit(exchange.SubmitRequest{UserID: user, Symbol: symbol, Side: side, Type: "LIMIT", Price: price, Quantity: qty})
	if err != nil {
		e.t.Fatalf("submit: %v", err)
	}
	return res
}

// waitSubscribers waits, without a fixed sleep, until the hub has n subscribers.
func (e *streamEnv) waitSubscribers(n int) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for e.hub.Len() != n {
		if time.Now().After(deadline) {
			e.t.Fatalf("hub has %d subscribers, want %d", e.hub.Len(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func read(t *testing.T, conn *websocket.Conn) streamMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("message type %v, want text", typ)
	}
	var m streamMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if m.Version != 1 || m.SentAt == "" || len(m.Data) == 0 || m.Data[0] != '{' {
		t.Fatalf("bad envelope: %s", raw)
	}
	return m
}

func changed(t *testing.T, m streamMsg) changedData {
	t.Helper()
	if m.Type != "exchange.changed" {
		t.Fatalf("type %q, want exchange.changed", m.Type)
	}
	var d changedData
	if err := json.Unmarshal(m.Data, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// closeStatus reads until the connection closes and returns its close code.
func closeStatus(t *testing.T, conn *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func TestStreamHelloThenOneChangePerCommit(t *testing.T) {
	env := newStreamEnv(t, nil)
	conn := env.dial(nil)

	hello := read(t, conn)
	if hello.Type != "hello" || hello.AsOfSequence != 0 || string(hello.Data) != `{"heartbeatSeconds":15}` ||
		hello.SentAt != "2026-10-20T15:00:05Z" {
		t.Fatalf("hello = %+v %s", hello, hello.Data)
	}

	// docs/realtime.md's two samples: a resting order, then a fill against it.
	env.submit("alice", "AAPL", "SELL", 19000, 10)
	env.submit("bob", "AAPL", "BUY", 19050, 25)

	first := read(t, conn)
	if d := changed(t, first); first.AsOfSequence != 1 || d.Symbol != "AAPL" || d.OrderID != "ord-1" ||
		d.UserID != "alice" || d.ExecutionCount != 0 || strings.Join(d.AffectedUserIDs, ",") != "alice" {
		t.Errorf("first change = %d %+v", first.AsOfSequence, d)
	}
	second := read(t, conn)
	if d := changed(t, second); second.AsOfSequence != 2 || d.OrderID != "ord-2" || d.UserID != "bob" ||
		d.ExecutionCount != 1 || strings.Join(d.AffectedUserIDs, ",") != "alice,bob" {
		t.Errorf("second change = %d %+v", second.AsOfSequence, d)
	}
}

func TestStreamTwoClientsBothReceiveCommittedChanges(t *testing.T) {
	env := newStreamEnv(t, nil)
	a, b := env.dial(nil), env.dial(nil)
	read(t, a)
	read(t, b)

	env.submit("bob", "MSFT", "BUY", 42000, 1)
	for _, conn := range []*websocket.Conn{a, b} {
		if m := read(t, conn); m.AsOfSequence != 1 || changed(t, m).Symbol != "MSFT" {
			t.Errorf("change = %+v", m)
		}
	}
}

func TestStreamHelloReportsTheCommittedSequence(t *testing.T) {
	env := newStreamEnv(t, nil)
	env.submit("alice", "AAPL", "SELL", 19000, 1)
	env.submit("alice", "AAPL", "SELL", 19100, 1)

	conn := env.dial(nil)
	if hello := read(t, conn); hello.Type != "hello" || hello.AsOfSequence != 2 {
		t.Fatalf("hello = %+v", hello)
	}
	env.submit("bob", "AAPL", "BUY", 19000, 1)
	if m := read(t, conn); m.AsOfSequence != 3 {
		t.Errorf("next change has sequence %d, want 3", m.AsOfSequence)
	}
}

func TestStreamRejectedOrdersSendNothing(t *testing.T) {
	env := newStreamEnv(t, nil)
	conn := env.dial(nil)
	read(t, conn)

	for _, req := range []exchange.SubmitRequest{
		{UserID: "bob", Symbol: "AAPL", Side: "BUY", Type: "LIMIT", Price: 20000, Quantity: 600}, // INSUFFICIENT_CASH
		{UserID: "carol", Symbol: "AAPL", Side: "BUY", Type: "LIMIT", Price: 1, Quantity: 1},     // UNKNOWN_USER
		{UserID: "bob", Symbol: "AAPL", Side: "BUY", Type: "MARKET", Price: 1, Quantity: 1},      // UNSUPPORTED_ORDER_TYPE
	} {
		if _, err := env.svc.Submit(req); err == nil {
			t.Fatalf("%+v was accepted", req)
		}
	}
	// The next message is the next commit: nothing was sent for the rejections.
	env.submit("alice", "MSFT", "SELL", 42000, 1)
	if m := read(t, conn); m.AsOfSequence != 1 || changed(t, m).UserID != "alice" {
		t.Errorf("message after rejections = %+v", m)
	}
}

func TestStreamHeartbeatAfterIdleCarriesTheLastSequence(t *testing.T) {
	env := newStreamEnv(t, func(c *StreamConfig) { c.HeartbeatInterval = 20 * time.Millisecond })
	conn := env.dial(nil)
	if hello := read(t, conn); string(hello.Data) != `{"heartbeatSeconds":0}` {
		// Sub-second test intervals round down; production uses 15 s.
		t.Fatalf("hello data %s", hello.Data)
	}
	env.submit("alice", "AAPL", "SELL", 19000, 1)
	if m := read(t, conn); m.Type != "exchange.changed" {
		t.Fatalf("got %+v", m)
	}
	hb := read(t, conn)
	if hb.Type != "heartbeat" || hb.AsOfSequence != 1 || string(hb.Data) != "{}" {
		t.Errorf("heartbeat = %+v %s", hb, hb.Data)
	}
}

func TestStreamClosesWith1003WhenTheClientSendsData(t *testing.T) {
	env := newStreamEnv(t, nil)
	conn := env.dial(nil)
	read(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	order := `{"userId":"bob","symbol":"AAPL","side":"BUY","type":"LIMIT","price":19000,"quantity":1}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(order)); err != nil {
		t.Fatal(err)
	}
	if code := closeStatus(t, conn); code != websocket.StatusUnsupportedData {
		t.Errorf("close code %d, want 1003", code)
	}
	if got := env.svc.Accounts()[1].Cash.Reserved; got != 0 {
		t.Errorf("an order sent over the socket reserved %d cents", got)
	}
	env.waitSubscribers(0)
}

func TestStreamDisconnectReleasesTheSubscription(t *testing.T) {
	env := newStreamEnv(t, nil)
	a, b := env.dial(nil), env.dial(nil)
	read(t, a)
	read(t, b)
	env.waitSubscribers(2)

	a.Close(websocket.StatusNormalClosure, "bye")
	env.waitSubscribers(1)
	b.CloseNow() // abrupt: no close handshake
	env.waitSubscribers(0)

	// Trading continues with no subscribers.
	env.submit("alice", "AAPL", "SELL", 19000, 1)
}

func TestStreamShutdownClosesEveryConnectionWith1001(t *testing.T) {
	env := newStreamEnv(t, nil)
	a, b := env.dial(nil), env.dial(nil)
	read(t, a)
	read(t, b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closed := make(chan error, 1)
	go func() { closed <- env.stream.Close(ctx) }()
	for _, conn := range []*websocket.Conn{a, b} {
		if code := closeStatus(t, conn); code != websocket.StatusGoingAway {
			t.Errorf("close code %d, want 1001", code)
		}
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := env.hub.Len(); n != 0 {
		t.Errorf("%d subscriptions left after shutdown", n)
	}

	// After shutdown no new stream starts.
	resp, err := http.Get(env.srv.URL + streamPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("plain GET after shutdown = %d", resp.StatusCode)
	}
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	if _, resp, err := websocket.Dial(dctx, env.url, nil); err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("upgrade after shutdown: err %v, response %v", err, resp)
	}
}

func TestStreamSlowConsumerIsDroppedWith1013AndTradingContinues(t *testing.T) {
	stall := make(chan struct{})
	var once sync.Once
	var writes int
	var mu sync.Mutex
	env := newStreamEnv(t, func(c *StreamConfig) {
		c.Hub = realtime.New(1) // a queue of one event
		c.beforeWrite = func() {
			mu.Lock()
			writes++
			n := writes
			mu.Unlock()
			if n == 2 { // the first exchange.changed write stalls
				<-stall
			}
		}
	})
	t.Cleanup(func() { once.Do(func() { close(stall) }) })

	conn := env.dial(nil)
	read(t, conn)
	env.waitSubscribers(1)

	// Event 1 is taken and its write stalls; event 2 fills the queue; event 3
	// overflows it, so the hub drops this subscriber without blocking.
	for i := range 3 {
		env.submit("alice", "AAPL", "SELL", 19000+int64(i), 1)
	}
	if n := env.hub.Len(); n != 0 {
		t.Fatalf("slow subscriber still registered (%d)", n)
	}
	once.Do(func() { close(stall) })

	// The client gets what was already taken or queued, then 1013.
	if m := read(t, conn); m.AsOfSequence != 1 {
		t.Errorf("first change %d", m.AsOfSequence)
	}
	if m := read(t, conn); m.AsOfSequence != 2 {
		t.Errorf("second change %d", m.AsOfSequence)
	}
	if code := closeStatus(t, conn); code != websocket.StatusTryAgainLater {
		t.Errorf("close code %d, want 1013", code)
	}
}

func TestStreamUpgradeFailures(t *testing.T) {
	env := newStreamEnv(t, nil)
	tests := []struct {
		name, method string
		header       http.Header
		status       int
		code         string
		wantHeader   [2]string
	}{
		{"plain GET", http.MethodGet, nil, http.StatusUpgradeRequired, "UPGRADE_REQUIRED", [2]string{"Upgrade", "websocket"}},
		{"POST", http.MethodPost, nil, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", [2]string{"Allow", "GET"}},
		{"HEAD", http.MethodHead, nil, http.StatusMethodNotAllowed, "", [2]string{"Allow", "GET"}},
		{"foreign origin", http.MethodGet, upgradeHeader("http://evil.example"), http.StatusForbidden, "FORBIDDEN_ORIGIN", [2]string{}},
		{"null origin", http.MethodGet, upgradeHeader("null"), http.StatusForbidden, "FORBIDDEN_ORIGIN", [2]string{}},
		{"other Vite port", http.MethodGet, upgradeHeader("http://localhost:5174"), http.StatusForbidden, "FORBIDDEN_ORIGIN", [2]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, env.srv.URL+streamPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tt.header {
				req.Header[k] = v
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tt.status)
			}
			if k := tt.wantHeader[0]; k != "" && resp.Header.Get(k) != tt.wantHeader[1] {
				t.Errorf("%s = %q, want %q", k, resp.Header.Get(k), tt.wantHeader[1])
			}
			if tt.code == "" {
				return
			}
			var body struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Error.Code != tt.code {
				t.Errorf("body code %q (%v), want %q", body.Error.Code, err, tt.code)
			}
		})
	}
	if n := env.hub.Len(); n != 0 {
		t.Errorf("failed upgrades left %d subscriptions", n)
	}
}

func upgradeHeader(origin string) http.Header {
	return http.Header{
		"Connection":            {"Upgrade"},
		"Upgrade":               {"websocket"},
		"Sec-Websocket-Version": {"13"},
		"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
		"Origin":                {origin},
	}
}

func TestStreamAllowsTheContractOrigins(t *testing.T) {
	env := newStreamEnv(t, nil)
	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:5173", env.srv.URL} {
		conn := env.dial(http.Header{"Origin": {origin}})
		if m := read(t, conn); m.Type != "hello" {
			t.Errorf("%s: first message %q", origin, m.Type)
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}
}

func TestStreamPingsKeepAHealthyConnectionOpen(t *testing.T) {
	pongs := make(chan struct{}, 16)
	env := newStreamEnv(t, func(c *StreamConfig) {
		c.PingInterval = 5 * time.Millisecond
		c.PongTimeout = 5 * time.Second
		c.afterPong = func() {
			select {
			case pongs <- struct{}{}:
			default:
			}
		}
	})
	conn := env.dial(nil)
	read(t, conn)

	// The client library answers pings while a Read is in progress.
	next := make(chan streamMsg, 1)
	go func() { next <- read(t, conn) }()
	for range 3 {
		select {
		case <-pongs:
		case <-time.After(5 * time.Second):
			t.Fatal("no pong within 5 s")
		}
	}
	// After several ping rounds the connection still delivers events.
	env.submit("alice", "AAPL", "SELL", 19000, 1)
	if m := <-next; m.Type != "exchange.changed" || m.AsOfSequence != 1 {
		t.Errorf("after pings got %+v", m)
	}
}

func TestRouterWithoutStreamAnswersNotFound(t *testing.T) {
	srv := httptest.NewServer(NewRouter(exchange.New(exchange.Config{})))
	defer srv.Close()
	resp, err := http.Get(srv.URL + streamPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

func TestStreamClosesAConnectionThatStopsAnsweringPings(t *testing.T) {
	env := newStreamEnv(t, func(c *StreamConfig) {
		c.PingInterval = 10 * time.Millisecond
		c.PongTimeout = 50 * time.Millisecond
	})
	conn := env.dial(nil)
	read(t, conn)
	env.waitSubscribers(1)
	// The client stops reading, so its library never answers a ping. The
	// server gives up and releases the subscription.
	env.waitSubscribers(0)
	env.submit("alice", "AAPL", "SELL", 19000, 1)
}
