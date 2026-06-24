// Package inference provides local advisory AI analysis adapters.
//
// Inference results are observations and recommendations only. They must not
// directly control feeding or equipment.
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	KindSensorAnomaly = "sensor_anomaly"
	KindVisionReview  = "vision_review"
	KindLogReview     = "log_review"
)

type Request struct {
	TankID  string         `json:"tank_id"`
	Kind    string         `json:"kind"`
	Context map[string]any `json:"context,omitempty"`
}

type Result struct {
	Observation       string   `json:"observation"`
	Confidence        float64  `json:"confidence"`
	Reasons           []string `json:"reasons,omitempty"`
	RecommendedAction string   `json:"recommended_action,omitempty"`
	SafetyFlags       []string `json:"safety_flags,omitempty"`
	EvidenceRefs      []string `json:"evidence_refs,omitempty"`

	// ControlAllowed is deliberately forced false. AI inference may inform
	// deterministic safety gates and operator review, but it cannot approve
	// feeding or equipment control by itself.
	ControlAllowed bool `json:"control_allowed"`
}

type OllamaConfig struct {
	Endpoint string
	Model    string
	Timeout  time.Duration
}

type OllamaAnalyzer struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewOllamaAnalyzer(cfg OllamaConfig) *OllamaAnalyzer {
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11435"
	}
	model := cfg.Model
	if model == "" {
		model = "gemma4:26b"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &OllamaAnalyzer{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{Timeout: timeout},
	}
}

func (a *OllamaAnalyzer) Analyze(ctx context.Context, req Request) (Result, error) {
	if strings.TrimSpace(req.TankID) == "" {
		return Result{}, errors.New("inference: tank_id is required")
	}
	if strings.TrimSpace(req.Kind) == "" {
		return Result{}, errors.New("inference: kind is required")
	}

	promptBytes, err := json.Marshal(map[string]any{
		"instruction": "Return only compact JSON matching this schema: observation string, confidence number 0..1, reasons string array, recommended_action string, safety_flags string array, evidence_refs string array. This is advisory only; never approve direct control.",
		"request":     req,
	})
	if err != nil {
		return Result{}, fmt.Errorf("inference: prompt marshal failed: %w", err)
	}

	body, err := json.Marshal(map[string]any{
		"model":  a.model,
		"prompt": string(promptBytes),
		"stream": false,
		"think":  false,
		"options": map[string]any{
			"temperature": 0,
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("inference: request marshal failed: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("inference: create request failed: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("inference: ollama request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("inference: ollama returned status %d", resp.StatusCode)
	}

	var ollamaResp struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return Result{}, fmt.Errorf("inference: ollama response decode failed: %w", err)
	}

	var result Result
	if err := json.Unmarshal([]byte(strings.TrimSpace(ollamaResp.Response)), &result); err != nil {
		return Result{}, fmt.Errorf("inference: analysis JSON decode failed: %w", err)
	}
	result.ControlAllowed = false
	if result.Observation == "" {
		return Result{}, errors.New("inference: analysis observation is required")
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	return result, nil
}
