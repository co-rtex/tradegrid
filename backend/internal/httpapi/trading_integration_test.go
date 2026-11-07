package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/co-rtex/tradegrid/backend/internal/exchange"
)

// Two-account integration tests (#46). Every request goes through the real
// router and exchange over HTTP, with a fixed clock and the default ord-N /
// exe-N IDs, and every test starts from a fresh seeded exchange. Nothing is
// mocked: the tests read back what a client would see and check both sides of
// every trade, the reservations, the book, and conservation.

type wireBalance struct {
	Total     int64 `json:"total"`
	Reserved  int64 `json:"reserved"`
	Available int64 `json:"available"`
}

type wireAccount struct {
	UserID    string      `json:"userId"`
	Cash      wireBalance `json:"cash"`
	Positions []struct {
		Symbol string `json:"symbol"`
		wireBalance
	} `json:"positions"`
}

type wireOrder struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	Price     int64  `json:"price"`
	Quantity  int64  `json:"quantity"`
	Remaining int64  `json:"remaining"`
	Status    string `json:"status"`
	Sequence  int64  `json:"sequence"`
}

type wireExecution struct {
	ID           string `json:"id"`
	Sequence     int64  `json:"sequence"`
	Symbol       string `json:"symbol"`
	Price        int64  `json:"price"`
	Quantity     int64  `json:"quantity"`
	BuyOrderID   string `json:"buyOrderId"`
	SellOrderID  string `json:"sellOrderId"`
	BuyerUserID  string `json:"buyerUserId"`
	SellerUserID string `json:"sellerUserId"`
}

type wireLevel struct {
	Price      int64 `json:"price"`
	Quantity   int64 `json:"quantity"`
	OrderCount int   `json:"orderCount"`
}

type wireBook struct {
	AsOfSequence int64       `json:"asOfSequence"`
	Bids         []wireLevel `json:"bids"`
	Asks         []wireLevel `json:"asks"`
}

type submitted struct {
	Status     int
	Order      wireOrder       `json:"order"`
	Executions []wireExecution `json:"executions"`
	Error      struct {
		Code string `json:"code"`
	} `json:"error"`
}

// client is one test's view of a server, failing the test on any transport
// or decoding problem.
type client struct {
	t   *testing.T
	url string
}

func newClient(t *testing.T, cfg exchange.Config) client {
	srv := httptest.NewServer(NewRouter(exchange.New(cfg)))
	t.Cleanup(srv.Close)
	return client{t: t, url: srv.URL}
}

func fixedClock() exchange.Config {
	return exchange.Config{Now: func() time.Time { return time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC) }}
}

func (c client) get(path string, v any) {
	c.t.Helper()
	resp, err := http.Get(c.url + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("GET %s: status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
}

// submit posts one order and decodes either outcome. It is safe to call from
// several goroutines; it reports problems with t.Error so callers can stop.
func (c client) submit(user, symbol, side string, price, qty int64) submitted {
	var s submitted
	resp, err := http.Post(c.url+"/api/v1/orders", "application/json",
		strings.NewReader(orderBody(user, symbol, side, price, qty)))
	if err != nil {
		c.t.Errorf("POST: %v", err)
		return s
	}
	defer resp.Body.Close()
	s.Status = resp.StatusCode
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		c.t.Errorf("POST: decode: %v", err)
	}
	return s
}

func (c client) mustSubmit(user, symbol, side string, price, qty int64) submitted {
	c.t.Helper()
	s := c.submit(user, symbol, side, price, qty)
	if s.Status != http.StatusCreated {
		c.t.Fatalf("%s %s %d %s @ %d: status %d %s", user, side, qty, symbol, price, s.Status, s.Error.Code)
	}
	return s
}

func (c client) accounts() map[string]wireAccount {
	c.t.Helper()
	var body struct{ Accounts []wireAccount }
	c.get("/api/v1/accounts", &body)
	out := map[string]wireAccount{}
	for _, a := range body.Accounts {
		out[a.UserID] = a
	}
	return out
}

func (c client) position(user, symbol string) wireBalance {
	c.t.Helper()
	for _, p := range c.accounts()[user].Positions {
		if p.Symbol == symbol {
			return p.wireBalance
		}
	}
	c.t.Fatalf("%s has no %s position", user, symbol)
	return wireBalance{}
}

func (c client) orders(user string) []wireOrder {
	c.t.Helper()
	var body struct{ Orders []wireOrder }
	c.get("/api/v1/orders?userId="+user+"&limit=200", &body)
	return body.Orders
}

func (c client) executions(symbol string) []wireExecution {
	c.t.Helper()
	var body struct{ Executions []wireExecution }
	c.get("/api/v1/executions?symbol="+symbol+"&limit=200", &body)
	return body.Executions
}

func (c client) book(symbol string) wireBook {
	c.t.Helper()
	var b wireBook
	c.get("/api/v1/orderbook?symbol="+symbol+"&depth=100", &b)
	return b
}

func bal(total, reserved int64) wireBalance {
	return wireBalance{Total: total, Reserved: reserved, Available: total - reserved}
}

// checkConsistent asserts what must hold after any sequence of submissions:
// cash and shares are conserved across both accounts; every reservation is
// exactly what the open orders still need; each order's filled quantity is
// the sum of its executions; and the book holds exactly the open orders'
// remaining quantity.
func (c client) checkConsistent() {
	c.t.Helper()
	accts := c.accounts()
	all := append(c.orders("alice"), c.orders("bob")...)
	var cash int64
	shares := map[string]int64{}
	needCash := map[string]int64{}
	needShares := map[string]int64{}
	for _, a := range accts {
		cash += a.Cash.Total
		for _, p := range a.Positions {
			shares[p.Symbol] += p.Total
		}
	}
	if cash != 20_000_000 || shares["AAPL"] != 200 || shares["MSFT"] != 200 {
		c.t.Errorf("not conserved: cash %d, shares %v", cash, shares)
	}

	filled := map[string]int64{}
	for _, symbol := range []string{"AAPL", "MSFT"} {
		for _, e := range c.executions(symbol) {
			filled[e.BuyOrderID] += e.Quantity
			filled[e.SellOrderID] += e.Quantity
		}
	}
	resting := map[string]map[int64]int64{"BUY": {}, "SELL": {}}
	for _, o := range all {
		if got := o.Quantity - o.Remaining; got != filled[o.ID] {
			c.t.Errorf("order %s filled %d, executions say %d", o.ID, got, filled[o.ID])
		}
		wantStatus := map[bool]string{true: "PARTIALLY_FILLED", false: "NEW"}[o.Remaining < o.Quantity]
		if o.Remaining == 0 {
			wantStatus = "FILLED"
		}
		if o.Status != wantStatus {
			c.t.Errorf("order %s status %s with %d of %d remaining", o.ID, o.Status, o.Remaining, o.Quantity)
		}
		if o.Remaining == 0 {
			continue
		}
		if o.Side == "BUY" {
			needCash[o.UserID] += o.Price * o.Remaining
		} else {
			needShares[o.UserID+" "+o.Symbol] += o.Remaining
		}
		if o.Symbol == "AAPL" {
			resting[o.Side][o.Price] += o.Remaining
		}
	}
	for user, a := range accts {
		if a.Cash.Reserved != needCash[user] {
			c.t.Errorf("%s reserved cash %d, open orders need %d", user, a.Cash.Reserved, needCash[user])
		}
		for _, p := range a.Positions {
			if p.Reserved != needShares[user+" "+p.Symbol] || p.Available != p.Total-p.Reserved || p.Available < 0 {
				c.t.Errorf("%s %s = %+v, open orders need %d reserved", user, p.Symbol, p.wireBalance, needShares[user+" "+p.Symbol])
			}
		}
	}
	b := c.book("AAPL")
	for side, levels := range map[string][]wireLevel{"BUY": b.Bids, "SELL": b.Asks} {
		got := map[int64]int64{}
		for _, l := range levels {
			got[l.Price] += l.Quantity
		}
		if fmt.Sprint(got) != fmt.Sprint(resting[side]) {
			c.t.Errorf("AAPL %s levels %v, open orders %v", side, got, resting[side])
		}
	}
	if len(b.Bids) > 0 && len(b.Asks) > 0 && b.Bids[0].Price >= b.Asks[0].Price {
		c.t.Errorf("crossed book: best bid %d, best ask %d", b.Bids[0].Price, b.Asks[0].Price)
	}
}

// TestTwoAccountTradingScenario walks one exchange through the Sprint 2
// behaviors in order and checks both counterparties after every step.
func TestTwoAccountTradingScenario(t *testing.T) {
	c := newClient(t, fixedClock())

	// No match: two resting asks from Alice at different prices, and a bid
	// from Bob below both. Three reservations are outstanding at once.
	a1 := c.mustSubmit("alice", "AAPL", "SELL", 19000, 10) // ord-1
	a2 := c.mustSubmit("alice", "AAPL", "SELL", 19200, 5)  // ord-2
	b1 := c.mustSubmit("bob", "AAPL", "BUY", 18800, 20)    // ord-3
	for _, s := range []submitted{a1, a2, b1} {
		if s.Order.Status != "NEW" || len(s.Executions) != 0 {
			t.Fatalf("resting order %s = %s with %d fills", s.Order.ID, s.Order.Status, len(s.Executions))
		}
	}
	if got := c.position("alice", "AAPL"); got != bal(100, 15) {
		t.Errorf("alice AAPL = %+v, want 15 of 100 reserved", got)
	}
	if got := c.accounts()["bob"].Cash; got != bal(10_000_000, 376_000) {
		t.Errorf("bob cash = %+v, want 376000 reserved", got)
	}
	c.checkConsistent()

	// Full fill with price improvement: Bob buys 10 at 19100 and pays the
	// resting 19000; the 1000-cent difference returns to available cash.
	full := c.mustSubmit("bob", "AAPL", "BUY", 19100, 10) // ord-4
	if full.Order.Status != "FILLED" || len(full.Executions) != 1 {
		t.Fatalf("full fill = %+v", full)
	}
	e := full.Executions[0]
	if e.Price != 19000 || e.Quantity != 10 || e.SellOrderID != a1.Order.ID || e.BuyerUserID != "bob" || e.SellerUserID != "alice" {
		t.Errorf("execution = %+v", e)
	}
	if got := c.accounts()["bob"].Cash; got != bal(9_810_000, 376_000) {
		t.Errorf("bob cash = %+v", got)
	}
	if got := c.accounts()["alice"].Cash; got != bal(10_190_000, 0) {
		t.Errorf("alice cash = %+v", got)
	}
	if got := c.position("alice", "AAPL"); got != bal(90, 5) {
		t.Errorf("alice AAPL = %+v", got)
	}
	if got := c.position("bob", "AAPL"); got != bal(110, 0) {
		t.Errorf("bob AAPL = %+v", got)
	}
	c.checkConsistent()

	// Partial fill: Bob buys 8 at 19200; 5 fill against Alice's ord-2 and 3
	// rest at 19200, keeping a 57600 reservation.
	part := c.mustSubmit("bob", "AAPL", "BUY", 19200, 8) // ord-5
	if part.Order.Status != "PARTIALLY_FILLED" || part.Order.Remaining != 3 || len(part.Executions) != 1 {
		t.Fatalf("partial fill = %+v", part)
	}
	if got := c.accounts()["bob"].Cash; got != bal(9_810_000-96_000, 376_000+57_600) {
		t.Errorf("bob cash = %+v", got)
	}
	if b := c.book("AAPL"); len(b.Asks) != 0 || len(b.Bids) != 2 || b.Bids[0] != (wireLevel{19200, 3, 1}) || b.Bids[1] != (wireLevel{18800, 20, 1}) {
		t.Errorf("book = %+v", b)
	}
	c.checkConsistent()

	// Alice sells into both bids: best price first, each at its own price.
	sweep := c.mustSubmit("alice", "AAPL", "SELL", 18800, 5) // ord-6
	if len(sweep.Executions) != 2 || sweep.Executions[0].Price != 19200 || sweep.Executions[0].Quantity != 3 ||
		sweep.Executions[1].Price != 18800 || sweep.Executions[1].Quantity != 2 {
		t.Errorf("sweep = %+v", sweep.Executions)
	}
	c.checkConsistent()

	// Same-account trade: Bob's own SELL fills his resting BUY at 18800. Cash
	// and share totals net to zero and both reservations are released.
	before := c.accounts()["bob"]
	self := c.mustSubmit("bob", "AAPL", "SELL", 18800, 4) // ord-7
	if len(self.Executions) != 1 || self.Executions[0].BuyerUserID != "bob" || self.Executions[0].SellerUserID != "bob" {
		t.Fatalf("same-account fill = %+v", self.Executions)
	}
	after := c.accounts()["bob"]
	if after.Cash.Total != before.Cash.Total || after.Cash.Reserved != before.Cash.Reserved-4*18800 {
		t.Errorf("bob cash %+v -> %+v", before.Cash, after.Cash)
	}
	if got := c.position("bob", "AAPL"); got.Total != 120 || got.Reserved != 0 {
		t.Errorf("bob AAPL = %+v", got)
	}
	c.checkConsistent()

	// Rejections change nothing: risk, validation, and decoding.
	snapshot := exchangeState(t, c.url)
	for _, s := range []submitted{
		c.submit("bob", "AAPL", "BUY", 100_000, 1_000),    // INSUFFICIENT_CASH
		c.submit("alice", "MSFT", "SELL", 42_000, 101),    // INSUFFICIENT_SHARES
		c.submit("alice", "AAPL", "SELL", 0, 1),           // INVALID_PRICE
		c.submit("carol", "AAPL", "SELL", 19_000, 1),      // UNKNOWN_USER
		c.submit("bob", "AAPL", "BUY", 19_000, 1_000_001), // INVALID_QUANTITY
	} {
		if s.Status == http.StatusCreated || s.Error.Code == "" {
			t.Errorf("rejection = %+v", s)
		}
	}
	if resp, _ := postJSON(t, c.url, `{"userId":"bob"`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: status %d", resp.StatusCode)
	}
	if got := exchangeState(t, c.url); fmt.Sprint(got) != fmt.Sprint(snapshot) {
		t.Errorf("rejections changed the exchange")
	}
	// Execution history: newest first, gap-free sequences, both parties.
	ex := c.executions("AAPL")
	if len(ex) != 5 {
		t.Fatalf("%d executions, want 5", len(ex))
	}
	for i, e := range ex {
		if e.Sequence != int64(len(ex)-i) || e.ID != fmt.Sprintf("exe-%d", e.Sequence) {
			t.Errorf("executions[%d] = %s seq %d", i, e.ID, e.Sequence)
		}
	}
}

// A failure after matching but before publishing, injected through the
// execution ID source, is a 500 and leaves every read exactly as it was. The
// same request then succeeds normally.
func TestInjectedPreCommitFailureChangesNothing(t *testing.T) {
	var mu sync.Mutex
	fail := false
	cfg := fixedClock()
	cfg.ExecutionID = func(seq int64) string {
		mu.Lock()
		defer mu.Unlock()
		if fail && seq == 2 {
			fail = false
			return "" // rejected by the execution mapper
		}
		return fmt.Sprintf("exe-%d", seq)
	}
	c := newClient(t, cfg)
	c.mustSubmit("alice", "AAPL", "SELL", 19000, 2)
	c.mustSubmit("alice", "AAPL", "SELL", 19100, 2)
	before := exchangeState(t, c.url)

	mu.Lock()
	fail = true
	mu.Unlock()
	s := c.submit("bob", "AAPL", "BUY", 19100, 3) // two fills; the second fails
	if s.Status != http.StatusInternalServerError || s.Error.Code != "INTERNAL" {
		t.Fatalf("injected failure = %d %s", s.Status, s.Error.Code)
	}
	if after := exchangeState(t, c.url); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("failed submission changed the exchange")
	}

	retry := c.mustSubmit("bob", "AAPL", "BUY", 19100, 3)
	if retry.Order.ID != "ord-3" || len(retry.Executions) != 2 || retry.Executions[0].ID != "exe-1" {
		t.Errorf("retry = %+v", retry)
	}
	c.checkConsistent()
}

// Concurrent submissions that each need more than half of Bob's cash, or of
// Alice's shares, cannot overspend or oversell: exactly one of each is
// accepted and every other gets the risk code.
func TestConcurrentContentionCannotOverspendOrOversell(t *testing.T) {
	c := newClient(t, fixedClock())
	const n = 16
	var wg sync.WaitGroup
	results := make(chan submitted, 2*n)
	for range n {
		wg.Add(2)
		go func() { defer wg.Done(); results <- c.submit("bob", "AAPL", "BUY", 18000, 300) }()   // 5,400,000 each
		go func() { defer wg.Done(); results <- c.submit("alice", "MSFT", "SELL", 50000, 60) }() // 60 of 100 shares each
	}
	wg.Wait()
	close(results)
	codes := map[string]int{}
	for r := range results {
		if r.Status == http.StatusCreated {
			codes[r.Order.UserID+" accepted"]++
		} else {
			codes[r.Error.Code]++
		}
	}
	want := map[string]int{"bob accepted": 1, "alice accepted": 1, "INSUFFICIENT_CASH": n - 1, "INSUFFICIENT_SHARES": n - 1}
	if fmt.Sprint(codes) != fmt.Sprint(want) {
		t.Errorf("outcomes = %v, want %v", codes, want)
	}
	if got := c.accounts()["bob"].Cash; got != bal(10_000_000, 5_400_000) {
		t.Errorf("bob cash = %+v", got)
	}
	if got := c.position("alice", "MSFT"); got != bal(100, 60) {
		t.Errorf("alice MSFT = %+v", got)
	}
	c.checkConsistent()
}

// Resting orders submitted concurrently at one price fill in the acceptance
// order the server returned, whatever order the goroutines ran in.
func TestConcurrentFIFOFollowsAcceptanceSequence(t *testing.T) {
	c := newClient(t, fixedClock())
	const n = 20
	var wg sync.WaitGroup
	seqByID := make(map[string]int64, n)
	var mu sync.Mutex
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user := []string{"alice", "bob"}[i%2]
			s := c.submit(user, "AAPL", "SELL", 19000, 1)
			if s.Status != http.StatusCreated {
				t.Errorf("SELL: status %d %s", s.Status, s.Error.Code)
				return
			}
			mu.Lock()
			seqByID[s.Order.ID] = s.Order.Sequence
			mu.Unlock()
		}()
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	buy := c.mustSubmit("bob", "AAPL", "BUY", 19000, 15)
	if len(buy.Executions) != 15 {
		t.Fatalf("%d fills, want 15", len(buy.Executions))
	}
	var filled []int64
	for _, e := range buy.Executions {
		filled = append(filled, seqByID[e.SellOrderID])
	}
	all := make([]int64, 0, n)
	for _, s := range seqByID {
		all = append(all, s)
	}
	slices.Sort(all)
	if !slices.Equal(filled, all[:15]) {
		t.Errorf("filled resting sequences %v, want the first 15 accepted %v", filled, all[:15])
	}
	c.checkConsistent()
}

// Many crossing orders from both accounts at once lose no fill and keep every
// reservation, order, execution, and book level consistent.
func TestConcurrentCrossingOrdersLoseNoFills(t *testing.T) {
	c := newClient(t, fixedClock())
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 25 {
				user := []string{"alice", "bob"}[(w+i)%2]
				side := []string{"BUY", "SELL"}[(w*7+i)%2]
				price := int64(18950 + (w*13+i*7)%11*10)
				if s := c.submit(user, "AAPL", side, price, int64(1+(w+i)%4)); s.Status != http.StatusCreated && s.Error.Code == "" {
					t.Errorf("unexpected response %d", s.Status)
				}
			}
		}()
	}
	wg.Wait()
	if len(c.executions("AAPL")) == 0 {
		t.Fatal("no orders crossed; the test exercises nothing")
	}
	c.checkConsistent()
}
