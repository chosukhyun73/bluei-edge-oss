// Package baseline provides shared utilities for tank baseline scoring:
// the subprocess invocation that runs the python score helper, plus a
// background Worker that periodically scores all tanks with active baseline
// models.
//
// 책임 분리:
//   - Scorer  : 한 Cage/Tank + 한 모델에 대해 subprocess 호출 → 결과 반환
//   - Worker  : 모든 Cage/Tank에 대해 주기적 score 실행 → events 적재
//   - API 핸들러 (internal/api): 운영자 즉시 평가 요청 시 Scorer 직접 호출
//
// docs/29 §3.0 "각 수조는 독립" 원칙: Cage/Tank별로 독립된 모델/스코어. 한 Cage/Tank
// 실패가 다른 Cage/Tank에 영향 X.
package baseline

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// DefaultScriptPath — tank_baseline_score.py 의 기본 위치 (CWD 기준).
const DefaultScriptPath = "local-ai/training/tank_baseline_score.py"

// DefaultForecastScriptPath — water_forecast_predict.py 위치.
const DefaultForecastScriptPath = "local-ai/training/water_forecast_predict.py"

// DefaultTimeout — 한 score subprocess 가 허용하는 최대 시간.
const DefaultTimeout = 60 * time.Second

// ScoreResult — tank_baseline_score.py 의 stdout JSON 과 1:1 매칭.
type ScoreResult struct {
	TankID       string             `json:"tank_id"`
	AnomalyScore float64            `json:"anomaly_score"`
	P95Threshold float64            `json:"p95_threshold"`
	P99Threshold float64            `json:"p99_threshold"`
	Verdict      string             `json:"verdict"`
	FeatureDiff  map[string]float64 `json:"feature_diff"`
	EvaluatedAt  string             `json:"evaluated_at"`
}

// ForecastResult — water_forecast_predict.py 의 stdout JSON.
type ForecastResult struct {
	TankID          string    `json:"tank_id"`
	TargetMetric    string    `json:"target_metric"`
	CurrentValue    *float64  `json:"current_value"`
	HorizonMinutes  []int     `json:"horizon_minutes"`
	PredictedValues []float64 `json:"predicted_values"`
	EvaluatedAt     string    `json:"evaluated_at"`
}

// Scorer encapsulates the python subprocess call. Reusable across API handler
// (on-demand) and Worker (periodic).
type Scorer struct {
	ScriptPath         string // baseline 스크립트
	ForecastScriptPath string // forecast 스크립트
	PythonBin          string
	DBPath             string
	Timeout            time.Duration
}

// NewScorer constructs a Scorer with sensible defaults; empty fields are
// filled in.
func NewScorer(dbPath string) *Scorer {
	return &Scorer{
		ScriptPath:         DefaultScriptPath,
		ForecastScriptPath: DefaultForecastScriptPath,
		PythonBin:          "python3",
		DBPath:             dbPath,
		Timeout:            DefaultTimeout,
	}
}

// Score runs the python helper for one tank and parses the JSON response.
func (s *Scorer) Score(ctx context.Context, tankID, modelDir string) (*ScoreResult, error) {
	if tankID == "" {
		return nil, fmt.Errorf("tank_id is required")
	}
	if modelDir == "" {
		return nil, fmt.Errorf("model_dir is required")
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin := s.PythonBin
	if bin == "" {
		bin = "python3"
	}
	script := s.ScriptPath
	if script == "" {
		script = DefaultScriptPath
	}
	cmd := exec.CommandContext(subCtx, bin, script,
		"--model-dir", modelDir,
		"--db", s.DBPath,
		"--tank-id", tankID,
		"--days", "1")
	stdout, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return nil, fmt.Errorf("score subprocess: %w (stderr: %s)", err, truncate(stderr, 200))
	}
	var sr ScoreResult
	if err := json.Unmarshal(stdout, &sr); err != nil {
		return nil, fmt.Errorf("score output parse: %w (raw: %s)", err, truncate(string(stdout), 200))
	}
	return &sr, nil
}

// Forecast runs the python forecast helper for one tank and parses the JSON.
func (s *Scorer) Forecast(ctx context.Context, tankID, modelDir string) (*ForecastResult, error) {
	if tankID == "" {
		return nil, fmt.Errorf("tank_id is required")
	}
	if modelDir == "" {
		return nil, fmt.Errorf("model_dir is required")
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bin := s.PythonBin
	if bin == "" {
		bin = "python3"
	}
	script := s.ForecastScriptPath
	if script == "" {
		script = DefaultForecastScriptPath
	}
	cmd := exec.CommandContext(subCtx, bin, script,
		"--model-dir", modelDir,
		"--db", s.DBPath,
		"--tank-id", tankID,
		"--days", "1")
	stdout, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		return nil, fmt.Errorf("forecast subprocess: %w (stderr: %s)", err, truncate(stderr, 200))
	}
	var fr ForecastResult
	if err := json.Unmarshal(stdout, &fr); err != nil {
		return nil, fmt.Errorf("forecast output parse: %w (raw: %s)", err, truncate(string(stdout), 200))
	}
	return &fr, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
