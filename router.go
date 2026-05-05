package main

import (
	"net/http"
	"time"
)

func newRouter(processor *Processor) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		healthHandler(w, r)
	})
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/image_processor", eventHandler(processor))
	return mux
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
