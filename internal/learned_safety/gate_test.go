package learned_safety

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

func makeGate(enabled bool, rules ...*storage.LearnedRule) *Gate {
	g := &Gate{
		cfg:   config.LearnedSafetyConfig{Enabled: enabled},
		log:   slog.Default(),
		rules: rules,
	}
	return g
}

// gateMigSQL — Check() 테스트에 필요한 최소 스키마 (current_tank_environment + learned_rules).
const gateMigSQL = `
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS current_tank_environment (
  tank_id TEXT NOT NULL, metric TEXT NOT NULL,
  value REAL, unit TEXT NOT NULL, quality TEXT NOT NULL,
  sensor_id TEXT NOT NULL, device_id TEXT NOT NULL,
  last_event_id TEXT NOT NULL, observed_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, payload_json TEXT NOT NULL,
  PRIMARY KEY (tank_id, metric)
);
CREATE TABLE IF NOT EXISTS learned_rules (
  rule_id         TEXT PRIMARY KEY,
  condition_json  TEXT NOT NULL,
  severity        TEXT NOT NULL,
  source          TEXT NOT NULL,
  confidence      REAL NOT NULL DEFAULT 0,
  hit_count       INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL,
  last_matched_at TEXT,
  enabled         INTEGER NOT NULL DEFAULT 1
);
`

// openGateTestStore — 임시 SQLite DB + 최소 스키마를 생성하고 테스트 종료 시 정리.
func openGateTestStore(t *testing.T) storage.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gate_test.db")
	st, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	sqlFile := filepath.Join(dir, "gate_mig.sql")
	if err := os.WriteFile(sqlFile, []byte(gateMigSQL), 0o600); err != nil {
		t.Fatalf("write sql: %v", err)
	}
	if err := storage.Migrate(st, sqlFile); err != nil {
		t.Fatalf("storage.Migrate: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// seedReading — current_tank_environment 에 테스트용 측정값 삽입.
func seedReading(t *testing.T, st storage.Store, tankID, metric string, value float64, observedAt time.Time) {
	t.Helper()
	v := value
	r := &storage.CurrentTankEnvironmentReading{
		TankID:      tankID,
		Metric:      metric,
		Value:       &v,
		Unit:        "test_unit",
		Quality:     "good",
		SensorID:    "sensor-test",
		DeviceID:    "device-test",
		LastEventID: "evt-test",
		ObservedAt:  observedAt.UTC().Format(time.RFC3339Nano),
	}
	if err := st.UpsertTankEnvironmentReading(context.Background(), r, "{}"); err != nil {
		t.Fatalf("seedReading: %v", err)
	}
}

func condJSON(metric, op string, threshold float64) string {
	c := Condition{Metric: metric, Operator: op, Threshold: threshold, WindowH: 24}
	b, _ := json.Marshal(c)
	return string(b)
}

func enabledRule(ruleID, cJSON string) *storage.LearnedRule {
	return &storage.LearnedRule{
		RuleID:        ruleID,
		ConditionJSON: cJSON,
		Severity:      "high",
		Source:        "operator_dispute",
		Confidence:    0.9,
		Enabled:       true,
		CreatedAt:     time.Now(),
	}
}

// TestGate_EmptyRules_Allow — 규칙 없으면 항상 허용.
func TestGate_EmptyRules_Allow(t *testing.T) {
	g := makeGate(true)
	blocked, reason := g.CheckWithValue("tank-01", "water_temperature", 30.0)
	if blocked {
		t.Fatalf("expected allow with no rules, got block: %s", reason)
	}
}

// TestGate_MatchingRule_Block — 규칙 조건 매칭 시 차단.
func TestGate_MatchingRule_Block(t *testing.T) {
	rule := enabledRule("rule-temp-01", condJSON("water_temperature", "gt", 28.0))
	g := makeGate(true, rule)

	blocked, reason := g.CheckWithValue("tank-01", "water_temperature", 29.5)
	if !blocked {
		t.Fatal("expected block when temperature exceeds learned threshold")
	}
	if reason == "" {
		t.Fatal("expected non-empty block reason")
	}
}

// TestGate_ValueBelowThreshold_Allow — 임계값 미만이면 허용.
func TestGate_ValueBelowThreshold_Allow(t *testing.T) {
	rule := enabledRule("rule-do-01", condJSON("dissolved_oxygen", "lt", 6.0))
	g := makeGate(true, rule)

	// DO=7.0 > threshold=6.0 → condition is "lt 6.0" → 7.0 is NOT < 6.0 → allow
	blocked, _ := g.CheckWithValue("tank-01", "dissolved_oxygen", 7.0)
	if blocked {
		t.Fatal("expected allow when value does not breach threshold")
	}
}

// TestGate_Disabled_AlwaysAllow — 비활성 게이트는 항상 허용.
func TestGate_Disabled_AlwaysAllow(t *testing.T) {
	rule := enabledRule("rule-temp-02", condJSON("water_temperature", "gt", 10.0))
	g := makeGate(false, rule)

	blocked, _ := g.CheckWithValue("tank-01", "water_temperature", 99.0)
	if blocked {
		t.Fatal("disabled gate must always allow")
	}
}

// --- Check() 라이브 센서값 테스트 (real sqlite store 사용) ---

// TestCheck_LiveHighTemperature_Block — DB에 고온 측정값 + 매칭 규칙 → 차단.
func TestCheck_LiveHighTemperature_Block(t *testing.T) {
	st := openGateTestStore(t)
	// 30초 전 측정값 삽입 (staleness 기본 300초 이내)
	seedReading(t, st, "tank-01", "water_temperature", 29.5, time.Now().Add(-30*time.Second))

	rule := enabledRule("rule-live-01", condJSON("water_temperature", "gt", 28.0))
	g := &Gate{
		store: st,
		cfg:   config.LearnedSafetyConfig{Enabled: true, StalenessMaxSec: stalenessDefault},
		log:   slog.Default(),
		rules: []*storage.LearnedRule{rule},
	}

	blocked, reason := g.Check("tank-01")
	if !blocked {
		t.Fatal("expected block when live temperature exceeds learned threshold")
	}
	if reason == "" {
		t.Fatal("expected non-empty block reason")
	}
}

// TestCheck_StaleReading_Allow — staleness_max_sec 초과 측정값 → skip → 허용.
func TestCheck_StaleReading_Allow(t *testing.T) {
	st := openGateTestStore(t)
	// staleness 60초, 측정값은 120초 전 → stale
	seedReading(t, st, "tank-02", "water_temperature", 35.0, time.Now().Add(-120*time.Second))

	rule := enabledRule("rule-stale-01", condJSON("water_temperature", "gt", 28.0))
	g := &Gate{
		store: st,
		cfg:   config.LearnedSafetyConfig{Enabled: true, StalenessMaxSec: 60},
		log:   slog.Default(),
		rules: []*storage.LearnedRule{rule},
	}

	blocked, _ := g.Check("tank-02")
	if blocked {
		t.Fatal("stale reading must be skipped (fail-open)")
	}
}

// TestCheck_LiveValueBelowThreshold_Allow — 조건 불만족 측정값 → 허용.
func TestCheck_LiveValueBelowThreshold_Allow(t *testing.T) {
	st := openGateTestStore(t)
	// 수온 22도 (규칙: gt 28.0) → 조건 불만족
	seedReading(t, st, "tank-03", "water_temperature", 22.0, time.Now().Add(-10*time.Second))

	rule := enabledRule("rule-nomatch-01", condJSON("water_temperature", "gt", 28.0))
	g := &Gate{
		store: st,
		cfg:   config.LearnedSafetyConfig{Enabled: true, StalenessMaxSec: stalenessDefault},
		log:   slog.Default(),
		rules: []*storage.LearnedRule{rule},
	}

	blocked, _ := g.Check("tank-03")
	if blocked {
		t.Fatal("value below threshold must not block")
	}
}
