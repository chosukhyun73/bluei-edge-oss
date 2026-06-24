package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// newTestServerWithApp — AppendEvent 가 필요한 핸들러 테스트용.
// 실제 runtime.App 포함 (store 공유).
func newTestServerWithApp(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	store := newTestStore(t)
	app := runtime.NewApp(cfg, store)
	return &Server{cfg: cfg, app: app, store: store}
}

// postAutoMode — POST /v1/tanks/{id}/autonomous-mode 호출 helper.
func postAutoMode(t *testing.T, s *Server, tankID, mode, reason, operatorID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"mode":        mode,
		"reason":      reason,
		"operator_id": operatorID,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/v1/tanks/"+tankID+"/autonomous-mode",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankAutonomousMode(w, req)
	return w
}

// getAutoMode — GET /v1/tanks/{id}/autonomous-mode 호출 helper.
func getAutoMode(t *testing.T, s *Server, tankID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/tanks/"+tankID+"/autonomous-mode", nil)
	w := httptest.NewRecorder()
	s.handleTankAutonomousMode(w, req)
	return w
}

// TestTankAutonomousModeIDFromPath — 라우트 helper 검증.
func TestTankAutonomousModeIDFromPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/v1/tanks/tank_01/autonomous-mode", "tank_01"},
		{"/v1/tanks/tank_x_y/autonomous-mode", "tank_x_y"},
		{"/v1/tanks/tank_01/state-vector", ""},
		{"/v1/tanks//autonomous-mode", ""},
		{"/v1/tanks/tank_01/profile", ""},
	}
	for _, c := range cases {
		if got := tankAutonomousModeIDFromPath(c.path); got != c.want {
			t.Errorf("path %q: got %q, want %q", c.path, got, c.want)
		}
	}
}

// TestGetTankAutonomousModeDefault — 행 없음 → mode:"off" 기본값 반환.
func TestGetTankAutonomousModeDefault(t *testing.T) {
	s := newTestServer(t)
	w := getAutoMode(t, s, "tank_new")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["mode"] != "off" {
		t.Errorf("expected default mode=off, got %v", resp["mode"])
	}
}

// TestPostTankAutonomousModeInvalidMode — 잘못된 mode → 422 INVALID_MODE.
func TestPostTankAutonomousModeInvalidMode(t *testing.T) {
	s := newTestServer(t)
	w := postAutoMode(t, s, "tank_01", "turbo", "some reason", "op")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "INVALID_MODE" {
		t.Errorf("expected INVALID_MODE error code, got %v", resp)
	}
}

// TestPostTankAutonomousModeMissingReason — observation 이지만 reason 없음 → 422 MISSING_REASON.
func TestPostTankAutonomousModeMissingReason(t *testing.T) {
	s := newTestServer(t)
	w := postAutoMode(t, s, "tank_01", "partial", "", "op")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "MISSING_REASON" {
		t.Errorf("expected MISSING_REASON error code, got %v", resp)
	}
}

// TestPostTankAutonomousModeOffNoReason — off 전환은 reason 없어도 허용.
func TestPostTankAutonomousModeOffNoReason(t *testing.T) {
	s := newTestServerWithApp(t)
	w := postAutoMode(t, s, "tank_01", "off", "", "op")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true: %v", resp)
	}
}

// TestPostTankAutonomousModeFlow — off → observation 전환 + audit 이벤트 + 두 번째 동일 모드는 no_change.
func TestPostTankAutonomousModeFlow(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 1. off → observation
	w1 := postAutoMode(t, s, "tank_01", "observation", "AI 학습 시작", "op1")
	if w1.Code != http.StatusOK {
		t.Fatalf("first POST: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}
	var r1 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &r1)
	if r1["ok"] != true || r1["no_change"] == true {
		t.Errorf("expected ok=true no_change=false, got %v", r1)
	}

	// 2. projection 반영 확인
	row, err := s.store.GetTankAutonomousMode(ctx, "tank_01")
	if err != nil || row == nil || row.Mode != "observation" {
		t.Fatalf("projection not written: row=%+v err=%v", row, err)
	}

	// 3. audit 이벤트 확인
	evts, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.autonomous_mode.changed",
		Limit:     10,
	})
	if len(evts) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(evts))
	}

	// 4. 동일 모드 재요청 → no_change=true, 이벤트 추가 없음
	w2 := postAutoMode(t, s, "tank_01", "observation", "재확인", "op1")
	if w2.Code != http.StatusOK {
		t.Fatalf("second POST: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var r2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if r2["no_change"] != true {
		t.Errorf("expected no_change=true on identical mode, got %v", r2)
	}

	// 이벤트 수 변화 없어야 함
	evts2, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.autonomous_mode.changed",
		Limit:     10,
	})
	if len(evts2) != 1 {
		t.Fatalf("expected still 1 audit event after no-change, got %d", len(evts2))
	}
}

// TestStateVectorIncludesAutonomous — state vector 에 autonomous 섹션 포함 + 기본값 off.
func TestStateVectorIncludesAutonomous(t *testing.T) {
	s := newTestServer(t)
	if err := s.store.UpsertTankProfile(context.Background(), &storage.TankProfile{
		TankID: "tank_auto", DisplayName: "Auto Test", Species: "연어",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	req := httptest.NewRequest("GET", "/v1/tanks/tank_auto/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_auto")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if v.Autonomous.Mode != "off" {
		t.Errorf("expected default autonomous mode=off, got %q", v.Autonomous.Mode)
	}
	if len(v.Autonomous.Notes) == 0 {
		t.Error("expected notes for default autonomous state")
	}
	// JSON 직렬화 확인
	body, _ := json.Marshal(v)
	bodyStr := string(body)
	for _, key := range []string{`"autonomous":`, `"mode":`} {
		if !containsStr(bodyStr, key) {
			t.Errorf("missing key %q in JSON", key)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
