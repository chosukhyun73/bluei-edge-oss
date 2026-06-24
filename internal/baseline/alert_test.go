package baseline_test

import (
	"context"
	"testing"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/events"
)

// payload helper — verdict 만 다른 표준 score 결과 생성.
func makeScore(tankID, verdict string, score float64) events.TankBaselineScoredPayload {
	return events.TankBaselineScoredPayload{
		TankID:       tankID,
		ModelDir:     "/tmp/fake",
		JobID:        "job_x",
		AnomalyScore: score,
		P95Threshold: 0.05,
		P99Threshold: 0.10,
		Verdict:      verdict,
		FeatureDiff: map[string]float64{
			"dissolved_oxygen": 1.5,
			"activity_score":   1.2,
			"ph":               0.3,
			"surface_cluster":  0.1,
		},
		EvaluatedAt: "2026-05-09T00:00:00Z",
	}
}

// TestRaiseAnomalyAlert — verdict=anomaly → 새 알림 생성, severity=critical.
func TestRaiseAnomalyAlert(t *testing.T) {
	app, st := newTestEnv(t)
	p := makeScore("tank_a", "anomaly", 0.15)
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st, p); err != nil {
		t.Fatalf("raise: %v", err)
	}
	open, err := st.GetOpenAlert(context.Background(), "tank.baseline.tank_a")
	if err != nil || open == nil {
		t.Fatalf("expected open alert, got %v err=%v", open, err)
	}
	if open.Severity != events.SeverityCritical {
		t.Errorf("severity: %s", open.Severity)
	}
	if open.SubjectKind != "tank" || open.SubjectID != "tank_a" {
		t.Errorf("subject: %s/%s", open.SubjectKind, open.SubjectID)
	}
}

// TestRaiseWarningAlert — verdict=warning → severity=warning.
func TestRaiseWarningAlert(t *testing.T) {
	app, st := newTestEnv(t)
	p := makeScore("tank_b", "warning", 0.07)
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st, p); err != nil {
		t.Fatalf("raise: %v", err)
	}
	open, _ := st.GetOpenAlert(context.Background(), "tank.baseline.tank_b")
	if open == nil {
		t.Fatal("expected open alert")
	}
	if open.Severity != events.SeverityWarning {
		t.Errorf("severity: %s", open.Severity)
	}
}

// TestNormalDoesNotCreateAlert — verdict=normal + 기존 알림 없으면 noop.
func TestNormalDoesNotCreateAlert(t *testing.T) {
	app, st := newTestEnv(t)
	p := makeScore("tank_c", "normal", 0.001)
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st, p); err != nil {
		t.Fatalf("noop: %v", err)
	}
	open, _ := st.GetOpenAlert(context.Background(), "tank.baseline.tank_c")
	if open != nil {
		t.Errorf("unexpected alert: %+v", open)
	}
}

// TestVerdictBackToNormalClosesAlert — anomaly → normal 전환 시 close.
func TestVerdictBackToNormalClosesAlert(t *testing.T) {
	app, st := newTestEnv(t)
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st,
		makeScore("tank_d", "anomaly", 0.15)); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if open, _ := st.GetOpenAlert(context.Background(), "tank.baseline.tank_d"); open == nil {
		t.Fatal("setup: expected open alert before close")
	}
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st,
		makeScore("tank_d", "normal", 0.001)); err != nil {
		t.Fatalf("close: %v", err)
	}
	open, _ := st.GetOpenAlert(context.Background(), "tank.baseline.tank_d")
	if open != nil {
		t.Errorf("expected alert closed, but still open: %+v", open)
	}
}

// TestRepeatedAnomalyUpdatesNotDuplicates — 같은 verdict 가 반복되면 dedupe.
func TestRepeatedAnomalyUpdatesNotDuplicates(t *testing.T) {
	app, st := newTestEnv(t)
	for i := 0; i < 3; i++ {
		if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st,
			makeScore("tank_e", "anomaly", 0.2)); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	// 항상 한 건만 열려있음
	all, err := st.ListOpenAlerts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, a := range all {
		if a.SubjectID == "tank_e" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 open alert for tank_e, got %d", count)
	}
}

// TestSeverityEscalation — warning 으로 raise → anomaly 로 같은 알림 격상.
func TestSeverityEscalation(t *testing.T) {
	app, st := newTestEnv(t)
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st,
		makeScore("tank_f", "warning", 0.07)); err != nil {
		t.Fatal(err)
	}
	open, _ := st.GetOpenAlert(context.Background(), "tank.baseline.tank_f")
	firstID := open.AlertID
	if open.Severity != events.SeverityWarning {
		t.Errorf("warning severity: %s", open.Severity)
	}
	if err := baseline.MaybeRaiseOrCloseAlert(context.Background(), app, st,
		makeScore("tank_f", "anomaly", 0.15)); err != nil {
		t.Fatal(err)
	}
	open2, _ := st.GetOpenAlert(context.Background(), "tank.baseline.tank_f")
	if open2.Severity != events.SeverityCritical {
		t.Errorf("escalated severity: %s", open2.Severity)
	}
	if open2.AlertID != firstID {
		t.Errorf("alert_id changed (should be same): %s -> %s", firstID, open2.AlertID)
	}
}
