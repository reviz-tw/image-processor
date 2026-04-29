package main

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeMode(t *testing.T) {
	t.Parallel()
	if normalizeMode("MISSING") != backfillModeMissing {
		t.Fatal()
	}
	if normalizeMode("failed") != backfillModeFailed {
		t.Fatal()
	}
	if normalizeMode(" ALL ") != backfillModeAll {
		t.Fatal()
	}
	if normalizeMode("x") != "" {
		t.Fatal()
	}
}

func TestBuildBackfillObjectName(t *testing.T) {
	t.Parallel()
	if got := buildBackfillObjectName("abc", "png"); got != "images/abc-w480.png" {
		t.Fatalf("%q", got)
	}
	if got := buildBackfillObjectName("abc", " .JPG "); got != "images/abc-w480.JPG" {
		t.Fatalf("%q", got)
	}
	if got := buildBackfillObjectName("abc", ""); got != "images/abc-w480.jpg" {
		t.Fatalf("%q", got)
	}
}

func TestTruncateReason(t *testing.T) {
	t.Parallel()
	if truncateReason("hi", 0) != "hi" {
		t.Fatal()
	}
	if truncateReason("hello", 3) != "hel" {
		t.Fatal()
	}
}

func TestParseIntEnvStyle(t *testing.T) {
	t.Parallel()
	if parseIntEnvStyle("  ") != 0 {
		t.Fatal()
	}
	if parseIntEnvStyle("42") != 42 {
		t.Fatal()
	}
	if parseIntEnvStyle("4a2") != 0 {
		t.Fatal()
	}
}

func TestApplyBackfillQueryParams(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/?mode=missing&limit=50&cursor=9&maxRetries=2&onlyOlderThanMinutes=15", nil)
	var body batchBackfillImageVectorRequest
	applyBackfillQueryParams(req, &body)
	if body.Mode != "missing" || body.Limit != 50 || body.Cursor != "9" || body.MaxRetries != 2 || body.OnlyOlderThanMins != 15 {
		t.Fatalf("%+v", body)
	}
	// JSON body wins when already set
	req2 := httptest.NewRequest(http.MethodGet, "/?mode=all", nil)
	body2 := batchBackfillImageVectorRequest{Mode: "missing", Limit: 10}
	applyBackfillQueryParams(req2, &body2)
	if body2.Mode != "missing" {
		t.Fatal("mode should not be overwritten from query")
	}
}

func TestAuthorizeBackfillRequest(t *testing.T) {
	t.Parallel()
	cfg := Config{BackfillAPIKey: "secret"}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Backfill-Api-Key", "secret")
	if !authorizeBackfillRequest(cfg, req) {
		t.Fatal("header auth")
	}
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.Header.Set("Authorization", "Bearer secret")
	if !authorizeBackfillRequest(cfg, req2) {
		t.Fatal("bearer auth")
	}
	req3 := httptest.NewRequest(http.MethodPost, "/", nil)
	if authorizeBackfillRequest(cfg, req3) {
		t.Fatal("missing token")
	}
	if authorizeBackfillRequest(Config{}, req) {
		t.Fatal("empty api key")
	}
}

func TestBatchBackfillImageVectorHandlerWithDeps_MethodAndAuth(t *testing.T) {
	deps := batchBackfillDeps{
		Cfg: Config{BackfillAPIKey: "k"},
		List: func(Config, ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error) {
			return nil, nil
		},
		MarkAttempt: func(Config, string) error { return nil },
		MarkFailed:  func(Config, string, string, time.Time) error { return nil },
		Backfill:    func(context.Context, string, string, string) error { return nil },
	}
	h := batchBackfillImageVectorHandlerWithDeps(deps)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"missing"}`))
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", rec2.Code)
	}
}

func TestBatchBackfillImageVectorHandlerWithDeps_Validation(t *testing.T) {
	cfg := Config{BackfillAPIKey: "k"}
	deps := batchBackfillDeps{
		Cfg: cfg,
		List: func(Config, ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error) {
			return nil, nil
		},
		MarkAttempt: func(Config, string) error { return nil },
		MarkFailed:  func(Config, string, string, time.Time) error { return nil },
		Backfill:    func(context.Context, string, string, string) error { return nil },
	}
	h := batchBackfillImageVectorHandlerWithDeps(deps)
	hdr := func(r *http.Request) {
		r.Header.Set("X-Backfill-Api-Key", "k")
	}

	t.Run("invalid mode", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"nope"}`))
		hdr(req)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("maxRetries negative", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"missing","maxRetries":-1}`))
		hdr(req)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("onlyOlderThan negative", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"missing","onlyOlderThanMinutes":-1}`))
		hdr(req)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("invalid cursor", func(t *testing.T) {
		d := deps
		d.List = func(cfg Config, in ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error) {
			_, _, err := buildListImageVectorBackfillQuery(cfg.QueriedDbTable, in)
			return nil, err
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"missing","cursor":"x"}`))
		hdr(req)
		batchBackfillImageVectorHandlerWithDeps(d).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%d", rec.Code)
		}
	})
	t.Run("list error 500", func(t *testing.T) {
		d := deps
		d.List = func(Config, ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error) {
			return nil, errors.New("db")
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"missing"}`))
		hdr(req)
		batchBackfillImageVectorHandlerWithDeps(d).ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%d", rec.Code)
		}
	})
}

func TestBatchBackfillImageVectorHandlerWithDeps_SuccessAndFailures(t *testing.T) {
	cfg := Config{BackfillAPIKey: "k", ImageBucket: "bkt"}
	var markFailedCalls int
	deps := batchBackfillDeps{
		Cfg: cfg,
		List: func(Config, ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error) {
			return []ImageVectorBackfillCandidate{
				{ID: "1", ImageFileID: "a", ImageFileExtension: "jpg", ImageBucket: ""},
				{ID: "2", ImageFileID: "b", ImageFileExtension: "png", ImageBucket: "bkt"},
			}, nil
		},
		MarkAttempt: func(Config, string) error { return nil },
		MarkFailed: func(Config, string, string, time.Time) error {
			markFailedCalls++
			return nil
		},
		Backfill: func(context.Context, string, string, string) error {
			return errors.New("boom")
		},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"all","limit":2000}`))
	req.Header.Set("X-Backfill-Api-Key", "k")
	batchBackfillImageVectorHandlerWithDeps(deps).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var resp batchBackfillImageVectorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Limit != 1000 {
		t.Fatalf("limit capped: %d", resp.Limit)
	}
	if resp.Processed != 2 || resp.Failed != 2 || resp.Succeeded != 0 {
		t.Fatalf("%+v", resp)
	}
	if markFailedCalls != 2 {
		t.Fatalf("markFailedCalls=%d", markFailedCalls)
	}
}

func TestBatchBackfillImageVectorHandlerWithDeps_SuccessPath(t *testing.T) {
	cfg := Config{BackfillAPIKey: "k", ImageBucket: "bkt"}
	deps := batchBackfillDeps{
		Cfg: cfg,
		List: func(Config, ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error) {
			return []ImageVectorBackfillCandidate{
				{ID: "9", ImageFileID: "x", ImageFileExtension: "", ImageBucket: "bkt"},
			}, nil
		},
		MarkAttempt: func(Config, string) error { return nil },
		MarkFailed:  func(Config, string, string, time.Time) error { return nil },
		Backfill:    func(context.Context, string, string, string) error { return nil },
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"missing"}`))
	req.Header.Set("X-Backfill-Api-Key", "k")
	batchBackfillImageVectorHandlerWithDeps(deps).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var resp batchBackfillImageVectorResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Succeeded != 1 || resp.Failed != 0 || resp.NextCursor != "9" {
		t.Fatalf("%+v", resp)
	}
}

func TestBatchBackfillImageVectorHandler_EmptyCandidates(t *testing.T) {
	withMockDB(t, func(mock sqlmock.Sqlmock) {
		rows := sqlmock.NewRows([]string{"id", "imageFile_id", "imageFile_extension"})
		mock.ExpectQuery("SELECT").WithArgs(driver.Value(int64(10))).WillReturnRows(rows)

		p := &Processor{
			cfg: Config{
				BackfillAPIKey: "sek",
				QueriedDbTable: "Photo",
				ImageBucket:    "bk",
			},
		}
		h := batchBackfillImageVectorHandler(p)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/?mode=missing&limit=10", http.NoBody)
		req.Header.Set("X-Backfill-Api-Key", "sek")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
}
