package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLRCNClientScoreSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/feeding-score" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req LRCNRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.ClipPath == "" && req.ClipBytesB64 == "" {
			t.Fatalf("expected clip reference in body")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(LRCNResponse{
			FeedingActivityScore: 0.72,
			ModelVersion:         "bootstrap-2026-06-15",
			InferenceMs:          142,
			FrameCount:           60,
			TankID:               req.TankID,
		})
	}))
	defer srv.Close()

	c := NewLRCNClient(LRCNConfig{Endpoint: srv.URL, Timeout: 2 * time.Second})
	got, err := c.Score(context.Background(), LRCNRequest{
		ClipPath: "/tmp/clip.mp4",
		TankID:   "tank-01",
	})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if got.FeedingActivityScore != 0.72 {
		t.Errorf("score=%v, want 0.72", got.FeedingActivityScore)
	}
	if got.ModelVersion != "bootstrap-2026-06-15" {
		t.Errorf("model_version=%q", got.ModelVersion)
	}
	if got.TankID != "tank-01" {
		t.Errorf("tank_id=%q", got.TankID)
	}
}

func TestLRCNClientScoreRequiresClip(t *testing.T) {
	c := NewLRCNClient(LRCNConfig{Endpoint: "http://127.0.0.1:1"})
	_, err := c.Score(context.Background(), LRCNRequest{TankID: "tank-01"})
	if err == nil || !strings.Contains(err.Error(), "clip_path") {
		t.Fatalf("expected clip required error, got %v", err)
	}
}

func TestLRCNClientModelNotReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"detail":"model_not_ready"}`))
	}))
	defer srv.Close()

	c := NewLRCNClient(LRCNConfig{Endpoint: srv.URL})
	_, err := c.Score(context.Background(), LRCNRequest{ClipPath: "/tmp/x.mp4"})
	if !errors.Is(err, ErrLRCNModelNotReady) {
		t.Fatalf("expected ErrLRCNModelNotReady, got %v", err)
	}
}

func TestLRCNClientReady(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":        "ready",
				"model_version": "bootstrap-2026-06-15",
			})
		}))
		defer srv.Close()
		ok, version, err := NewLRCNClient(LRCNConfig{Endpoint: srv.URL}).Ready(context.Background())
		if err != nil {
			t.Fatalf("Ready: %v", err)
		}
		if !ok || version != "bootstrap-2026-06-15" {
			t.Errorf("ok=%v version=%q", ok, version)
		}
	})
	t.Run("not_ready", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()
		ok, _, err := NewLRCNClient(LRCNConfig{Endpoint: srv.URL}).Ready(context.Background())
		if err != nil {
			t.Fatalf("Ready: %v", err)
		}
		if ok {
			t.Errorf("expected not ready")
		}
	})
}

func TestLRCNClientScoreClampsScore(t *testing.T) {
	// Defense in depth: even if the upstream returns out-of-range values,
	// the client clamps to [0, 1]. Inference outputs are advisory and must
	// never breach the safety domain.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(LRCNResponse{FeedingActivityScore: 1.42})
	}))
	defer srv.Close()
	got, err := NewLRCNClient(LRCNConfig{Endpoint: srv.URL}).Score(
		context.Background(), LRCNRequest{ClipPath: "/tmp/x.mp4"},
	)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if got.FeedingActivityScore != 1.0 {
		t.Errorf("score=%v, want clamped 1.0", got.FeedingActivityScore)
	}
}
