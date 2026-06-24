package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

// TestTankStateVectorIDFromPath — 라우트 매칭 helper 동작 확인.
func TestTankStateVectorIDFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/v1/tanks/tank_01/state-vector", "tank_01"},
		{"/v1/tanks/tank_with_underscore/state-vector", "tank_with_underscore"},
		{"/v1/tanks/tank_01/state", ""},   // /state 만 — vector 아님
		{"/v1/tanks/tank_01/profile", ""}, // /profile — vector 아님
		{"/v1/tanks/", ""},                // 빈 id
		{"/something/else", ""},
	}
	for _, c := range cases {
		if got := tankStateVectorIDFromPath(c.path); got != c.want {
			t.Errorf("path %q: got %q, want %q", c.path, got, c.want)
		}
	}
}

// findMigDir — 테스트가 어느 디렉터리에서 돌든 migrations/ 디렉터리를 찾는다.
func findMigDir(t *testing.T) string {
	t.Helper()
	p, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(p, "migrations")
		if _, err := os.Stat(filepath.Join(candidate, "001_init.sql")); err == nil {
			return candidate
		}
		p = filepath.Dir(p)
	}
	t.Fatal("migrations/ not found from any parent")
	return ""
}

func newTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	migDir := findMigDir(t)
	for _, name := range []string{"001_init.sql", "002_autonomous_mode.sql", "003_lifecycle.sql", "004_sampling.sql", "005_fcr_calibration.sql", "006_decision_policy.sql", "007_weight_history.sql", "008_groups.sql", "009_farms_sites.sql", "010_water_treatment_groups.sql", "011_controllers.sql", "012_actuators_sensors.sql", "013_species_profiles.sql", "014_feeding_schedules.sql", "015_predictive_events.sql", "016_learned_safety.sql", "017_arbiter_decisions.sql", "018_arbiter_preemption.sql", "019_feed_cycle_intent.sql", "020_sync_batch_event_seq_index.sql", "021_tank_physical_dimensions.sql", "022_camera_models.sql", "023_camera_view_geometry.sql", "024_sensor_models.sql", "025_actuator_models.sql", "029_actuator_model_category_specs.sql", "030_traceability_lifecycle.sql", "031_species_fao.sql", "032_documents.sql", "033_inventory.sql", "034_document_subject.sql", "035_partners.sql", "036_site_trade.sql"} {
		if err := storage.Migrate(st, filepath.Join(migDir, name)); err != nil {
			t.Fatalf("migrate %s: %v", name, err)
		}
	}
	return st
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	return &Server{cfg: cfg, store: newTestStore(t)}
}

// TestTankStateVectorEmptyTank — 빈 Cage/Tank (등록만 되고 데이터 없음) 도 정상 응답 +
// 미구현 영역에 대한 안내 notes 가 채워진다.
func TestTankStateVectorEmptyTank(t *testing.T) {
	s := newTestServer(t)
	if err := s.store.UpsertTankProfile(context.Background(), &storage.TankProfile{
		TankID:      "tank_empty",
		DisplayName: "Empty Tank",
		Species:     "참돔",
		SystemType:  "RAS",
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/tanks/tank_empty/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_empty")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if v.TankID != "tank_empty" {
		t.Errorf("tank_id: %q", v.TankID)
	}
	if v.Timestamp == "" {
		t.Error("timestamp empty")
	}

	// Biological context 는 profile 에서 채워진다
	if v.BiologicalContext.Species != "참돔" {
		t.Errorf("species: %q", v.BiologicalContext.Species)
	}
	if v.BiologicalContext.Source != "tank_profile" {
		t.Errorf("source: %q", v.BiologicalContext.Source)
	}

	// Fish 는 비어 있고 notes 에 사유 명시
	if v.Fish.ActivityScore != nil {
		t.Error("expected nil ActivityScore for empty tank")
	}
	if len(v.Fish.Notes) == 0 {
		t.Error("expected fish notes explaining missing data")
	}

	// Water 도 비어 있고 notes 에 forecast 관련 안내 (모델 없음 또는 이벤트 없음)
	if len(v.Water.Metrics) != 0 {
		t.Errorf("expected empty water metrics, got %d", len(v.Water.Metrics))
	}
	hasForecastNote := false
	for _, n := range v.Water.Notes {
		if strings.Contains(n, "forecast") || strings.Contains(n, "예측") || strings.Contains(n, "water-forecast") {
			hasForecastNote = true
			break
		}
	}
	if !hasForecastNote {
		t.Errorf("expected forecast-related note in water notes, got: %v", v.Water.Notes)
	}

	// Predictions 는 미배포 상태
	if v.Water.Predictions.Available {
		t.Error("expected Predictions.Available=false in Phase 0")
	}

	// Equipment 도 비어 있고 unknown
	if v.Equipment.HealthSummary != "unknown" {
		t.Errorf("health_summary: %q", v.Equipment.HealthSummary)
	}

	// Confidence 는 cold (active weights 없음)
	if v.Confidence.AdaptationLevel != "cold" {
		t.Errorf("adaptation_level: %q", v.Confidence.AdaptationLevel)
	}
	if v.Confidence.HasActiveWeights {
		t.Error("expected HasActiveWeights=false")
	}
}

// TestTankStateVectorJSONSerializable — UI 가 받을 형식이 JSON 으로 직렬화 가능.
func TestTankStateVectorJSONSerializable(t *testing.T) {
	s := newTestServer(t)
	if err := s.store.UpsertTankProfile(context.Background(), &storage.TankProfile{
		TankID: "tank_json", DisplayName: "JSON Tank", Species: "조피볼락",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	req := httptest.NewRequest("GET", "/v1/tanks/tank_json/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_json")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Top-level keys 검증
	for _, k := range []string{
		"tank_id", "timestamp",
		`"fish":`, `"water":`, `"equipment":`,
		`"feeding":`, `"biological_context":`, `"confidence":`,
	} {
		if !strings.Contains(string(body), k) {
			t.Errorf("missing key %q in JSON: %s", k, string(body))
		}
	}
}

// TestTankStateVectorIncludesAnomaly — Anomaly 섹션이 state vector 에 포함되며,
// 모델이 없으면 has_model=false + notes 안내.
func TestTankStateVectorIncludesAnomaly(t *testing.T) {
	s := newTestServer(t)
	if err := s.store.UpsertTankProfile(context.Background(), &storage.TankProfile{
		TankID: "tank_a", DisplayName: "A", Species: "참돔",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	req := httptest.NewRequest("GET", "/v1/tanks/tank_a/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_a")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if v.Anomaly.HasModel {
		t.Error("expected HasModel=false for new tank")
	}
	if len(v.Anomaly.Notes) == 0 {
		t.Error("expected notes explaining missing baseline model")
	}
	body, _ := json.Marshal(v)
	if !strings.Contains(string(body), `"anomaly":`) {
		t.Errorf("missing anomaly key in JSON: %s", string(body))
	}
}

// TestTankStateVectorMissingProfile — 등록 안 된 Cage/Tank도 에러 없이 응답.
// (미등록 Cage/Tank에 대한 상태 조회는 향후 입식 메타데이터 입력 전 유스케이스.)
func TestTankStateVectorMissingProfile(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/v1/tanks/tank_unknown/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_unknown")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if v.TankID != "tank_unknown" {
		t.Errorf("tank_id: %q", v.TankID)
	}
	hasMissingNote := false
	for _, n := range v.BiologicalContext.Notes {
		if strings.Contains(n, "TankProfile") {
			hasMissingNote = true
			break
		}
	}
	if !hasMissingNote {
		t.Error("expected note about missing TankProfile")
	}
}
