package main

import (
	"fmt"
	"io"
	"net/http"
)

func proxyHandler(w http.ResponseWriter, r *http.Request) {

	req, err := http.NewRequest(
		r.Method,
		"http://localhost:9000"+r.URL.RequestURI(),
		r.Body,
	)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	
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

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)
}

func main() {
	http.HandleFunc("/", proxyHandler)

	fmt.Println("Proxy running on :8080")

	http.ListenAndServe(":8080", nil)
}