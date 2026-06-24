package baseline_test

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// seedAlert — 테스트용 open_alerts 에 알림 1건 삽입.
func seedAlert(t *testing.T, st storage.Store, subjectKind, subjectID, severity string) {
	t.Helper()
	ctx := context.Background()
	row := &storage.OpenAlert{
		AlertID:        "alert_" + subjectID + "_" + severity,
		AlertDedupeKey: "test." + subjectID + "." + severity,
		AlertType:      "test.alert",
		Severity:       severity,
		SubjectKind:    subjectKind,
		SubjectID:      subjectID,
		Status:         events.AlertStatusOpen,
		RaisedAt:       time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		PayloadJSON:    "{}",
	}
	if _, err := st.UpsertAlert(ctx, row); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
}

func TestSafetyGatePassesWithoutAlerts(t *testing.T) {
	_, st := newTestEnv(t)
	result, err := baseline.EvaluateSafetyGate(context.Background(), st, "tank_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("expected Blocked=false, got true (detail: %s)", result.Detail)
	}
}

func TestSafetyGateBlocksOnCriticalTankAlert(t *testing.T) {
	_, st := newTestEnv(t)
	seedAlert(t, st, "tank", "tank_a", events.SeverityCritical)

	result, err := baseline.EvaluateSafetyGate(context.Background(), st, "tank_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Errorf("expected Blocked=true, got false")
	}
	if result.Reason != "open_critical_alert" {
		t.Errorf("expected reason=open_critical_alert, got %q", result.Reason)
	}
	if result.Detail == "" {
		t.Errorf("expected non-empty Detail")
	}
}

func TestSafetyGateIgnoresWarningSeverity(t *testing.T) {
	_, st := newTestEnv(t)
	// warning 알림은 차단 X — critical 만 차단
	seedAlert(t, st, "tank", "tank_a", events.SeverityWarning)

	result, err := baseline.EvaluateSafetyGate(context.Background(), st, "tank_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("expected Blocked=false for warning severity, got true")
	}
}

func TestSafetyGateIgnoresOtherTanks(t *testing.T) {
	_, st := newTestEnv(t)
	// tank_a 에 critical alert — tank_b 는 통과해야 함
	seedAlert(t, st, "tank", "tank_a", events.SeverityCritical)

	result, err := baseline.EvaluateSafetyGate(context.Background(), st, "tank_b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Errorf("expected Blocked=false for tank_b (alert is on tank_a), got true")
	}
}
