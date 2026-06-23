package service_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
)

// TestRequestIDHeader: every response carries an X-Request-ID; a client-supplied id is echoed (so a
// caller can correlate a request with the server's structured access log).
func TestRequestIDHeader(t *testing.T) {
	h := service.New(registry.New(), nil, nil).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID not set on response")
	}

	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/stats", nil)
	req.Header.Set("X-Request-ID", "my-trace-123")
	h.ServeHTTP(rec2, req)
	if got := rec2.Header().Get("X-Request-ID"); got != "my-trace-123" {
		t.Errorf("X-Request-ID = %q, expected the supplied id to be echoed", got)
	}
}

// TestStatsEndpoint: GET /v1/stats exposes the candidate-compile cache hit rate (observability), 200 even
// with no model loaded.
func TestStatsEndpoint(t *testing.T) {
	h := service.New(registry.New(), nil, nil).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("stats code %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "compileCache") {
		t.Errorf("stats body missing compileCache: %s", rec.Body.String())
	}
}

// TestMetricsEndpoint: GET /metrics returns Prometheus-format counters.
func TestMetricsEndpoint(t *testing.T) {
	h := service.New(registry.New(), nil, nil).Handler()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/stats", nil)) // generate a request
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "feelc_http_requests_total") {
		t.Errorf("metrics: code %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestAuthOptIn: with a token configured, the API requires a valid bearer token; the health probes stay
// exempt so orchestrators can probe without a credential.
func TestAuthOptIn(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	srv.SetAuthToken("s3cret")
	h := srv.Handler()

	cases := []struct {
		name, path, auth string
		want             int
	}{
		{"no token", "/v1/stats", "", 401},
		{"wrong token", "/v1/stats", "Bearer nope", 401},
		{"valid token", "/v1/stats", "Bearer s3cret", 200},
		{"healthz exempt", "/healthz", "", 200},
		{"readyz exempt", "/readyz", "", 503}, // exempt from auth, still 503 with no model
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", c.path, nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: code %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

// TestRateLimitOptIn: with a per-IP limit configured, a burst of requests from one IP eventually gets 429.
func TestRateLimitOptIn(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	srv.SetRateLimit(1) // burst = 2
	h := srv.Handler()
	got429 := false
	for i := 0; i < 8; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/stats", nil)) // same default RemoteAddr → one IP
		if rec.Code == 429 {
			got429 = true
		}
	}
	if !got429 {
		t.Error("expected at least one 429 from the rate limiter under a burst")
	}
}
