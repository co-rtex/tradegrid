package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// newTestServer serves the real router on a random loopback port, so these
// tests go through the same routing, method matching, and HTTP encoding as
// the running backend. NewRouter takes no dependencies: no database, seeded
// accounts, or exchange is needed for any of these tests.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, url, err)
	}
	return resp, body
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)

	resp, body := do(t, http.MethodGet, srv.URL+"/health")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body %q", resp.StatusCode, http.StatusOK, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json; charset=utf-8")
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body %q is not valid JSON: %v", body, err)
	}
	if want := map[string]any{"status": "ok"}; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// HEAD is answered by the GET route, with the same headers and no body.
func TestHealthHead(t *testing.T) {
	srv := newTestServer(t)

	resp, body := do(t, http.MethodHead, srv.URL+"/health")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json; charset=utf-8")
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestHealthRejectsUnsupportedMethods(t *testing.T) {
	srv := newTestServer(t)

	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			resp, body := do(t, method, srv.URL+"/health")

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d; body %q", resp.StatusCode, http.StatusMethodNotAllowed, body)
			}
			if got, want := resp.Header.Get("Allow"), "GET, HEAD"; got != want {
				t.Errorf("Allow = %q, want %q", got, want)
			}
		})
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	srv := newTestServer(t)

	for _, path := range []string{"/", "/healthz", "/health/", "/api/v1/health"} {
		t.Run(path, func(t *testing.T) {
			resp, body := do(t, http.MethodGet, srv.URL+path)

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want %d; body %q", resp.StatusCode, http.StatusNotFound, body)
			}
		})
	}
}
