package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/config"
)

// newTestServerWithPolicy — decision policy 테스트용 서버.
// system_default grace=10, enabled=false.
func newTestServerWithPolicy(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.Site.Timezone = "Asia/Seoul"
	cfg.DecisionPolicy.AutoExecuteEnabled = false
	cfg.DecisionPolicy.GraceMinutes = 10
	return &Server{cfg: cfg, store: newTestStore(t)}
}

// getDecisionPolicy — GET /v1/tanks/{id}/decision-policy 호출 helper.
func getDecisionPolicy(t *testing.T, s *Server, tankID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/tanks/"+tankID+"/decision-policy", nil)
	w := httptest.NewRecorder()
	s.handleTankDecisionPolicy(w, req)
	return w
}

// postDecisionPolicy — POST /v1/tanks/{id}/decision-policy 호출 helper.
func postDecisionPolicy(t *testing.T, s *Server, tankID string, enabled bool, graceMinutes int, updatedBy string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"auto_execute_enabled": enabled,
		"grace_minutes":        graceMinutes,
		"updated_by":           updatedBy,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/v1/tanks/"+tankID+"/decision-policy",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankDecisionPolicy(w, req)
	return w
}

// TestGetPolicyDefault — Cage/Tank 행 없음 → system fallback + source="system_default".
func TestGetPolicyDefault(t *testing.T) {
	s := newTestServerWithPolicy(t)
	w := getDecisionPolicy(t, s, "tank_new")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["source"] != "system_default" {
		t.Errorf("expected source=system_default, got %v", resp["source"])
	}
	if resp["auto_execute_enabled"] != false {
		t.Errorf("expected auto_execute_enabled=false (system default), got %v", resp["auto_execute_enabled"])
	}
	if resp["grace_minutes"] != float64(10) {
		t.Errorf("expected grace_minutes=10 (system default), got %v", resp["grace_minutes"])
	}
}

// TestPostPolicyHappyPath — 정상 POST → 200 + projection upsert + GET 후 source="tank_override".
func TestPostPolicyHappyPath(t *testing.T) {
	s := newTestServerWithPolicy(t)
	ctx := context.Background()

	w := postDecisionPolicy(t, s, "tank_01", true, 5, "operator_A")
	if w.Code != http.StatusOK {
		t.Fatalf("POST expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var postResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &postResp)
	if postResp["ok"] != true || postResp["changed"] != true {
		t.Errorf("expected ok=true changed=true: %v", postResp)
	}

	// GET 후 tank_override 확인
	w2 := getDecisionPolicy(t, s, "tank_01")
	var getResp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &getResp)
	if getResp["source"] != "tank_override" {
		t.Errorf("expected source=tank_override after POST, got %v", getResp["source"])
	}
	if getResp["auto_execute_enabled"] != true {
		t.Errorf("expected auto_execute_enabled=true, got %v", getResp["auto_execute_enabled"])
	}
	if getResp["grace_minutes"] != float64(5) {
		t.Errorf("expected grace_minutes=5, got %v", getResp["grace_minutes"])
	}

	// store 직접 확인
	row, err := s.store.GetTankDecisionPolicy(ctx, "tank_01")
	if err != nil || row == nil {
		t.Fatalf("store.GetTankDecisionPolicy: err=%v row=%v", err, row)
	}
	if !row.AutoExecuteEnabled || row.GraceMinutes != 5 || row.UpdatedBy != "operator_A" {
		t.Errorf("store row mismatch: %+v", row)
	}
}

// TestPostPolicyInvalidGrace — grace=0, grace=1500 → 422 INVALID_GRACE_MINUTES.
func TestPostPolicyInvalidGrace(t *testing.T) {
	s := newTestServerWithPolicy(t)
	for _, grace := range []int{0, 1500} {
		w := postDecisionPolicy(t, s, "tank_01", true, grace, "op")
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("grace=%d: expected 422, got %d: %s", grace, w.Code, w.Body.String())
		}
		var resp map[string]any
		json.Unmarshal(w.Body.Bytes(), &resp)
		if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "INVALID_GRACE_MINUTES" {
			t.Errorf("grace=%d: expected INVALID_GRACE_MINUTES, got %v", grace, errObj)
		}
	}
}

// TestPostPolicyTogglesEnabled — enabled=false → enabled=true 변경 후 GET 으로 검증.
func TestPostPolicyTogglesEnabled(t *testing.T) {
	s := newTestServerWithPolicy(t)

	// 1. enabled=false 설정
	postDecisionPolicy(t, s, "tank_toggle", false, 15, "op")
	w1 := getDecisionPolicy(t, s, "tank_toggle")
	var r1 map[string]any
	json.Unmarshal(w1.Body.Bytes(), &r1)
	if r1["auto_execute_enabled"] != false {
		t.Errorf("expected false after first POST, got %v", r1["auto_execute_enabled"])
	}

	// 2. enabled=true 로 변경
	w2 := postDecisionPolicy(t, s, "tank_toggle", true, 15, "op")
	var r2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if r2["changed"] != true {
		t.Errorf("expected changed=true when toggling, got %v", r2["changed"])
	}

	// 3. GET 으로 검증
	w3 := getDecisionPolicy(t, s, "tank_toggle")
	var r3 map[string]any
	json.Unmarshal(w3.Body.Bytes(), &r3)
	if r3["auto_execute_enabled"] != true {
		t.Errorf("expected true after toggle, got %v", r3["auto_execute_enabled"])
	}
}

// TestTankDecisionPolicyIDFromPath — 라우트 helper 검증.
func TestTankDecisionPolicyIDFromPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/v1/tanks/tank_01/decision-policy", "tank_01"},
		{"/v1/tanks/tank_x_y/decision-policy", "tank_x_y"},
		{"/v1/tanks/tank_01/autonomous-mode", ""},
		{"/v1/tanks//decision-policy", ""},
	}
	for _, c := range cases {
		if got := tankDecisionPolicyIDFromPath(c.path); got != c.want {
			t.Errorf("path %q: got %q, want %q", c.path, got, c.want)
		}
	}
}

// TestPostPolicySameValueNoChange — 동일 값 재전송 → changed=false.
func TestPostPolicySameValueNoChange(t *testing.T) {
	s := newTestServerWithPolicy(t)

	postDecisionPolicy(t, s, "tank_same", true, 10, "op")
	w2 := postDecisionPolicy(t, s, "tank_same", true, 10, "op")
	var r2 map[string]any
	json.Unmarshal(w2.Body.Bytes(), &r2)
	if r2["changed"] != false {
		t.Errorf("expected changed=false on identical POST, got %v", r2["changed"])
	}
}
