package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ManifestPath is where active vision algorithm weights and rollback history
// are tracked. It is the single source of truth for "which model is currently
// in production". The example YAML config remains read-only example data.
const ManifestPath = "local-ai/models/active/manifest.json"

// AlgorithmActiveState is the per-algorithm record of what's promoted now,
// plus a small history that supports rollback.
type AlgorithmActiveState struct {
	ActiveWeightsPath string         `json:"active_weights_path"`
	ActiveJobID       string         `json:"active_job_id,omitempty"`
	AppliedAt         string         `json:"applied_at,omitempty"`
	OperatorID        string         `json:"operator_id,omitempty"`
	History           []HistoryEntry `json:"history,omitempty"`
}

type HistoryEntry struct {
	WeightsPath string `json:"weights_path"`
	JobID       string `json:"job_id,omitempty"`
	AppliedAt   string `json:"applied_at,omitempty"`
	RemovedAt   string `json:"removed_at,omitempty"`
	OperatorID  string `json:"operator_id,omitempty"`
}

// Manifest is the on-disk JSON shape.
//
// Algorithms          — vision YOLO 같은 algorithm-level 모델 (Phase 1 vision)
// TankBaselines       — Cage/Tank별 autoencoder baseline (Phase 1 tank-baseline)
// TankWaterForecasts  — Cage/Tank별 단기 수질 예측 (Phase 2 water-forecast)
//
// docs/29 §3.0 "각 수조는 독립" 원칙: 모든 tank_*  맵은 tank_id 가 키.
// 향후 모델 kind 가 늘어나면 (tank_id, kind) 의 nested 구조로 통합 후보.
type Manifest struct {
	Algorithms         map[string]AlgorithmActiveState `json:"algorithms"`
	TankBaselines      map[string]AlgorithmActiveState `json:"tank_baselines,omitempty"`
	TankWaterForecasts map[string]AlgorithmActiveState `json:"tank_water_forecasts,omitempty"`
	UpdatedAt          string                          `json:"updated_at"`
}

// ErrNoHistory is returned when rollback is requested but the algorithm has
// no prior weights to fall back to.
var ErrNoHistory = errors.New("vision: no rollback history for algorithm")

var manifestMu sync.Mutex

// LoadManifest reads the manifest file, returning an empty (zero-value but
// initialized) manifest if none exists yet. Safe under concurrent calls.
func LoadManifest() (*Manifest, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	return loadManifestLocked()
}

func loadManifestLocked() (*Manifest, error) {
	m := &Manifest{
		Algorithms:         map[string]AlgorithmActiveState{},
		TankBaselines:      map[string]AlgorithmActiveState{},
		TankWaterForecasts: map[string]AlgorithmActiveState{},
	}
	data, err := os.ReadFile(ManifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Algorithms == nil {
		m.Algorithms = map[string]AlgorithmActiveState{}
	}
	if m.TankBaselines == nil {
		m.TankBaselines = map[string]AlgorithmActiveState{}
	}
	if m.TankWaterForecasts == nil {
		m.TankWaterForecasts = map[string]AlgorithmActiveState{}
	}
	return m, nil
}

// saveManifest atomically writes the manifest. Caller must hold manifestMu.
func saveManifestLocked(m *Manifest) error {
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.MkdirAll(filepath.Dir(ManifestPath), 0o755); err != nil {
		return fmt.Errorf("mkdir manifest: %w", err)
	}
	tmp := ManifestPath + ".tmp"
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, ManifestPath); err != nil {
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}

// Promote sets a new active weights path for the algorithm and pushes the
// previous one (if any) into history. Validates the candidate file exists.
func Promote(algorithmID, candidatePath, jobID, operatorID string) (*AlgorithmActiveState, error) {
	if algorithmID == "" {
		return nil, errors.New("algorithm_id is required")
	}
	if candidatePath == "" {
		return nil, errors.New("candidate weights path is required")
	}
	if _, err := os.Stat(candidatePath); err != nil {
		return nil, fmt.Errorf("candidate weights not found: %w", err)
	}
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cur := m.Algorithms[algorithmID]
	if cur.ActiveWeightsPath != "" {
		cur.History = append(cur.History, HistoryEntry{
			WeightsPath: cur.ActiveWeightsPath,
			JobID:       cur.ActiveJobID,
			AppliedAt:   cur.AppliedAt,
			RemovedAt:   now,
			OperatorID:  cur.OperatorID,
		})
	}
	cur.ActiveWeightsPath = candidatePath
	cur.ActiveJobID = jobID
	cur.AppliedAt = now
	cur.OperatorID = operatorID
	m.Algorithms[algorithmID] = cur
	if err := saveManifestLocked(m); err != nil {
		return nil, err
	}
	state := cur
	return &state, nil
}

// Rollback restores the most recent prior weights for the algorithm. The
// currently-active weights are discarded (not pushed back to history) since
// rollback is "the new model is bad, go back". Returns ErrNoHistory if the
// algorithm has nothing to fall back to.
func Rollback(algorithmID, operatorID string) (*AlgorithmActiveState, error) {
	if algorithmID == "" {
		return nil, errors.New("algorithm_id is required")
	}
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return nil, err
	}
	cur, ok := m.Algorithms[algorithmID]
	if !ok || len(cur.History) == 0 {
		return nil, ErrNoHistory
	}
	prev := cur.History[len(cur.History)-1]
	cur.History = cur.History[:len(cur.History)-1]
	cur.ActiveWeightsPath = prev.WeightsPath
	cur.ActiveJobID = prev.JobID
	cur.AppliedAt = time.Now().UTC().Format(time.RFC3339Nano)
	cur.OperatorID = operatorID
	m.Algorithms[algorithmID] = cur
	if err := saveManifestLocked(m); err != nil {
		return nil, err
	}
	state := cur
	return &state, nil
}

// ActiveState returns the current active record (or zero value if none).
func ActiveState(algorithmID string) (AlgorithmActiveState, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return AlgorithmActiveState{}, err
	}
	return m.Algorithms[algorithmID], nil
}

// PromoteTankBaseline registers a freshly trained baseline model for a tank.
// modelDir must contain model.pt + feature_spec.json + baseline_stats.json.
// Unlike vision Promote, baseline 은 비-actuating (verdict 만 산출) 이라
// 자동 promote 가 안전. 운영자가 명시적 rollback 으로 이전 모델 복원 가능.
func PromoteTankBaseline(tankID, modelDir, jobID, operatorID string) (*AlgorithmActiveState, error) {
	if tankID == "" {
		return nil, errors.New("tank_id is required")
	}
	if modelDir == "" {
		return nil, errors.New("model_dir is required")
	}
	if _, err := os.Stat(filepath.Join(modelDir, "model.pt")); err != nil {
		return nil, fmt.Errorf("baseline model.pt not found in %s: %w", modelDir, err)
	}
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cur := m.TankBaselines[tankID]
	if cur.ActiveWeightsPath != "" {
		cur.History = append(cur.History, HistoryEntry{
			WeightsPath: cur.ActiveWeightsPath,
			JobID:       cur.ActiveJobID,
			AppliedAt:   cur.AppliedAt,
			RemovedAt:   now,
			OperatorID:  cur.OperatorID,
		})
	}
	cur.ActiveWeightsPath = modelDir
	cur.ActiveJobID = jobID
	cur.AppliedAt = now
	cur.OperatorID = operatorID
	m.TankBaselines[tankID] = cur
	if err := saveManifestLocked(m); err != nil {
		return nil, err
	}
	state := cur
	return &state, nil
}

// ActiveTankBaseline returns the current active baseline for a tank.
func ActiveTankBaseline(tankID string) (AlgorithmActiveState, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return AlgorithmActiveState{}, err
	}
	return m.TankBaselines[tankID], nil
}

// PromoteTankWaterForecast registers a freshly trained water-forecast model for a tank.
// Same semantics as PromoteTankBaseline (auto-promote 안전 — 비-actuating 신호).
func PromoteTankWaterForecast(tankID, modelDir, jobID, operatorID string) (*AlgorithmActiveState, error) {
	if tankID == "" {
		return nil, errors.New("tank_id is required")
	}
	if modelDir == "" {
		return nil, errors.New("model_dir is required")
	}
	if _, err := os.Stat(filepath.Join(modelDir, "model.pt")); err != nil {
		return nil, fmt.Errorf("forecast model.pt not found in %s: %w", modelDir, err)
	}
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cur := m.TankWaterForecasts[tankID]
	if cur.ActiveWeightsPath != "" {
		cur.History = append(cur.History, HistoryEntry{
			WeightsPath: cur.ActiveWeightsPath,
			JobID:       cur.ActiveJobID,
			AppliedAt:   cur.AppliedAt,
			RemovedAt:   now,
			OperatorID:  cur.OperatorID,
		})
	}
	cur.ActiveWeightsPath = modelDir
	cur.ActiveJobID = jobID
	cur.AppliedAt = now
	cur.OperatorID = operatorID
	m.TankWaterForecasts[tankID] = cur
	if err := saveManifestLocked(m); err != nil {
		return nil, err
	}
	state := cur
	return &state, nil
}

// ActiveTankWaterForecast returns the current active forecast model for a tank.
func ActiveTankWaterForecast(tankID string) (AlgorithmActiveState, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return AlgorithmActiveState{}, err
	}
	return m.TankWaterForecasts[tankID], nil
}

// RollbackTankBaseline restores the most recent prior baseline for a tank.
func RollbackTankBaseline(tankID, operatorID string) (*AlgorithmActiveState, error) {
	if tankID == "" {
		return nil, errors.New("tank_id is required")
	}
	manifestMu.Lock()
	defer manifestMu.Unlock()
	m, err := loadManifestLocked()
	if err != nil {
		return nil, err
	}
	cur, ok := m.TankBaselines[tankID]
	if !ok || len(cur.History) == 0 {
		return nil, ErrNoHistory
	}
	prev := cur.History[len(cur.History)-1]
	cur.History = cur.History[:len(cur.History)-1]
	cur.ActiveWeightsPath = prev.WeightsPath
	cur.ActiveJobID = prev.JobID
	cur.AppliedAt = time.Now().UTC().Format(time.RFC3339Nano)
	cur.OperatorID = operatorID
	m.TankBaselines[tankID] = cur
	if err := saveManifestLocked(m); err != nil {
		return nil, err
	}
	state := cur
	return &state, nil
}
