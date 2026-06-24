package baseline_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/vision"

	_ "modernc.org/sqlite"
)

// rawDB — test.db 파일에 직접 접근해 INSERT 가 가능한 핸들 반환.
func rawDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("rawDB open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// insertBaselineEvent — tank.baseline.scored 이벤트를 DB 에 직접 삽입.
// recordedAt 을 명시적으로 지정 가능해야 하므로 raw SQL 사용.
func insertBaselineEvent(t *testing.T, db *sql.DB, tankID, verdict string, score float64, at time.Time) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"tank_id":%q,"model_dir":"fake","anomaly_score":%f,"p95_threshold":0.5,"p99_threshold":0.9,"verdict":%q,"evaluated_at":%q}`,
		tankID, score, verdict, at.UTC().Format(time.RFC3339Nano),
	)
	atStr := at.UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`
		INSERT INTO events (event_id, event_type, schema_version, site_id, edge_id,
		                    source_module, source_device_id, payload_json, event_json, recorded_at)
		VALUES (lower(hex(randomblob(8))), 'tank.baseline.scored', '1.0', 'site_t', 'edge_t',
		        'test', ?, ?, '{}', ?)`,
		tankID, payload, atStr,
	)
	if err != nil {
		t.Fatalf("insertBaselineEvent: %v", err)
	}
}

// insertForecastEvent — water.forecast.recorded 이벤트를 직접 삽입.
func insertForecastEvent(t *testing.T, db *sql.DB, tankID string, predictedAt30 float64, at time.Time) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"tank_id":%q,"model_dir":"fake","metric":"dissolved_oxygen","horizon_minutes":[10,30,60],"predicted_values":[6.4,%f,6.6],"evaluated_at":%q}`,
		tankID, predictedAt30, at.UTC().Format(time.RFC3339Nano),
	)
	atStr := at.UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`
		INSERT INTO events (event_id, event_type, schema_version, site_id, edge_id,
		                    source_module, source_device_id, payload_json, event_json, recorded_at)
		VALUES (lower(hex(randomblob(8))), 'water.forecast.recorded', '1.0', 'site_t', 'edge_t',
		        'test', ?, ?, '{}', ?)`,
		tankID, payload, atStr,
	)
	if err != nil {
		t.Fatalf("insertForecastEvent: %v", err)
	}
}

// insertSensorEvent — sensor.reading.recorded 이벤트를 직접 삽입.
func insertSensorEvent(t *testing.T, db *sql.DB, tankID, metric string, value float64, observedAt time.Time) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"reading_id":"r_%d","sensor_id":"s1","device_id":"d1","metric":%q,"value":%f,"unit":"mg/L","quality":"ok","observed_at":%q,"location":{"tank_id":%q}}`,
		observedAt.UnixNano(), metric, value, observedAt.UTC().Format(time.RFC3339Nano), tankID,
	)
	atStr := observedAt.UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`
		INSERT INTO events (event_id, event_type, schema_version, site_id, edge_id,
		                    source_module, source_device_id, payload_json, event_json, recorded_at)
		VALUES (lower(hex(randomblob(8))), 'sensor.reading.recorded', '1.0', 'site_t', 'edge_t',
		        'test', ?, ?, '{}', ?)`,
		tankID, payload, atStr,
	)
	if err != nil {
		t.Fatalf("insertSensorEvent: %v", err)
	}
}

// TestComputeConfidenceEmptyTank — 이벤트 없음, manifest 없음 → composite ≈ 0, cold.
func TestComputeConfidenceEmptyTank(t *testing.T) {
	_, st := newTestEnv(t)
	c, err := baseline.ComputeTankConfidence(context.Background(), st, "tank_empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Composite > 0.01 {
		t.Errorf("expected composite ≈ 0, got %.4f", c.Composite)
	}
	if c.AdaptationLevel != "cold" {
		t.Errorf("expected cold, got %s", c.AdaptationLevel)
	}
}

// TestComputeConfidenceForecastAccuracyHigh — 작은 오차 페어 → ForecastAccuracy > 0.95.
func TestComputeConfidenceForecastAccuracyHigh(t *testing.T) {
	_, st := newTestEnv(t)

	// DB 직접 접근
	tmp, _ := os.Getwd()
	db := rawDB(t, filepath.Join(tmp, "test.db"))

	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		evalAt := now.Add(-time.Duration(i+1) * 2 * time.Hour)
		// 예측: 6.5, 실측: 6.51 → 오차 0.01
		insertForecastEvent(t, db, "tank_hi", 6.5, evalAt)
		insertSensorEvent(t, db, "tank_hi", "dissolved_oxygen", 6.51, evalAt.Add(30*time.Minute))
	}

	c, err := baseline.ComputeTankConfidence(context.Background(), st, "tank_hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ForecastAccuracy <= 0.95 {
		t.Errorf("expected ForecastAccuracy > 0.95, got %.4f", c.ForecastAccuracy)
	}
}

// TestComputeConfidenceForecastAccuracyPoor — 큰 오차 (1.5 mg/L) → ForecastAccuracy = 0 (클램프).
func TestComputeConfidenceForecastAccuracyPoor(t *testing.T) {
	_, st := newTestEnv(t)

	tmp, _ := os.Getwd()
	db := rawDB(t, filepath.Join(tmp, "test.db"))

	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		evalAt := now.Add(-time.Duration(i+1) * 2 * time.Hour)
		// 예측: 6.5, 실측: 5.0 → 오차 1.5 mg/L
		insertForecastEvent(t, db, "tank_poor", 6.5, evalAt)
		insertSensorEvent(t, db, "tank_poor", "dissolved_oxygen", 5.0, evalAt.Add(30*time.Minute))
	}

	c, err := baseline.ComputeTankConfidence(context.Background(), st, "tank_poor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ForecastAccuracy != 0 {
		t.Errorf("expected ForecastAccuracy = 0, got %.4f", c.ForecastAccuracy)
	}
}

// TestComputeConfidenceBaselineStabilityHigh — 안정된 normal scores → BaselineStability > 0.95.
func TestComputeConfidenceBaselineStabilityHigh(t *testing.T) {
	_, st := newTestEnv(t)

	tmp, _ := os.Getwd()
	db := rawDB(t, filepath.Join(tmp, "test.db"))

	now := time.Now().UTC()
	// anomaly_score = 0.1000 ± 0.0001 — CV ≈ 0.001 → stability > 0.99
	for i := 0; i < 20; i++ {
		score := 0.1000 + float64(i%3)*0.0001 // 0.1000, 0.1001, 0.1002, 반복
		at := now.Add(-time.Duration(i) * 30 * time.Minute)
		insertBaselineEvent(t, db, "tank_stable", "normal", score, at)
	}

	c, err := baseline.ComputeTankConfidence(context.Background(), st, "tank_stable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BaselineStability <= 0.95 {
		t.Errorf("expected BaselineStability > 0.95, got %.4f", c.BaselineStability)
	}
}

// TestComputeConfidenceAdaptationLevelBands — Composite 에 따라 올바른 레벨 반환.
func TestComputeConfidenceAdaptationLevelBands(t *testing.T) {
	cases := []struct {
		composite float64
		expected  string
	}{
		{0.0, "cold"},
		{0.29, "cold"},
		{0.30, "observation"},
		{0.59, "observation"},
		{0.60, "adapted"},
		{0.84, "adapted"},
		{0.85, "autonomous"},
		{1.0, "autonomous"},
	}
	for _, tc := range cases {
		got := baseline.AdaptationLevelFromComposite(tc.composite)
		if got != tc.expected {
			t.Errorf("composite=%.2f: expected %s, got %s", tc.composite, tc.expected, got)
		}
	}
}

// TestComputeConfidenceTrainingMaturity — baseline + forecast manifest 등록 → TrainingMaturity = 1.0.
func TestComputeConfidenceTrainingMaturity(t *testing.T) {
	_, st := newTestEnv(t)

	cwd, _ := os.Getwd()
	baseDir := filepath.Join(cwd, "fake_base_conf")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "model.pt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := vision.PromoteTankBaseline("tank_mature", baseDir, "job1", "test"); err != nil {
		t.Fatalf("PromoteTankBaseline: %v", err)
	}

	fcDir := filepath.Join(cwd, "fake_fc_conf")
	if err := os.MkdirAll(fcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fcDir, "model.pt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := vision.PromoteTankWaterForecast("tank_mature", fcDir, "job2", "test"); err != nil {
		t.Fatalf("PromoteTankWaterForecast: %v", err)
	}

	c, err := baseline.ComputeTankConfidence(context.Background(), st, "tank_mature")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.TrainingMaturity != 1.0 {
		t.Errorf("expected TrainingMaturity = 1.0, got %.2f", c.TrainingMaturity)
	}
	if !c.HasBaseline {
		t.Error("expected HasBaseline = true")
	}
	if !c.HasForecast {
		t.Error("expected HasForecast = true")
	}
}

// TestComputeConfidenceStalenessAdjustment — 오래된 이벤트만 있으면 composite 절반 + staleness 노트.
func TestComputeConfidenceStalenessAdjustment(t *testing.T) {
	_, st := newTestEnv(t)

	tmp, _ := os.Getwd()
	db := rawDB(t, filepath.Join(tmp, "test.db"))

	// 25시간 전 이벤트만 삽입 (staleness 유발)
	old := time.Now().UTC().Add(-25 * time.Hour)
	for i := 0; i < 20; i++ {
		at := old.Add(-time.Duration(i) * 10 * time.Minute)
		insertBaselineEvent(t, db, "tank_stale", "normal", 0.001, at)
	}

	// staleness 전 composite 기준값 계산 (training maturity 없이): BaselineStability 만 기여.
	// staleness 후에는 composite 가 절반이어야 함.
	c, err := baseline.ComputeTankConfidence(context.Background(), st, "tank_stale")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// staleness 노트 존재 확인
	hasStalenessNote := false
	for _, n := range c.Notes {
		if n == "최근 24시간 내 평가 이벤트 없음 — 점수 staleness 적용" {
			hasStalenessNote = true
			break
		}
	}
	if !hasStalenessNote {
		t.Errorf("expected staleness note, got notes: %v", c.Notes)
	}

	// BaselineStability 는 양수, Composite 는 절반 조정됐어야 함.
	// 조정 전 composite = 0.3 * BaselineStability (TrainingMaturity=0, ForecastAccuracy=0)
	// 조정 후 composite = 절반
	if c.BaselineStability <= 0 {
		t.Error("expected positive BaselineStability with 20 normal events")
	}
	expectedComposite := c.BaselineStability * 0.3 * 0.5
	if abs64(c.Composite-expectedComposite) > 0.01 {
		t.Errorf("staleness-adjusted composite expected ≈ %.4f, got %.4f", expectedComposite, c.Composite)
	}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
