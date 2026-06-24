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

// postStocking — POST /v1/tanks/{id}/stocking 호출 helper.
func postStocking(t *testing.T, s *Server, tankID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/tanks/"+tankID+"/stocking", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankLifecycleRoute(w, req)
	return w
}

// postHarvest — POST /v1/tanks/{id}/harvest 호출 helper.
func postHarvest(t *testing.T, s *Server, tankID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/tanks/"+tankID+"/harvest", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankLifecycleRoute(w, req)
	return w
}

// getLifecycle — GET /v1/tanks/{id}/lifecycle 호출 helper.
func getLifecycle(t *testing.T, s *Server, tankID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/tanks/"+tankID+"/lifecycle", nil)
	w := httptest.NewRecorder()
	s.handleTankLifecycleRoute(w, req)
	return w
}

// defaultStockingBody — 검증 통과 최소 입식 바디.
func defaultStockingBody() map[string]any {
	return map[string]any{
		"species":              "연어",
		"growth_stage":         "growout",
		"initial_count":        1000,
		"initial_avg_weight_g": 50.0,
		"operator_id":          "op1",
	}
}

// TestPostStockingHappyPath — 신규 Cage/Tank 입식 → 200 + lifecycle 생성 + audit 이벤트 + 자율모드 off.
func TestPostStockingHappyPath(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 자율 모드를 observation 으로 먼저 설정 (auto-off 확인용)
	postAutoMode(t, s, "tank_01", "observation", "테스트 설정", "op1")

	w := postStocking(t, s, "tank_01", defaultStockingBody())
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true: %v", resp)
	}
	stockingID, _ := resp["stocking_id"].(string)
	if stockingID == "" {
		t.Fatal("missing stocking_id in response")
	}

	// lifecycle row 확인
	lc, err := s.store.GetTankLifecycle(ctx, "tank_01")
	if err != nil || lc == nil {
		t.Fatalf("lifecycle not stored: err=%v lc=%v", err, lc)
	}
	if lc.Status != "active" {
		t.Errorf("expected status=active, got %q", lc.Status)
	}
	if lc.ActiveStockingID != stockingID {
		t.Errorf("stocking_id mismatch: got %q want %q", lc.ActiveStockingID, stockingID)
	}
	if lc.InitialCount != 1000 {
		t.Errorf("initial_count: got %d want 1000", lc.InitialCount)
	}

	// audit 이벤트 확인
	evts, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.stocking.recorded",
		Limit:     10,
	})
	if len(evts) < 1 {
		t.Fatal("expected stocking audit event")
	}

	// 자율 모드 자동 off 확인
	modeRow, _ := s.store.GetTankAutonomousMode(ctx, "tank_01")
	if modeRow == nil || modeRow.Mode != "off" {
		t.Errorf("expected autonomous mode=off after stocking, got %v", modeRow)
	}
	// 모드 변경 이벤트도 적재됐는지
	modeEvts, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.autonomous_mode.changed",
		Limit:     10,
	})
	// observation→off 변경 1건 (observation 설정 + auto-off = 2건)
	if len(modeEvts) < 2 {
		t.Errorf("expected >=2 mode events (set + auto-off), got %d", len(modeEvts))
	}
}

// TestPostStockingRejectsWhileActive — 이미 active lifecycle 있으면 409 CONFLICT_ACTIVE_LIFECYCLE.
func TestPostStockingRejectsWhileActive(t *testing.T) {
	s := newTestServerWithApp(t)

	// 첫 번째 입식
	w1 := postStocking(t, s, "tank_01", defaultStockingBody())
	if w1.Code != http.StatusOK {
		t.Fatalf("first stocking: expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// 두 번째 입식 시도 → 409
	w2 := postStocking(t, s, "tank_01", defaultStockingBody())
	if w2.Code != http.StatusConflict {
		t.Fatalf("second stocking: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w2.Body.Bytes(), &resp)
	if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "CONFLICT_ACTIVE_LIFECYCLE" {
		t.Errorf("expected CONFLICT_ACTIVE_LIFECYCLE, got %v", resp)
	}
}

// TestPostStockingMissingFields — 필수 필드 누락 시 422.
func TestPostStockingMissingFields(t *testing.T) {
	s := newTestServerWithApp(t)

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			"missing species",
			map[string]any{"growth_stage": "growout", "initial_count": 100, "initial_avg_weight_g": 50.0},
			"MISSING_SPECIES",
		},
		{
			"invalid growth_stage",
			map[string]any{"species": "연어", "growth_stage": "adult", "initial_count": 100, "initial_avg_weight_g": 50.0},
			"INVALID_GROWTH_STAGE",
		},
		{
			"zero initial_count",
			map[string]any{"species": "연어", "growth_stage": "growout", "initial_count": 0, "initial_avg_weight_g": 50.0},
			"INVALID_INITIAL_COUNT",
		},
		{
			"zero avg_weight",
			map[string]any{"species": "연어", "growth_stage": "growout", "initial_count": 100, "initial_avg_weight_g": 0.0},
			"INVALID_INITIAL_AVG_WEIGHT",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := postStocking(t, s, "tank_01", c.body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]any
			json.Unmarshal(w.Body.Bytes(), &resp)
			if errObj, _ := resp["error"].(map[string]any); errObj["code"] != c.code {
				t.Errorf("expected code %q, got %v", c.code, resp)
			}
		})
	}
}

// TestPostHarvestHappyPath — 입식 후 출하 → 200 + status=harvested + 자율모드 off.
func TestPostHarvestHappyPath(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 입식
	postStocking(t, s, "tank_01", defaultStockingBody())

	// 출하
	w := postHarvest(t, s, "tank_01", map[string]any{
		"harvested_count":  950,
		"avg_weight_g":     500.0,
		"total_biomass_kg": 475.0,
		"operator_id":      "op1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true: %v", resp)
	}

	// lifecycle status = harvested
	lc, err := s.store.GetTankLifecycle(ctx, "tank_01")
	if err != nil || lc == nil {
		t.Fatalf("lifecycle not found: %v %v", err, lc)
	}
	if lc.Status != "harvested" {
		t.Errorf("expected status=harvested, got %q", lc.Status)
	}

	// harvest audit 이벤트
	evts, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.harvest.recorded",
		Limit:     10,
	})
	if len(evts) < 1 {
		t.Fatal("expected harvest audit event")
	}
}

// TestPostHarvestRejectsWithoutActiveLineage — 활성 lineage 없으면 409.
func TestPostHarvestRejectsWithoutActiveLineage(t *testing.T) {
	s := newTestServerWithApp(t)

	w := postHarvest(t, s, "tank_01", map[string]any{
		"harvested_count": 100, "avg_weight_g": 300.0, "operator_id": "op1",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if errObj, _ := resp["error"].(map[string]any); errObj["code"] != "NO_ACTIVE_LIFECYCLE" {
		t.Errorf("expected NO_ACTIVE_LIFECYCLE, got %v", resp)
	}
}

// TestGetLifecycleEmpty — lifecycle 없음 → 200 with current=null, history=[].
func TestGetLifecycleEmpty(t *testing.T) {
	s := newTestServer(t)

	w := getLifecycle(t, s, "tank_new")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["current"] != nil {
		t.Errorf("expected current=null for fresh tank, got %v", resp["current"])
	}
	history, _ := resp["history"].([]any)
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d items", len(history))
	}
}

// TestGetLifecycleAfterStocking — 입식 후 current 채워짐 + history 에 stocking 이벤트 포함.
func TestGetLifecycleAfterStocking(t *testing.T) {
	s := newTestServerWithApp(t)

	postStocking(t, s, "tank_01", defaultStockingBody())

	w := getLifecycle(t, s, "tank_01")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["current"] == nil {
		t.Error("expected current to be populated after stocking")
	}
	cur, _ := resp["current"].(map[string]any)
	if cur["status"] != "active" {
		t.Errorf("expected status=active, got %v", cur["status"])
	}

	history, _ := resp["history"].([]any)
	if len(history) < 1 {
		t.Fatal("expected at least 1 history item")
	}
	first, _ := history[0].(map[string]any)
	if first["type"] != "stocking" {
		t.Errorf("expected history[0].type=stocking, got %v", first["type"])
	}
}

// TestStateVectorUsesLifecycleFirst — 입식 후 BiologicalContext.Source="lifecycle".
func TestStateVectorUsesLifecycleFirst(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// TankProfile 등록 (fallback 기반)
	if err := s.store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "tank_01",
		DisplayName: "테스트 Cage/Tank",
		Species:     "송어",
		FishCount:   500,
		AvgWeightG:  100.0,
	}); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	// 입식 전 → tank_profile source
	req0 := httptest.NewRequest("GET", "/v1/tanks/tank_01/state-vector", nil)
	v0, _ := s.buildTankStateVector(req0, "tank_01")
	if v0.BiologicalContext.Source != "tank_profile" {
		t.Errorf("before stocking: expected source=tank_profile, got %q", v0.BiologicalContext.Source)
	}

	// 입식
	postStocking(t, s, "tank_01", defaultStockingBody())

	// 입식 후 → lifecycle source + 마릿수 일치
	req1 := httptest.NewRequest("GET", "/v1/tanks/tank_01/state-vector", nil)
	v1, _ := s.buildTankStateVector(req1, "tank_01")
	if v1.BiologicalContext.Source != "lifecycle" {
		t.Errorf("after stocking: expected source=lifecycle, got %q", v1.BiologicalContext.Source)
	}
	if v1.BiologicalContext.FishCount != 1000 {
		t.Errorf("expected fish_count=1000 from lifecycle, got %d", v1.BiologicalContext.FishCount)
	}
	if v1.BiologicalContext.GrowthStage != "growout" {
		t.Errorf("expected growth_stage=growout, got %q", v1.BiologicalContext.GrowthStage)
	}
	if v1.BiologicalContext.StockingID == "" {
		t.Error("expected stocking_id to be set")
	}
}
