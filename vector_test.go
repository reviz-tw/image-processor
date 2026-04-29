package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestComputeImageVector_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vectorize" || r.Method != http.MethodPost {
			t.Fatalf("bad request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(VectorResponse{Vector: []float64{1, 2, 3}})
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	idx := strings.LastIndex(host, ":")
	if idx < 0 {
		t.Fatal(host)
	}
	t.Setenv("VECTOR_PORT", host[idx+1:])
	defer os.Unsetenv("VECTOR_PORT")

	vec, err := ComputeImageVector([]byte("fake-image"))
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 || vec[0] != 1 {
		t.Fatalf("%v", vec)
	}
}

func TestComputeImageVector_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("VECTOR_PORT", host[strings.LastIndex(host, ":")+1:])
	defer os.Unsetenv("VECTOR_PORT")

	_, err := ComputeImageVector([]byte("x"))
	if err == nil || !strings.Contains(err.Error(), "418") {
		t.Fatalf("%v", err)
	}
}

func TestComputeImageVector_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("VECTOR_PORT", host[strings.LastIndex(host, ":")+1:])
	defer os.Unsetenv("VECTOR_PORT")

	_, err := ComputeImageVector([]byte("x"))
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("%v", err)
	}
}
