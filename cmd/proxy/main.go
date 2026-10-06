package main


import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
	"log/slog"
	"crypto/rand"
)


type Backend struct {
	URL  string
	Name string
	Healthy bool
}

type BackendPool struct {
	backends []Backend
	current  int
	mu	   sync.Mutex
}

type Metrics struct {
	mu                  sync.Mutex
	totalRequests       int
	successfulRequests  int
	failedRequests      int
	retriesTotal        int
	backendRequests     map[string]int
	backendFailures     map[string]int
}



func (m *Metrics) RequestStarted() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests++
}

func (m *Metrics) RequestSucceeded(backend string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.successfulRequests++
	m.backendRequests[backend]++
}

func (m *Metrics) RequestFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.failedRequests++
}

func (m *Metrics) BackendFailed(backend string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.backendFailures[backend]++
}

func (m *Metrics) Retry() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.retriesTotal++
}

func generateRequestID() string {
	b := make([]byte, 16)

	_, err := rand.Read(b)
	if err != nil {
		return "unknown"
	}

	return fmt.Sprintf("%x", b)
}

func isRetryable(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions
}

func createProxyRequest(r *http.Request, backend Backend, body []byte, requestID string,) (*http.Request, error) {
	req, err := http.NewRequest(
		r.Method,
		backend.URL+r.URL.RequestURI(),
		bytes.NewReader(body),
	)

	if err != nil {
		return nil, err
	}

	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("X-Request-ID", requestID)

	return req, nil
}

func proxyHandler(pool *BackendPool, logger *slog.Logger, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		requestID := generateRequestID()
		start := time.Now()

		metrics.RequestStarted()

		body, err := io.ReadAll(r.Body)

		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		attempted := make(map[string]bool)

		for {

			// Select the next healthy backend
			backend, err := pool.Next(attempted)

			if err != nil {
				metrics.RequestFailed()

				http.Error(
					w,
					"No healthy backends available",
					http.StatusServiceUnavailable,
				)
				return
			}

			attempted[backend.URL] = true

			req, err := createProxyRequest(
				r,
				backend,
				body,
				requestID,
			)

			if err != nil {
				metrics.RequestFailed()

				http.Error(
					w,
					"Failed to create request",
					http.StatusInternalServerError,
				)
				return
			}

			resp, err := client.Do(req)

			// --------------------------------
			// SUCCESS
			// --------------------------------

			if err == nil {

				defer resp.Body.Close()

				for key, values := range resp.Header {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}

				w.WriteHeader(resp.StatusCode)

				io.Copy(w, resp.Body)

				metrics.RequestSucceeded(backend.Name)

				logger.Info(
					"request completed",
					"request_id", requestID,
					"method", r.Method,
					"path", r.URL.Path,
					"backend", backend.Name,
					"status", resp.StatusCode,
					"duration", time.Since(start).String(),
				)

				return
			}

			// --------------------------------
			// BACKEND FAILURE
			// --------------------------------

			metrics.BackendFailed(backend.Name)
			pool.MarkUnhealthy(backend.URL)

			logger.Error(
				"backend request failed",
				"request_id", requestID,
				"backend", backend.Name,
				"url", backend.URL,
				"error", err,
			)

			// Don't retry unsafe methods
			if !isRetryable(r.Method) {

				metrics.RequestFailed()

				http.Error(
					w,
					"Backend unavailable",
					http.StatusBadGateway,
				)

				return
			}

			// Record the retry
			metrics.Retry()

			// Loop continues and Next() selects another backend
		}
	}
}

func metricsHandler(metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		metrics.mu.Lock()
		defer metrics.mu.Unlock()

		fmt.Fprintf(w, "total_requests %d\n", metrics.totalRequests)
		fmt.Fprintf(w, "successful_requests %d\n", metrics.successfulRequests)
		fmt.Fprintf(w, "failed_requests %d\n", metrics.failedRequests)

		for backend, count := range metrics.backendRequests {
			fmt.Fprintf(
				w,
				"backend_requests{backend=\"%s\"} %d\n",
				backend,
				count,
			)
		}

		fmt.Fprintf(w, "retries_total %d\n", metrics.retriesTotal)

		for backend, count := range metrics.backendFailures {
			fmt.Fprintf(
				w,
				"backend_failures{backend=\"%s\"} %d\n",
				backend,
				count,
			)
		}
	}
}



func (p *BackendPool) MarkUnhealthy(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.backends {
		if p.backends[i].URL == url {
			p.backends[i].Healthy = false
			return
		}
	}
}

func (p *BackendPool) Next(attempted map[string]bool) (Backend, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < len(p.backends); i++ {

		backend := p.backends[p.current]

		p.current = (p.current + 1) % len(p.backends)

		if !backend.Healthy {
			continue
		}

		if attempted[backend.URL] {
			continue
		}

		return backend, nil
	}

	return Backend{}, errors.New("no untried healthy backends available")
}

func (p *BackendPool) HealthCheck() {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for i := range p.backends {
		url := p.backends[i].URL + "/hello"

		resp, err := client.Get(url)

		healthy := false

		if err == nil {
			healthy = resp.StatusCode >= 200 && resp.StatusCode < 300
			resp.Body.Close()
		}

		p.mu.Lock()
		p.backends[i].Healthy = healthy
		p.mu.Unlock()
	}
}

func (p *BackendPool) StartHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.HealthCheck()

		case <-ctx.Done():
			fmt.Println("Health checker stopped")
			return
		}
	}
}

func (p *BackendPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.backends)
}



func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("proxy starting", "address", ":8080")

	// Create a context that can be canceled to stop the health checker
	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := &Metrics{
		backendRequests: make(map[string]int),
		backendFailures: make(map[string]int),
	}

	pool := &BackendPool{
		backends: []Backend{
			{
				URL:     "http://localhost:9001",
				Name:    "backend-1",
				Healthy: true,
			},
			{
				URL:     "http://localhost:9002",
				Name:    "backend-2",
				Healthy: true,
			},
			{
				URL:     "http://localhost:9003",
				Name:    "backend-3",
				Healthy: true,
			},
		},
	}

	// Start the health checker in a separate goroutine
	go pool.StartHealthChecker(appCtx)

	// Create a new ServeMux and register handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metricsHandler(metrics))
	mux.HandleFunc("/", proxyHandler(pool, logger, metrics))

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	fmt.Println("Proxy running on :8080")

	go func() {
		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			fmt.Println("Server error:", err)
		}
	}()

	// Wait for an interrupt signal to gracefully shut down the server
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	<-stop

	fmt.Println("\nShutting down proxy...")

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Println("Shutdown error:", err)
	}

	fmt.Println("Proxy stopped")
}
