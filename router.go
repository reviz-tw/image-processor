package main

import (
	"net/http"
	"time"
)

func newRouter(processor *Processor) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", healthHandler)
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/image_processor", eventHandler(processor))
	mux.Handle("/batch_backfill_image_vector", batchBackfillImageVectorHandler(processor))
	return mux
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
