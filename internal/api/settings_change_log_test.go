package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

func changeEvents(t *testing.T, s *Server, eventType string) []map[string]any {
	t.Helper()
	evts, err := s.store.QueryEvents(context.Background(), storage.EventFilter{EventType: eventType, Limit: 100})
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	var out []map[string]any
	for _, e := range evts {
		var p map[string]any
		_ = json.Unmarshal([]byte(e.PayloadJSON), &p)
		out = append(out, p)
	}
	return out
}

func TestGroupChange_LoggedOnlyWhenChanged(t *testing.T) {
	s := newTestServerWithApp(t)
	body := func(mode string) map[string]any {
		return map[string]any{
			"group_id": "g1", "name": "G1",
			"metadata": map[string]any{"feeding_policy": map[string]any{"bsf_mode": mode, "daily_cycles": 8}},
		}
	}
	if w := groupsPost(t, s, body("standard")); w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	// 같은 값 재저장 — 기록 없음
	groupsPost(t, s, body("standard"))
	// 대시보드 경로(POST upsert)로 모드 변경
	groupsPost(t, s, body("aggressive"))
	// PUT 경로로 되돌림
	groupPut(t, s, "g1", body("standard"))

	evs := changeEvents(t, s, events.EventGroupProfileChanged)
	if len(evs) != 3 {
		t.Fatalf("want 3 events (created, POST change, PUT change), got %d: %v", len(evs), evs)
	}
	var sawPost bool
	for _, e := range evs {
		ch, _ := e["changes"].(map[string]any)
		c, ok := ch["metadata.feeding_policy.bsf_mode"].(map[string]any)
		if e["action"] == "updated" && ok && c["from"] == "standard" && c["to"] == "aggressive" {
			sawPost = true
			if len(ch) != 1 {
				t.Errorf("only bsf_mode should change, got %v", ch)
			}
		}
	}
	if !sawPost {
		t.Errorf("standard→aggressive change not recorded: %v", evs)
	}
}

func TestTankPolicyOverrideChange_Logged(t *testing.T) {
	s := newTestServerWithApp(t)
	post := func(meta map[string]any) {
		b, _ := json.Marshal(map[string]any{
			"tank_id": "tank_01", "display_name": "7-1", "species": "atlantic_salmon",
			"system_type": "flow_through", "metadata": meta,
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/tanks", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.handlePostTank(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("post tank: %d %s", w.Code, w.Body.String())
		}
	}
	post(map[string]any{"note": "x"})                                                                      // override 없음 — 기록 없음
	post(map[string]any{"note": "y", "feeding_policy_override": map[string]any{"bsf_mode": "aggressive"}}) // 추가
	post(map[string]any{"note": "z", "feeding_policy_override": map[string]any{"bsf_mode": "aggressive"}}) // override 불변 — 기록 없음
	post(map[string]any{"note": "z"})                                                                      // 해제

	evs := changeEvents(t, s, events.EventTankFeedingPolicyChanged)
	if len(evs) != 2 {
		t.Fatalf("want 2 events (set, clear), got %d: %v", len(evs), evs)
	}
}
