package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
)

type Backend struct {
	URL  string
	Name string
}

type BackendPool struct {
	backends []Backend
	current  int
	mu	   sync.Mutex
}

func (p *BackendPool) Next() Backend {
	p.mu.Lock()
    defer p.mu.Unlock()

	backend := p.backends[p.current]
	p.current = (p.current + 1) % len(p.backends)

	return backend
}

func proxyHandler(pool *BackendPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		backend := pool.Next()

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

func main() {

	pool := &BackendPool{
		backends: []Backend{
			{
				URL:  "http://localhost:9001",
				Name: "backend-1",
			},
			{
				URL:  "http://localhost:9002",
				Name: "backend-2",
			},
			{
				URL:  "http://localhost:9003",
				Name: "backend-3",
			},
		},
	}

	http.HandleFunc("/", proxyHandler(pool))

	fmt.Println("Proxy running on :8080")

	http.ListenAndServe(":8080", nil)
}