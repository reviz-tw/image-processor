package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

type backfillMode string

const (
	backfillModeMissing backfillMode = "missing"
	backfillModeFailed  backfillMode = "failed"
	backfillModeAll     backfillMode = "all"
)

type batchBackfillImageVectorRequest struct {
	Mode              string `json:"mode"`
	Limit             int    `json:"limit"`
	Cursor            string `json:"cursor"`
	MaxRetries        int    `json:"maxRetries"`
	OnlyOlderThanMins int    `json:"onlyOlderThanMinutes"`
}

type batchBackfillImageVectorResponse struct {
	Mode       string `json:"mode"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor,omitempty"`
	NextCursor string `json:"nextCursor,omitempty"`
	Processed  int    `json:"processed"`
	Succeeded  int    `json:"succeeded"`
	Failed     int    `json:"failed"`
}

type batchBackfillDeps struct {
	Cfg         Config
	List        func(Config, ListImageVectorBackfillCandidatesInput) ([]ImageVectorBackfillCandidate, error)
	MarkAttempt func(Config, string) error
	MarkFailed  func(Config, string, string, time.Time) error
	Backfill    func(context.Context, string, string, string) error
}

func batchBackfillImageVectorHandler(processor *Processor) http.Handler {
	return batchBackfillImageVectorHandlerWithDeps(batchBackfillDeps{
		Cfg:         processor.cfg,
		List:        ListImageVectorBackfillCandidates,
		MarkAttempt: MarkImageVectorBackfillAttempt,
		MarkFailed:  MarkImageVectorBackfillFailed,
		Backfill: func(ctx context.Context, bucket, object, imageFileID string) error {
			return processor.BackfillImageVectorFromObject(ctx, bucket, object, imageFileID)
		},
	})
}

func batchBackfillImageVectorHandlerWithDeps(deps batchBackfillDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeBackfillRequest(deps.Cfg, r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req batchBackfillImageVectorRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req = batchBackfillImageVectorRequest{}
		}
		applyBackfillQueryParams(r, &req)
		mode := normalizeMode(req.Mode)
		if mode == "" {
			http.Error(w, "invalid mode; expected missing|failed|all", http.StatusBadRequest)
			return
		}

		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}
		if limit > 1000 {
			limit = 1000
		}

		maxRetries := req.MaxRetries
		if maxRetries < 0 {
			http.Error(w, "maxRetries must be >= 0", http.StatusBadRequest)
			return
		}

		onlyOlderThanMinutes := req.OnlyOlderThanMins
		if onlyOlderThanMinutes < 0 {
			http.Error(w, "onlyOlderThanMinutes must be >= 0", http.StatusBadRequest)
			return
		}

		candidates, err := deps.List(deps.Cfg, ListImageVectorBackfillCandidatesInput{
			Mode:              string(mode),
			Limit:             limit,
			Cursor:            strings.TrimSpace(req.Cursor),
			MaxRetries:        maxRetries,
			OnlyOlderThanMins: onlyOlderThanMinutes,
		})
		if err != nil {
			if errors.Is(err, ErrInvalidCursor) {
				http.Error(w, "invalid cursor: must be integer", http.StatusBadRequest)
				return
			}
			http.Error(w, "query backfill candidates failed", http.StatusInternalServerError)
			log.Printf("query backfill candidates failed: %v", err)
			return
		}

		succeeded := 0
		failed := 0
		nextCursor := ""
		for _, c := range candidates {
			nextCursor = c.ID

			if err := deps.MarkAttempt(deps.Cfg, c.ImageFileID); err != nil {
				log.Printf("mark retry count failed: id=%s imageFileID=%s err=%v", c.ID, c.ImageFileID, err)
			}

			objectName := buildBackfillObjectName(c.ImageFileID, c.ImageFileExtension)
			if strings.TrimSpace(c.ImageBucket) == "" {
				failed++
				reason := "missing image bucket; set IMAGE_BUCKET env"
				if updateErr := deps.MarkFailed(deps.Cfg, c.ImageFileID, reason, time.Now()); updateErr != nil {
					log.Printf("mark image vector failed status error: imageFileID=%s err=%v", c.ImageFileID, updateErr)
				}
				log.Printf("backfill image vector failed: id=%s imageFileID=%s object=%s err=%s", c.ID, c.ImageFileID, objectName, reason)
				continue
			}
			err := deps.Backfill(r.Context(), c.ImageBucket, objectName, c.ImageFileID)
			if err != nil {
				failed++
				reason := truncateReason(err.Error(), 1024)
				if updateErr := deps.MarkFailed(deps.Cfg, c.ImageFileID, reason, time.Now()); updateErr != nil {
					log.Printf("mark image vector failed status error: imageFileID=%s err=%v", c.ImageFileID, updateErr)
				}
				log.Printf("backfill image vector failed: id=%s imageFileID=%s object=%s err=%v", c.ID, c.ImageFileID, objectName, err)
				continue
			}

			succeeded++
		}

		resp := batchBackfillImageVectorResponse{
			Mode:       string(mode),
			Limit:      limit,
			Cursor:     req.Cursor,
			NextCursor: nextCursor,
			Processed:  len(candidates),
			Succeeded:  succeeded,
			Failed:     failed,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func normalizeMode(raw string) backfillMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(backfillModeMissing):
		return backfillModeMissing
	case string(backfillModeFailed):
		return backfillModeFailed
	case string(backfillModeAll):
		return backfillModeAll
	default:
		return ""
	}
}

func buildBackfillObjectName(imageFileID, imageFileExtension string) string {
	ext := strings.TrimSpace(imageFileExtension)
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		ext = "jpg"
	}
	return "images/" + imageFileID + "-w480." + ext
}

func truncateReason(reason string, maxLen int) string {
	if maxLen <= 0 || len(reason) <= maxLen {
		return reason
	}
	return reason[:maxLen]
}

func applyBackfillQueryParams(r *http.Request, req *batchBackfillImageVectorRequest) {
	q := r.URL.Query()
	if req.Mode == "" {
		req.Mode = q.Get("mode")
	}
	if req.Limit == 0 && q.Get("limit") != "" {
		req.Limit = parseIntEnvStyle(q.Get("limit"))
	}
	if req.Cursor == "" {
		req.Cursor = q.Get("cursor")
	}
	if req.MaxRetries == 0 && q.Get("maxRetries") != "" {
		req.MaxRetries = parseIntEnvStyle(q.Get("maxRetries"))
	}
	if req.OnlyOlderThanMins == 0 && q.Get("onlyOlderThanMinutes") != "" {
		req.OnlyOlderThanMins = parseIntEnvStyle(q.Get("onlyOlderThanMinutes"))
	}
}

func parseIntEnvStyle(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0
		}
		value = value*10 + int(ch-'0')
	}
	return value
}

func authorizeBackfillRequest(cfg Config, r *http.Request) bool {
	if strings.TrimSpace(cfg.BackfillAPIKey) == "" {
		log.Printf("reject /batch_backfill_image_vector: BACKFILL_API_KEY is empty")
		return false
	}
	token := strings.TrimSpace(r.Header.Get("X-Backfill-Api-Key"))
	if token == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			token = strings.TrimSpace(auth[len("Bearer "):])
		}
	}
	return token != "" && token == cfg.BackfillAPIKey
}
