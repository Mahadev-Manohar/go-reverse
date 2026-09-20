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
		fmt.Fprintf(w, "Hello from %s\n", *name)
	})

	fmt.Printf("%s running on :%s\n", *name, *port)

	http.ListenAndServe(":"+*port, nil)
}