
package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Test 1: Requests should rotate through healthy backends.
func TestRoundRobinRouting(t *testing.T) {
	pool := &BackendPool{
		backends: []Backend{
			{URL: "http://backend-1", Name: "backend-1", Healthy: true},
			{URL: "http://backend-2", Name: "backend-2", Healthy: true},
			{URL: "http://backend-3", Name: "backend-3", Healthy: true},
		},
	}

	var got []string

	for i := 0; i < 6; i++ {
		backend, err := pool.Next(map[string]bool{})
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i+1, err)
		}

		got = append(got, backend.Name)
	}

	want := []string{
		"backend-1", "backend-2", "backend-3",
		"backend-1", "backend-2", "backend-3",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-robin mismatch:\ngot:  %v\nwant: %v", got, want)
	}
}

// Test 2: The rate limiter should reject excess requests.
func TestRateLimiterMiddleware(t *testing.T) {
	limiter := NewRateLimiter(1, 2)

	handler := limiter.Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("allowed"))
		},
	))

	// Same client IP for all requests.
	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.0.2.10:12345"

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Burst capacity is 2, so the first two requests should pass.
	for i := 1; i <= 2; i++ {
		rec := makeRequest()
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want %d",
				i, rec.Code, http.StatusOK)
		}
	}

	// No waiting: the third request should be rejected.
	rec := makeRequest()

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: got status %d, want %d",
			rec.Code, http.StatusTooManyRequests)
	}

	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on rate-limited response")
	}
}

// Test 3: If a backend fails, the proxy should retry a healthy backend.
func TestBackendFailureFailover(t *testing.T) {
	// Create a backend, then close it to simulate an unavailable server.
	failedServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("unreachable backend"))
		},
	))
	failedURL := failedServer.URL
	failedServer.Close()

	// This backend should receive the retried request.
	healthyServer := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("healthy backend response"))
		},
	))
	defer healthyServer.Close()

	pool := &BackendPool{
		backends: []Backend{
			{URL: failedURL, Name: "failed-backend", Healthy: true},
			{URL: healthyServer.URL, Name: "healthy-backend", Healthy: true},
		},
	}

	metrics := &Metrics{
		backendRequests:        make(map[string]int),
		backendFailures:        make(map[string]int),
		requestDurationBuckets: make(map[float64]int),
		healthChecksTotal:      make(map[string]int),
		healthCheckFailures:    make(map[string]int),
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := proxyHandler(pool, logger, metrics)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d; body: %s",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), "healthy backend response") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}

	// Verify the failed backend was marked unhealthy.
	pool.mu.Lock()
	failedBackendHealthy := pool.backends[0].Healthy
	pool.mu.Unlock()

	if failedBackendHealthy {
		t.Fatal("expected failed backend to be marked unhealthy")
	}

	// Verify failover was recorded.
	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	if metrics.retriesTotal != 1 {
		t.Fatalf("got %d retries, want 1", metrics.retriesTotal)
	}

	if metrics.backendFailures["failed-backend"] != 1 {
		t.Fatalf("expected one failure for failed-backend, got %d",
			metrics.backendFailures["failed-backend"])
	}

	if metrics.backendRequests["healthy-backend"] != 1 {
		t.Fatalf("expected healthy-backend to handle one request, got %d",
			metrics.backendRequests["healthy-backend"])
	}
}

// Ensure this file uses the standard library only.
var _ = time.Second
