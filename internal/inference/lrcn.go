// Package inference: LRCN activity baseline client.
//
// LRCN (Long-term Recurrent Convolutional Networks) is the activity-baseline
// inference service ported from the validated KIARA-assessed legacy system
// (SW appraisal 330,000,000 KRW, ref. KI210401-3, 2021-04-19).
//
// The Python FastAPI process (services/lrcn/server.py) loads the PyTorch
// LRCN model and exposes POST /v1/feeding-score. This Go client only carries
// JSON requests; the inference output is treated as an advisory signal and
// must never directly approve feeding or equipment control by itself.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LRCNConfig holds endpoint and client tuning for the LRCN service.
type LRCNConfig struct {
	Endpoint string        // default http://127.0.0.1:8081
	Timeout  time.Duration // default 15s (matches inference latency budget)
}

// LRCNClient calls the LRCN FastAPI service.
type LRCNClient struct {
	endpoint string
	client   *http.Client
	// segClient — 세그먼트 일괄 채점 전용. 세그먼트당 수 초 × 최대 8펄스 + CPU 경합
	// 여유라 통클립용 timeout 으로는 부족하다.
	segClient *http.Client
}

// NewLRCNClient constructs a client with sensible defaults.
func NewLRCNClient(cfg LRCNConfig) *LRCNClient {
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8081"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	return &LRCNClient{
		endpoint:  endpoint,
		client:    &http.Client{Timeout: timeout},
		segClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// LRCNRequest mirrors services/lrcn/server.py:ScoreRequest.
type LRCNRequest struct {
	ClipPath     string `json:"clip_path,omitempty"`
	ClipBytesB64 string `json:"clip_bytes_b64,omitempty"`
	TankID       string `json:"tank_id,omitempty"`
}

// LRCNResponse mirrors services/lrcn/server.py:ScoreResponse.
type LRCNResponse struct {
	FeedingActivityScore float64 `json:"feeding_activity_score"`
	ModelVersion         string  `json:"model_version"`
	InferenceMs          int     `json:"inference_ms"`
	FrameCount           int     `json:"frame_count"`
	TankID               string  `json:"tank_id,omitempty"`
}

// Score sends a clip reference to the LRCN service and returns the activity score.
//
// Either ClipPath (local file on the shared host) or ClipBytesB64 (base64
// payload) must be provided. The endpoint returns 503 if the model has not
// loaded weights yet — this is a legitimate state before the Gangneung
// bootstrap training completes (cold start window).
func (c *LRCNClient) Score(ctx context.Context, req LRCNRequest) (LRCNResponse, error) {
	if strings.TrimSpace(req.ClipPath) == "" && strings.TrimSpace(req.ClipBytesB64) == "" {
		return LRCNResponse{}, errors.New("lrcn: clip_path or clip_bytes_b64 required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return LRCNResponse{}, fmt.Errorf("lrcn: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint+"/v1/feeding-score", bytes.NewReader(body),
	)
	if err != nil {
		return LRCNResponse{}, fmt.Errorf("lrcn: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return LRCNResponse{}, fmt.Errorf("lrcn: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return LRCNResponse{}, ErrLRCNModelNotReady
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return LRCNResponse{}, fmt.Errorf(
			"lrcn: status %d body=%s",
			resp.StatusCode, strings.TrimSpace(string(bodyBytes)),
		)
	}

	var out LRCNResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return LRCNResponse{}, fmt.Errorf("lrcn: decode response: %w", err)
	}
	// Defensive clamp — server should already return [0, 1] but we never trust
	// inference outputs unconditionally.
	if out.FeedingActivityScore < 0 {
		out.FeedingActivityScore = 0
	}
	if out.FeedingActivityScore > 1 {
		out.FeedingActivityScore = 1
	}
	return out, nil
}

// LRCNSegmentWindow mirrors services/lrcn/server.py:SegmentWindow.
type LRCNSegmentWindow struct {
	PulseNo       int `json:"pulse_no"`
	OffsetStartMs int `json:"offset_start_ms"`
	OffsetEndMs   int `json:"offset_end_ms"`
}

// LRCNSegmentScoresRequest mirrors services/lrcn/server.py:SegmentScoresRequest.
type LRCNSegmentScoresRequest struct {
	ClipPath  string              `json:"clip_path"`
	Segments  []LRCNSegmentWindow `json:"segments"`
	TankID    string              `json:"tank_id,omitempty"`
	MaxFrames int                 `json:"max_frames,omitempty"`
}

// LRCNSegmentScore mirrors services/lrcn/server.py:SegmentScore.
// FeedingScore 가 nil 이면 해당 세그먼트 채점 실패 (Error 에 사유).
type LRCNSegmentScore struct {
	PulseNo      int      `json:"pulse_no"`
	FeedingScore *float64 `json:"feeding_score"`
	Error        string   `json:"error,omitempty"`
}

// LRCNSegmentScoresResponse mirrors services/lrcn/server.py:SegmentScoresResponse.
type LRCNSegmentScoresResponse struct {
	ModelVersion string             `json:"model_version"`
	Scores       []LRCNSegmentScore `json:"scores"`
	InferenceMs  int                `json:"inference_ms"`
}

// SegmentScores scores per-pulse segment windows of a clip with the 2-head
// segments model (feeding head). Advisory only — 모달 프리필/학습 보조 신호.
func (c *LRCNClient) SegmentScores(ctx context.Context, req LRCNSegmentScoresRequest) (LRCNSegmentScoresResponse, error) {
	if strings.TrimSpace(req.ClipPath) == "" || len(req.Segments) == 0 {
		return LRCNSegmentScoresResponse{}, errors.New("lrcn: clip_path and segments required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return LRCNSegmentScoresResponse{}, fmt.Errorf("lrcn: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint+"/v1/segment-scores", bytes.NewReader(body),
	)
	if err != nil {
		return LRCNSegmentScoresResponse{}, fmt.Errorf("lrcn: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.segClient.Do(httpReq)
	if err != nil {
		return LRCNSegmentScoresResponse{}, fmt.Errorf("lrcn: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return LRCNSegmentScoresResponse{}, ErrLRCNModelNotReady
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return LRCNSegmentScoresResponse{}, fmt.Errorf(
			"lrcn: status %d body=%s",
			resp.StatusCode, strings.TrimSpace(string(bodyBytes)),
		)
	}

	var out LRCNSegmentScoresResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return LRCNSegmentScoresResponse{}, fmt.Errorf("lrcn: decode response: %w", err)
	}
	// Defensive clamp — 서버가 [0,1] 을 보장해야 하지만 추론 출력은 무조건 신뢰하지 않는다.
	for i := range out.Scores {
		if s := out.Scores[i].FeedingScore; s != nil {
			if *s < 0 {
				zero := 0.0
				out.Scores[i].FeedingScore = &zero
			} else if *s > 1 {
				one := 1.0
				out.Scores[i].FeedingScore = &one
			}
		}
	}
	return out, nil
}

// Ready calls GET /readyz on the LRCN service.
//
// Returns (true, modelVersion, nil) when the service has loaded weights.
// Returns (false, "", nil) when the service is alive but weights are not yet
// loaded (cold-start). Network/protocol failures are returned as errors.
func (c *LRCNClient) Ready(ctx context.Context) (bool, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/readyz", nil)
	if err != nil {
		return false, "", fmt.Errorf("lrcn: create readyz request: %w", err)
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return false, "", fmt.Errorf("lrcn: readyz request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return false, "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("lrcn: readyz status %d", resp.StatusCode)
	}
	var body struct {
		Status       string `json:"status"`
		ModelVersion string `json:"model_version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, "", fmt.Errorf("lrcn: decode readyz: %w", err)
	}
	return body.Status == "ready", body.ModelVersion, nil
}

// ErrLRCNModelNotReady is returned when the LRCN service is alive but has not
// loaded inference weights yet (typical during the Gangneung bootstrap window).
var ErrLRCNModelNotReady = errors.New("lrcn: model not ready")
