// This deterministic local target makes demonstrations independent of the internet.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9091", "demo listen address")
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthy", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy"}`))
	})
	mux.HandleFunc("GET /unhealthy", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "demo maintenance", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.Write([]byte("delayed response"))
		case <-r.Context().Done():
		}
	})
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("demo service listening on %s", *addr)
	log.Fatal(server.ListenAndServe())
}
