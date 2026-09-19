package main

import (
	"fmt"
	"net/http"
)

func hello(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello from backend!")
}

func main() {
	http.HandleFunc("/hello", hello)

	fmt.Println("Backend running on :9000")

	http.ListenAndServe(":9000", nil)
}
