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
	mu                sync.Mutex
	totalRequests     int
	successfulRequests int
	failedRequests    int
	backendRequests   map[string]int
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

func proxyHandler(pool *BackendPool, logger *slog.Logger, metrics *Metrics,) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := generateRequestID()
		start := time.Now()

		metrics.mu.Lock()
		metrics.totalRequests++
		metrics.mu.Unlock()

		body, err := io.ReadAll(r.Body)

		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		maxAttempts := pool.Size()

		for attempt := 0; attempt < maxAttempts; attempt++ {

			backend, err := pool.Next()

			if err != nil {
				metrics.mu.Lock()
				metrics.failedRequests++
				metrics.mu.Unlock()
				http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
				return
			}

			req, err := createProxyRequest(r, backend, body, requestID)

			if err != nil {
				http.Error(w, "Failed to create request", http.StatusInternalServerError)
				return
			}

			resp, err := client.Do(req)

			if err == nil {
				defer resp.Body.Close()

				for key, values := range resp.Header {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}

				w.WriteHeader(resp.StatusCode)
				io.Copy(w, resp.Body)

				metrics.mu.Lock()
				metrics.successfulRequests++
				metrics.backendRequests[backend.Name]++
				metrics.mu.Unlock()

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

			// Backend failed
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
				http.Error(w, "Backend unavailable", http.StatusBadGateway)
				return
			}
		}

		metrics.mu.Lock()
		metrics.failedRequests++
		metrics.mu.Unlock()
		http.Error(w, "Backend unavailable", http.StatusBadGateway)
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

func (p *BackendPool) Next() (Backend, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < len(p.backends); i++ {
		backend := p.backends[p.current]

		p.current = (p.current + 1) % len(p.backends)

		if backend.Healthy {
			return backend, nil
		}
	}

	return Backend{}, errors.New("no healthy backends available")
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

	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metrics := &Metrics{
		backendRequests: make(map[string]int),
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

	go pool.StartHealthChecker(appCtx)

	server := &http.Server{
		Addr:    ":8080",
		Handler: proxyHandler(pool, logger, metrics),
	}

	fmt.Println("Proxy running on :8080")

	go func() {
		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			fmt.Println("Server error:", err)
		}
	}()

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
