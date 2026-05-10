// Package testutil provides shared helpers for unit-testing the CETIC Cloud
// Platform provider against a mocked HTTP backend.
//
// The first consumer is the CCR (CETIC Container Registry) test suite —
// `internal/resources/registry`, `registryuser`, `registryacl` and the
// `internal/datasources/registry` data source. Future tests for other
// resources should reuse the same fixture pattern :
//
//   srv := testutil.NewTestServer(t, testutil.Routes{
//       {Method: "POST", Path: "/v1/registries", Status: 201, Body: createBody},
//       {Method: "GET",  Path: "/v1/registries/abc", Status: 200, Body: getBody},
//   })
//   defer srv.Close()
//   c := client.New(srv.URL, "ccp_test_unit", "0.0.0-test")
package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Route is a single HTTP fixture. If `BodyFn` is set it takes precedence
// over `Body` and receives the parsed request body so the caller can
// generate a stateful response (incrementing counters, echoing fields, …).
type Route struct {
	Method string
	Path   string
	Status int
	Body   any
	BodyFn func(t *testing.T, reqBody []byte) (status int, body any)
}

// Routes is a list of Route. When multiple Routes match the same
// (method, path) the server consumes them in declaration order — which
// lets tests express step transitions like
// `provisioning -> provisioning -> active`.
type Routes []Route

// RecordedRequest captures one inbound request — useful to assert on the
// body the provider sent (e.g. that PublicIPID was forwarded).
type RecordedRequest struct {
	Method string
	Path   string
	Body   []byte
}

// Server wraps httptest.Server with route consumption + recording.
type Server struct {
	*httptest.Server
	mu      sync.Mutex
	queue   map[string][]Route // key: METHOD + " " + PATH
	calls   []RecordedRequest
	t       *testing.T
}

func key(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

// NewTestServer starts a httptest.Server pre-loaded with `routes`. Each
// match consumes the head of the route queue for that key. Unmatched
// requests yield a 500 + a `t.Errorf` so test authors notice missing
// fixtures immediately.
func NewTestServer(t *testing.T, routes Routes) *Server {
	t.Helper()
	s := &Server{
		queue: make(map[string][]Route),
		t:     t,
	}
	for _, r := range routes {
		k := key(r.Method, r.Path)
		s.queue[k] = append(s.queue[k], r)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Calls returns a copy of all requests received so far. Use to assert on
// what the provider sent (HTTP method, JSON body, sequencing).
func (s *Server) Calls() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RecordedRequest, len(s.calls))
	copy(out, s.calls)
	return out
}

// PendingRoutes returns true if any Route is still waiting to be consumed.
// Tests should call this in defer to ensure expected fixtures all fired.
func (s *Server) PendingRoutes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, q := range s.queue {
		n += len(q)
	}
	return n
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	s.mu.Lock()
	s.calls = append(s.calls, RecordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Body:   body,
	})
	k := key(r.Method, r.URL.Path)
	q := s.queue[k]
	if len(q) == 0 {
		s.mu.Unlock()
		s.t.Errorf("testutil: unexpected request %s %s — no fixture left", r.Method, r.URL.Path)
		http.Error(w, `{"detail":"unexpected request in test"}`, http.StatusInternalServerError)
		return
	}
	route := q[0]
	s.queue[k] = q[1:]
	s.mu.Unlock()

	status := route.Status
	var payload any = route.Body
	if route.BodyFn != nil {
		status, payload = route.BodyFn(s.t, body)
	}
	if status == 0 {
		status = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	switch v := payload.(type) {
	case string:
		_, _ = io.Copy(w, strings.NewReader(v))
	case []byte:
		_, _ = io.Copy(w, bytes.NewReader(v))
	default:
		_ = json.NewEncoder(w).Encode(v)
	}
}
