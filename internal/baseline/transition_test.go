package baseline_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// insertTransitionEvent — tank.transition.detected 이벤트를 DB 에 직접 삽입.
func insertTransitionEvent(t *testing.T, db *sql.DB, tankID, reason string, weightAtDetection float64, at time.Time) {
	t.Helper()
	evidenceJSON := "{}"
	if reason == "weight_threshold_passed" && weightAtDetection > 0 {
		evidenceJSON = fmt.Sprintf(`{"weight_at_detection":%f,"current_weight":%f,"threshold_passed":%f}`,
			weightAtDetection, weightAtDetection, weightAtDetection)
	}
	payload := fmt.Sprintf(
		`{"tank_id":%q,"reason":%q,"detected_at":%q,"evidence":%s}`,
		tankID, reason, at.UTC().Format(time.RFC3339Nano), evidenceJSON,
	)
	atStr := at.UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`
		INSERT INTO events (event_id, event_type, schema_version, site_id, edge_id,
		                    source_module, source_device_id, payload_json, event_json, recorded_at)
		VALUES (lower(hex(randomblob(8))), 'tank.transition.detected', '1.0', 'site_t', 'edge_t',
		        'test', ?, ?, '{}', ?)`,
		tankID, payload, atStr,
	)
	if err != nil {
		t.Fatalf("insertTransitionEvent: %v", err)
	}
}

// upsertTestTankProfile — 테스트용 TankProfile 을 저장소에 등록.
func upsertTestTankProfile(t *testing.T, st storage.Store, tankID string, avgWeightG float64) {
	t.Helper()
	err := st.UpsertTankProfile(context.Background(), &config.TankProfile{
		TankID:      tankID,
		DisplayName: tankID,
		Species:     "참돔",
		AvgWeightG:  avgWeightG,
	})
	if err != nil {
		t.Fatalf("upsertTestTankProfile: %v", err)
	}
}

// currentDB — newTestEnv 가 chdir 한 tmp 내 test.db 경로 반환.
func currentDB(t *testing.T) *sql.DB {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return rawDB(t, filepath.Join(cwd, "test.db"))
}

// containsStr — 단순 부분문자열 포함 여부 확인.
func containsStr(s, sub string) bool {
	return strings.Contains(s, sub)
}

// TestDetectTransitionInsufficientData — 30건 미만이면 Detected=false, notes 포함.
func TestDetectTransitionInsufficientData(t *testing.T) {
	_, st := newTestEnv(t)
	db := currentDB(t)

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		at := now.Add(-time.Duration(i+1) * 12 * time.Hour)
		insertBaselineEvent(t, db, "tank_insuf", "normal", 0.001, at)
	}

	res, err := baseline.DetectGrowthTransition(context.Background(), st, "tank_insuf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Detected {
		t.Error("expected Detected=false with insufficient data")
	}
	hasNote := false
	for _, n := range res.Notes {
		if containsStr(n, "부족") || containsStr(n, "필요") {
			hasNote = true
			break
		}
	}
	if !hasNote {
		t.Errorf("expected insufficient-data note, got: %v", res.Notes)
	}
}

// TestDetectTransitionNoShift — 양쪽 절반 모두 90% 정상, 비슷한 score → Detected=false.
func TestDetectTransitionNoShift(t *testing.T) {
	_, st := newTestEnv(t)
	db := currentDB(t)
	now := time.Now().UTC()

	// older 절반 (8~14일 전): 18 normal + 2 anomaly → 90% normal, mean≈0.01
	for i := 0; i < 18; i++ {
		at := now.Add(-time.Duration(i+8)*24*time.Hour - time.Duration(i)*time.Hour)
		insertBaselineEvent(t, db, "tank_noshift", "normal", 0.001, at)
	}
	for i := 0; i < 2; i++ {
		at := now.Add(-time.Duration(i+10) * 24 * time.Hour)
		insertBaselineEvent(t, db, "tank_noshift", "anomaly", 0.6, at)
	}
	// recent 절반 (0~7일): 18 normal + 2 anomaly → 90% normal, mean≈0.0015
	for i := 0; i < 18; i++ {
		at := now.Add(-time.Duration(i+1) * 8 * time.Hour)
		insertBaselineEvent(t, db, "tank_noshift", "normal", 0.0015, at)
	}
	for i := 0; i < 2; i++ {
		at := now.Add(-time.Duration(i+2)*8*time.Hour - time.Hour)
		insertBaselineEvent(t, db, "tank_noshift", "anomaly", 0.62, at)
	}

	res, err := baseline.DetectGrowthTransition(context.Background(), st, "tank_noshift")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Detected {
		t.Errorf("expected Detected=false on stable data, reason=%q notes=%v", res.Reason, res.Notes)
	}
}

// TestDetectTransitionAnomalyDrift — 정상비율 95%→40%, score 5배 → Detected=true, anomaly_drift.
func TestDetectTransitionAnomalyDrift(t *testing.T) {
	_, st := newTestEnv(t)
	db := currentDB(t)
	now := time.Now().UTC()

	// older (8~14d): 9일~13일 전, 19 normal (score=0.001) + 1 anomaly → ~95% normal
	// 14일 경계 넘지 않도록 8~13일 사이에만 분산
	for i := 0; i < 19; i++ {
		// 8일 ~ 13일 사이 균등 배치 (300분 간격)
		hoursBack := 8*24 + i*8 // 192h ~ 336h (8d ~ 14d exclusive)
		at := now.Add(-time.Duration(hoursBack) * time.Hour)
		insertBaselineEvent(t, db, "tank_drift", "normal", 0.001, at)
	}
	// anomaly 1건 (older 구간)
	insertBaselineEvent(t, db, "tank_drift", "anomaly", 0.002, now.Add(-10*24*time.Hour))

	// recent (0~7d): 8 normal (score=0.005) + 12 anomaly → 40% normal
	for i := 0; i < 8; i++ {
		at := now.Add(-time.Duration(i+1) * 12 * time.Hour)
		insertBaselineEvent(t, db, "tank_drift", "normal", 0.005, at)
	}
	for i := 0; i < 12; i++ {
		at := now.Add(-time.Duration(i+1) * 5 * time.Hour)
		insertBaselineEvent(t, db, "tank_drift", "anomaly", 0.005, at)
	}

	res, err := baseline.DetectGrowthTransition(context.Background(), st, "tank_drift")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Detected {
		t.Errorf("expected Detected=true, notes=%v", res.Notes)
	}
	if res.Reason != "anomaly_drift_detected" {
		t.Errorf("expected reason=anomaly_drift_detected, got %q", res.Reason)
	}
	if res.Evidence == nil {
		t.Fatal("expected non-nil evidence")
	}
	for _, k := range []string{"recent_normal_rate", "older_normal_rate", "recent_mean_score", "older_mean_score"} {
		if _, ok := res.Evidence[k]; !ok {
			t.Errorf("evidence missing key %q", k)
		}
	}
}

// TestDetectTransitionWeightThresholdFirstCrossing — 체중 120g, 이전 전환 없음 → threshold_passed=100.
func TestDetectTransitionWeightThresholdFirstCrossing(t *testing.T) {
	_, st := newTestEnv(t)
	upsertTestTankProfile(t, st, "tank_w1", 120.0)

	res, err := baseline.DetectGrowthTransition(context.Background(), st, "tank_w1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Detected {
		t.Errorf("expected Detected=true, notes=%v", res.Notes)
	}
	if res.Reason != "weight_threshold_passed" {
		t.Errorf("expected reason=weight_threshold_passed, got %q", res.Reason)
	}
	tp, _ := res.Evidence["threshold_passed"].(float64)
	if tp != 100 {
		t.Errorf("expected threshold_passed=100, got %v", res.Evidence["threshold_passed"])
	}
}

// TestDetectTransitionWeightThresholdNoRegression — 체중 120g, 이미 100g 통과 기록 → Detected=false (weight).
func TestDetectTransitionWeightThresholdNoRegression(t *testing.T) {
	_, st := newTestEnv(t)
	upsertTestTankProfile(t, st, "tank_w2", 120.0)
	db := currentDB(t)

	// 이미 100g 임계 통과 이벤트 존재 (48시간 전)
	insertTransitionEvent(t, db, "tank_w2", "weight_threshold_passed", 100.0, time.Now().UTC().Add(-48*time.Hour))

	res, err := baseline.DetectGrowthTransition(context.Background(), st, "tank_w2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// weight signal 은 더 이상 발화하면 안 됨
	if res.Detected && res.Reason == "weight_threshold_passed" {
		t.Errorf("expected no weight transition (already crossed), got Detected=true reason=%q", res.Reason)
	}
}

// TestDetectTransitionWeightTakesPrecedence — 두 신호 모두 발화 → weight 가 우선.
func TestDetectTransitionWeightTakesPrecedence(t *testing.T) {
	_, st := newTestEnv(t)
	// 체중 120g → weight signal 발화
	upsertTestTankProfile(t, st, "tank_wp", 120.0)

	// anomaly drift signal 도 발화하는 데이터 삽입 (tank_drift 와 동일 패턴)
	db := currentDB(t)
	now := time.Now().UTC()
	for i := 0; i < 19; i++ {
		hoursBack := 8*24 + i*8
		at := now.Add(-time.Duration(hoursBack) * time.Hour)
		insertBaselineEvent(t, db, "tank_wp", "normal", 0.001, at)
	}
	insertBaselineEvent(t, db, "tank_wp", "anomaly", 0.002, now.Add(-10*24*time.Hour))
	for i := 0; i < 8; i++ {
		at := now.Add(-time.Duration(i+1) * 12 * time.Hour)
		insertBaselineEvent(t, db, "tank_wp", "normal", 0.005, at)
	}
	for i := 0; i < 12; i++ {
		at := now.Add(-time.Duration(i+1) * 5 * time.Hour)
		insertBaselineEvent(t, db, "tank_wp", "anomaly", 0.005, at)
	}

	res, err := baseline.DetectGrowthTransition(context.Background(), st, "tank_wp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Detected {
		t.Fatalf("expected Detected=true, notes=%v", res.Notes)
	}
	if res.Reason != "weight_threshold_passed" {
		t.Errorf("expected weight_threshold_passed to take precedence, got %q", res.Reason)
	}
}

// TestRaiseTransitionAlert — alert 생성 확인: severity=warning, type=tank.transition, dedupe 격리.
func TestRaiseTransitionAlert(t *testing.T) {
	_, st := newTestEnv(t)

	// raiseTransitionAlert 는 package-private 이므로 storage layer 검증으로 대신.
	// tank.transition.alert_tgt 의 dedupe key 로 alert 를 upsert 하고 조회.
	key := "tank.transition.alert_tgt"
	before, err := st.GetOpenAlert(context.Background(), key)
	if err != nil {
		t.Fatalf("GetOpenAlert: %v", err)
	}
	if before != nil {
		t.Fatal("expected no pre-existing alert")
	}

	now := time.Now().UTC()
	row := &storage.OpenAlert{
		AlertID:        "alert_test_1",
		AlertDedupeKey: key,
		AlertType:      "tank.transition",
		Severity:       events.SeverityWarning,
		SubjectKind:    "tank",
		SubjectID:      "alert_tgt",
		Status:         events.AlertStatusOpen,
		RaisedAt:       now,
		UpdatedAt:      now,
		PayloadJSON:    `{}`,
	}
	created, err := st.UpsertAlert(context.Background(), row)
	if err != nil {
		t.Fatalf("UpsertAlert: %v", err)
	}
	if !created {
		t.Error("expected created=true on first upsert")
	}

	a, err := st.GetOpenAlert(context.Background(), key)
	if err != nil {
		t.Fatalf("GetOpenAlert after upsert: %v", err)
	}
	if a == nil {
		t.Fatal("expected alert to exist after upsert")
	}
	if a.Severity != events.SeverityWarning {
		t.Errorf("expected severity=warning, got %q", a.Severity)
	}
	if a.AlertType != "tank.transition" {
		t.Errorf("expected alert_type=tank.transition, got %q", a.AlertType)
	}

	// dedupe: 다른 Cage/Tank의 key 와 격리
	key2 := "tank.transition.other_tank"
	a2, err := st.GetOpenAlert(context.Background(), key2)
	if err != nil {
		t.Fatalf("GetOpenAlert key2: %v", err)
	}
	if a2 != nil {
		t.Error("expected no alert for other_tank (dedupe isolation)")
	}
}
