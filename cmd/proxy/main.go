package main


import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
	"errors"
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


func isRetryable(method string) bool {
	return method == http.MethodGet ||
		method == http.MethodHead ||
		method == http.MethodOptions
}

func createProxyRequest(r *http.Request, backend Backend, body []byte) (*http.Request, error) {
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

	return req, nil
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

func (p *BackendPool) StartHealthChecker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		p.HealthCheck()
	}
}

func proxyHandler(pool *BackendPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		body, err := io.ReadAll(r.Body)

		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		client := &http.Client{
			Timeout: 5 * time.Second,
		}

		maxAttempts := 2

		for attempt := 0; attempt < maxAttempts; attempt++ {

			backend, err := pool.Next()

			if err != nil {
				http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
				return
			}

			req, err := createProxyRequest(r, backend, body)

			if err != nil {
				http.Error(w, "Failed to create request", http.StatusInternalServerError)
				return
			}

			resp, err := client.Do(req)

			if err == nil {
				defer resp.Body.Close()

				// Forward response headers
				for key, values := range resp.Header {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}

				// Forward status code
				w.WriteHeader(resp.StatusCode)

				// Forward response body
				io.Copy(w, resp.Body)

				return
			}

			// Backend failed
			pool.MarkUnhealthy(backend.URL)

			// Don't retry non-idempotent requests
			if !isRetryable(r.Method) {
				http.Error(w, "Backend unavailable", http.StatusBadGateway)
				return
			}
		}

		http.Error(w, "Backend unavailable", http.StatusBadGateway)
	}
}

func main() {

	pool := &BackendPool{
		backends: []Backend{
			{
				URL:  "http://localhost:9001",
				Name: "backend-1",
				Healthy: true,
			},
			{
				URL:  "http://localhost:9002",
				Name: "backend-2",
				Healthy: true,	
			},
			{
				URL:  "http://localhost:9003",
				Name: "backend-3",
				Healthy: true,
			},
		},
	}

	go pool.StartHealthChecker()

	http.HandleFunc("/", proxyHandler(pool))
	fmt.Println("Proxy running on :8080")
	http.ListenAndServe(":8080", nil)
}
