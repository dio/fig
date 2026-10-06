package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	var received atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("/count", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]uint64{"received": received.Load()})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "body limit", 413)
			return
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path, "policy": r.Header.Get("x-fig-policy"), "plan": r.Header.Get("x-fig-plan"), "body": string(data),
		})
	})
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
