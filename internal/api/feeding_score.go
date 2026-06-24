package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"bluei.kr/edge/internal/inference"
)

// LRCN feeding-activity HTTP handler.
//
// Endpoint: POST /v1/vision/feeding-score
// Body:     {"clip_path": "...", "tank_id": "..."} or {"clip_bytes_b64": "...", "tank_id": "..."}
// Response: { "feeding_activity_score": 0..1, "model_version": "...",
//             "inference_ms": int, "frame_count": int, "tank_id": "..." }
//
// The LRCN endpoint is read from the LRCN_ENDPOINT env var (default
// http://127.0.0.1:8081). This keeps Phase 1 config schemas untouched while
// allowing the operator to point to a remote service if needed.
//
// Inference output is an advisory score. The decision routing layer (C-2) is
// responsible for combining this with confidence × mode before any feeding
// command is dispatched.

var (
	lrcnClientOnce sync.Once
	lrcnClient     *inference.LRCNClient
)

func lrcn() *inference.LRCNClient {
	lrcnClientOnce.Do(func() {
		lrcnClient = inference.NewLRCNClient(inference.LRCNConfig{
			Endpoint: os.Getenv("LRCN_ENDPOINT"),
			Timeout:  15 * time.Second,
		})
	})
	return lrcnClient
}

type feedingScoreRequest struct {
	ClipPath     string `json:"clip_path,omitempty"`
	ClipBytesB64 string `json:"clip_bytes_b64,omitempty"`
	TankID       string `json:"tank_id,omitempty"`
}

func (s *Server) handlePostFeedingScore(w http.ResponseWriter, r *http.Request) {
	var req feedingScoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be JSON", err.Error())
		return
	}
	if req.ClipPath == "" && req.ClipBytesB64 == "" {
		writeError(
			w, http.StatusBadRequest, "MISSING_CLIP",
			"clip_path or clip_bytes_b64 is required", "",
		)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := lrcn().Score(ctx, inference.LRCNRequest{
		ClipPath:     req.ClipPath,
		ClipBytesB64: req.ClipBytesB64,
		TankID:       req.TankID,
	})
	if err != nil {
		if errors.Is(err, inference.ErrLRCNModelNotReady) {
			slog.Info(
				"lrcn model not ready",
				"tank_id", req.TankID,
			)
			writeError(
				w, http.StatusServiceUnavailable, "MODEL_NOT_READY",
				"LRCN model has not loaded weights yet (bootstrap pending)",
				err.Error(),
			)
			return
		}
		slog.Warn("lrcn score failed", "tank_id", req.TankID, "error", err)
		writeError(
			w, http.StatusBadGateway, "INFERENCE_FAILED",
			"LRCN inference failed", err.Error(),
		)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"feeding_activity_score": resp.FeedingActivityScore,
		"model_version":          resp.ModelVersion,
		"inference_ms":           resp.InferenceMs,
		"frame_count":            resp.FrameCount,
		"tank_id":                resp.TankID,
	})
}
