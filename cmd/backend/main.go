package main

import (
	"flag"
	"fmt"
	"net/http"
)

func main() {
	port := flag.String("port", "9001", "backend port")
	name := flag.String("name", "backend-1", "backend name")

	flag.Parse()

	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")

		fmt.Fprintf(
			w,
			"Hello from %s\nRequest ID: %s\n",
			*name,
			requestID,
		)
	})

	fmt.Printf("%s running on :%s\n", *name, *port)

	http.ListenAndServe(":"+*port, nil)
}