package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRouter_Health(t *testing.T) {
	t.Parallel()
	p := &Processor{cfg: Config{}}
	mux := newRouter(p)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}

func TestNewRouter_DoesNotExposeImageVectorBackfill(t *testing.T) {
	t.Parallel()
	p := &Processor{cfg: Config{}}
	mux := newRouter(p)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/batch_backfill_image_vector", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected image vector backfill route to be removed, got %d", rec.Code)
	}
}

func TestNewHTTPServer_Addr(t *testing.T) {
	t.Parallel()
	srv := newHTTPServer(":9999", http.NewServeMux())
	if srv.Addr != ":9999" || srv.ReadHeaderTimeout == 0 {
		t.Fatalf("%+v", srv)
	}
}
