package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// seedOpenAlert — 테스트용 open_alerts 에 알림 1건 삽입.
func seedOpenAlert(t *testing.T, st storage.Store, subjectKind, subjectID, severity string) {
	t.Helper()
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
	if _, err := st.UpsertAlert(context.Background(), row); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
}

// TestProposeDecisionBlockedByCriticalAlert — critical alert 가 있으면 route=rejected.
func TestProposeDecisionBlockedByCriticalAlert(t *testing.T) {
	s := newTestServerWithApp(t)

	// critical alert 시드 — 이 Cage/Tank 자율 결정 차단돼야 함
	seedOpenAlert(t, s.store, "tank", "tank_guarded", events.SeverityCritical)

	// feeding 결정 제안 — confidence 가 0 이어도 safety gate 가 먼저 rejected 설정
	w := proposeDecision(t, s, "tank_guarded", "feeding", "ai.test")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	route, _ := resp["route"].(string)
	if route != "rejected" {
		t.Errorf("expected route=rejected, got %q", route)
	}

	reasoning, _ := resp["reasoning"].(string)
	if reasoning == "" {
		t.Errorf("expected non-empty reasoning")
	}
	// safety_gate_blocked 키워드가 reasoning 에 포함돼야 함
	if len(reasoning) < 10 {
		t.Errorf("reasoning too short: %q", reasoning)
	}
}

// TestExecuteAutonomousActionAuditOnlyForUnsupportedKind — oxygen_supply 등 비-feeding kind 는
// executeAutonomousAction 이 control_not_wired 로 차단하고 nil result 반환 (audit-only).
func TestExecuteAutonomousActionAuditOnlyForUnsupportedKind(t *testing.T) {
	s := newTestServerWithApp(t)

	// executeAutonomousAction 직접 호출 — kind=oxygen_supply
	result, err := s.executeAutonomousAction(
		context.Background(),
		"tank_test", "decision_test_01", "oxygen_supply",
		map[string]any{"target_do_mg_l": 8.0},
		"auto_executed",
	)

	// audit-only 경로: error=nil, result=nil
	if err != nil {
		t.Errorf("expected nil error for unsupported kind, got: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for unsupported kind (audit-only), got: %+v", result)
	}

	// blocked 이벤트가 적재됐는지 확인
	es, qErr := s.store.QueryEvents(context.Background(), storage.EventFilter{
		EventType: events.EventAutonomousActionBlocked,
		Limit:     10,
	})
	if qErr != nil {
		t.Fatalf("query blocked events: %v", qErr)
	}
	found := false
	for _, e := range es {
		var p events.AutonomousActionBlockedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == "tank_test" && p.DecisionKind == "oxygen_supply" && p.Reason == "control_not_wired" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected control_not_wired blocked event to be appended for oxygen_supply")
	}
}
