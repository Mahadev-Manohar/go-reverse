package main

import (
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
	p.mu.Lock()
	defer p.mu.Unlock()

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for i := range p.backends {
		resp, err := client.Get(p.backends[i].URL + "/hello")

		if err != nil {
			p.backends[i].Healthy = false
			continue
		}

		resp.Body.Close()

		p.backends[i].Healthy = resp.StatusCode >= 200 &&
			resp.StatusCode < 300
	}
}

func proxyHandler(pool *BackendPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		backend, err := pool.Next()
		if err != nil {
			http.Error(w, "No healthy backends available", http.StatusServiceUnavailable)
			return
		}

		targetURL := backend.URL + r.URL.RequestURI()

		req, err := http.NewRequest(
			r.Method,
			targetURL,
			r.Body,
		)

		if err != nil {
			http.Error(w, "Failed to create request", http.StatusInternalServerError)
			return
		}

		// Forward request headers
		for key, values := range r.Header {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}

		client := &http.Client{}

		resp, err := client.Do(req)

		if err != nil {
			http.Error(w, "Backend unavailable", http.StatusBadGateway)
			return
		}

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
	}
}

func (p *BackendPool) StartHealthChecker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		p.HealthCheck()
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