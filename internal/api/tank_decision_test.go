package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/storage"
)

// proposeDecision — POST /v1/tanks/{id}/decisions/proposed 호출 helper.
func proposeDecision(t *testing.T, s *Server, tankID, kind, source string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"decision_kind":    kind,
		"proposing_source": source,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/v1/tanks/"+tankID+"/decisions/proposed",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankDecisionRoute(w, req)
	return w
}

// listPending — GET /v1/tanks/{id}/decisions/pending 호출 helper.
func listPending(t *testing.T, s *Server, tankID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/tanks/"+tankID+"/decisions/pending", nil)
	w := httptest.NewRecorder()
	s.handleTankDecisionRoute(w, req)
	return w
}

// resolveDecision — POST /v1/tanks/{id}/decisions/{id}/resolve 호출 helper.
func resolveDecision(t *testing.T, s *Server, tankID, decisionID, resolution string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"resolution":  resolution,
		"operator_id": "op_test",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/v1/tanks/"+tankID+"/decisions/"+decisionID+"/resolve",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankDecisionRoute(w, req)
	return w
}

// appendRoutedEvent — 테스트용: tank.decision.routed 이벤트 직접 적재.
func appendRoutedEvent(t *testing.T, s *Server, tankID, decisionID, route string) {
	t.Helper()
	const now = "2026-01-01T00:00:00.000000000Z"
	payload := map[string]any{
		"decision_id": decisionID, "tank_id": tankID,
		"decision_kind": "feeding", "proposing_source": "test",
		"confidence": 0.5, "autonomous_mode": "off",
		"route": route, "reasoning": "test", "proposed_at": now,
	}
	payloadJSON, _ := json.Marshal(payload)
	_, err := s.store.AppendEvent(context.Background(), &storage.Event{
		EventID:     "evt_test_" + decisionID,
		EventType:   "tank.decision.routed",
		PayloadJSON: string(payloadJSON),
	})
	if err != nil {
		t.Fatalf("append routed event: %v", err)
	}
}

// TestProposeDecisionRouteRejected — 빈 Cage/Tank (모델 없음, 데이터 없음) → confidence=0 → rejected.
func TestProposeDecisionRouteRejected(t *testing.T) {
	s := newTestServerWithApp(t)
	w := proposeDecision(t, s, "tank_cold", "feeding", "ai.feeding.recommender")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["route"] != "rejected" {
		t.Errorf("expected route=rejected for cold tank, got %v", resp["route"])
	}
	if resp["decision_id"] == "" {
		t.Error("expected decision_id in response")
	}
}

// TestProposeDecisionRouteAdvisory — observation 모드이지만 빈 store → confidence=0 → rejected.
// (c<0.3 은 모드 무관 rejected. advisory_only 는 c≥0.3 + observation 에서만 발생.)
func TestProposeDecisionRouteAdvisory(t *testing.T) {
	s := newTestServerWithApp(t)
	// observation 모드 설정
	w1 := postAutoMode(t, s, "tank_obs", "observation", "AI 학습 시작", "op")
	if w1.Code != http.StatusOK {
		t.Fatalf("set mode: %d %s", w1.Code, w1.Body.String())
	}

	w := proposeDecision(t, s, "tank_obs", "oxygen_supply", "ai.oxygen.recommender")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	// 빈 store → confidence=0 → c<0.3 → rejected (observation 모드여도 cold 구간 우선)
	if resp["route"] != "rejected" {
		t.Errorf("expected route=rejected (confidence=0 < 0.3 regardless of mode), got %v", resp["route"])
	}
	if resp["mode"] != "observation" {
		t.Errorf("expected mode=observation in response, got %v", resp["mode"])
	}
}

// TestListPendingDecisions — 빈 store 에서 pending list 호출 → 0건 + 정상 응답.
func TestListPendingDecisions(t *testing.T) {
	s := newTestServerWithApp(t)
	w := listPending(t, s, "tank_list_test")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["tank_id"] != "tank_list_test" {
		t.Errorf("expected tank_id in response, got %v", resp)
	}
	count := int(resp["count"].(float64))
	if count != 0 {
		t.Errorf("expected 0 pending for fresh tank, got %d", count)
	}
}

// TestListPendingDecisionsTwoEvents — 2건 pending 이벤트 직접 적재 → list 에 2건 반환.
func TestListPendingDecisionsTwoEvents(t *testing.T) {
	s := newTestServerWithApp(t)
	tankID := "tank_pending_direct"

	appendRoutedEvent(t, s, tankID, "decision_01", "pending_approval")
	appendRoutedEvent(t, s, tankID, "decision_02", "pending_approval")

	w := listPending(t, s, tankID)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	count := int(resp["count"].(float64))
	if count != 2 {
		t.Errorf("expected 2 pending, got %d", count)
	}
}

// TestResolveDecision — 1건 적재 후 resolve approved → list 0건, resolved event 1건.
func TestResolveDecision(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	tankID := "tank_resolve_test"
	decisionID := "decision_resolve_01"

	appendRoutedEvent(t, s, tankID, decisionID, "pending_approval")

	// list → 1건
	w1 := listPending(t, s, tankID)
	var r1 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &r1)
	if int(r1["count"].(float64)) != 1 {
		t.Fatalf("expected 1 pending before resolve, got %v", r1["count"])
	}

	// resolve
	wr := resolveDecision(t, s, tankID, decisionID, "approved")
	if wr.Code != http.StatusOK {
		t.Fatalf("resolve: expected 200, got %d: %s", wr.Code, wr.Body.String())
	}
	var rr map[string]any
	json.Unmarshal(wr.Body.Bytes(), &rr)
	if rr["ok"] != true {
		t.Errorf("expected ok=true: %v", rr)
	}

	// list → 0건
	w2 := listPending(t, s, tankID)
	var r2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if int(r2["count"].(float64)) != 0 {
		t.Errorf("expected 0 pending after resolve, got %v", r2["count"])
	}

	// resolved event 적재 확인
	evts, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.decision.resolved",
		Limit:     10,
	})
	if len(evts) != 1 {
		t.Errorf("expected 1 resolved event, got %d", len(evts))
	}
}

// TestResolveInvalidResolution — resolution="maybe" → 422 INVALID_RESOLUTION.
func TestResolveInvalidResolution(t *testing.T) {
	s := newTestServerWithApp(t)
	w := resolveDecision(t, s, "tank_x", "decision_x", "maybe")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "INVALID_RESOLUTION" {
		t.Errorf("expected INVALID_RESOLUTION, got %v", resp)
	}
}

// TestProposeMissingDecisionKind — decision_kind 없음 → 422 MISSING_DECISION_KIND.
func TestProposeMissingDecisionKind(t *testing.T) {
	s := newTestServerWithApp(t)
	body, _ := json.Marshal(map[string]string{"proposing_source": "ai.test"})
	req := httptest.NewRequest(http.MethodPost, "/v1/tanks/tank_x/decisions/proposed", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankDecisionRoute(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "MISSING_DECISION_KIND" {
		t.Errorf("expected MISSING_DECISION_KIND, got %v", resp)
	}
}

// TestDecisionRoutePathHelpers — 경로 helper 검증.
func TestDecisionRoutePathHelpers(t *testing.T) {
	tankCases := []struct{ path, want string }{
		{"/v1/tanks/tank_01/decisions/proposed", "tank_01"},
		{"/v1/tanks/tank_01/decisions/pending", "tank_01"},
		{"/v1/tanks/tank_01/decisions/decision_abc/resolve", "tank_01"},
		{"/v1/tanks/tank_01/state-vector", ""},
	}
	for _, c := range tankCases {
		if got := tankIDFromDecisionPath(c.path); got != c.want {
			t.Errorf("tankIDFromDecisionPath(%q): got %q, want %q", c.path, got, c.want)
		}
	}

	decisionCases := []struct{ path, want string }{
		{"/v1/tanks/tank_01/decisions/decision_abc/resolve", "decision_abc"},
		{"/v1/tanks/tank_01/decisions/proposed", ""},
	}
	for _, c := range decisionCases {
		if got := decisionIDFromResolvePath(c.path); got != c.want {
			t.Errorf("decisionIDFromResolvePath(%q): got %q, want %q", c.path, got, c.want)
		}
	}
}
