package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// getWeightProjection — GET /v1/tanks/{id}/weight-projection 호출 helper.
func getWeightProjection(t *testing.T, s *Server, tankID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/tanks/"+tankID+"/weight-projection", nil)
	w := httptest.NewRecorder()
	s.handleTankWeightProjection(w, req)
	return w
}

// TestGetWeightProjectionNoLifecycle — 활성 lifecycle 없으면 200 + ok=false.
func TestGetWeightProjectionNoLifecycle(t *testing.T) {
	s := newTestServerWithApp(t)

	w := getWeightProjection(t, s, "tank_fresh")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != false {
		t.Errorf("expected ok=false, got: %v", resp["ok"])
	}
	if resp["quality"] != "no_lifecycle" {
		t.Errorf("expected quality=no_lifecycle, got: %v", resp["quality"])
	}
}

// TestGetWeightProjectionAfterStocking — 입식 후 GET → 200, projection 채워짐.
func TestGetWeightProjectionAfterStocking(t *testing.T) {
	s := newTestServerWithApp(t)

	// 입식
	wStock := postStocking(t, s, "tank_w1", map[string]any{
		"species":              "참돔",
		"growth_stage":         "juvenile",
		"initial_count":        1000,
		"initial_avg_weight_g": 50.0,
		"operator_id":          "op1",
	})
	if wStock.Code != http.StatusOK {
		t.Fatalf("stocking failed: %d %s", wStock.Code, wStock.Body.String())
	}

	w := getWeightProjection(t, s, "tank_w1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got: %v", resp)
	}
	if resp["species"] != "참돔" {
		t.Errorf("expected species=참돔, got %v", resp["species"])
	}
	proj, ok := resp["projection"].(map[string]any)
	if !ok {
		t.Fatalf("expected projection object, got: %v", resp["projection"])
	}
	if proj["anchor_source"] != "stocking" {
		t.Errorf("expected anchor_source=stocking, got %v", proj["anchor_source"])
	}
	if proj["quality"] == nil {
		t.Error("expected quality field in projection")
	}
}

// TestWeightProjectionTankIDFromPath — 라우트 helper 검증.
func TestWeightProjectionTankIDFromPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/v1/tanks/tank_01/weight-projection", "tank_01"},
		{"/v1/tanks/tank_x_y/weight-projection", "tank_x_y"},
		{"/v1/tanks/tank_01/sampling", ""},
		{"/v1/tanks//weight-projection", ""},
		{"/v1/tanks/a/b/weight-projection", ""},
	}
	for _, c := range cases {
		if got := weightProjectionTankID(c.path); got != c.want {
			t.Errorf("path %q: got %q, want %q", c.path, got, c.want)
		}
	}
}

// TestStateVectorIncludesEstimatedWeight — 입식 + feeding 후 state vector 에 추정값 포함.
func TestStateVectorIncludesEstimatedWeight(t *testing.T) {
	s := newTestServerWithApp(t)

	// 입식 (2일 전 타임스탬프로)
	stockedAt := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	wStock := postStocking(t, s, "tank_sv1", map[string]any{
		"species":              "참돔",
		"growth_stage":         "juvenile",
		"initial_count":        1000,
		"initial_avg_weight_g": 50.0,
		"stocked_at":           stockedAt,
		"operator_id":          "op1",
	})
	if wStock.Code != http.StatusOK {
		t.Fatalf("stocking failed: %d %s", wStock.Code, wStock.Body.String())
	}

	// feeding event 직접 append (2일 전 이후 시점)
	feedPayload := events.FeedingRecordedPayload{
		FeedingID:   "feed_sv1",
		TankID:      "tank_sv1",
		FeederID:    "feeder_01",
		Source:      "manual",
		FeedAmountG: 14000.0, // 14kg — 충분히 체중 증가
		FedAt:       time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano),
		Quality:     "ok",
	}
	if _, err := s.app.AppendEvent(t.Context(), "test", "feeder", "feeder_01",
		events.EventFeedingRecorded, "feed_sv1", feedPayload); err != nil {
		t.Fatalf("append feeding: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/tanks/tank_sv1/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_sv1")
	if err != nil {
		t.Fatalf("buildTankStateVector: %v", err)
	}

	bio := v.BiologicalContext
	if bio.EstimatedAvgWeightG <= bio.AvgWeightG {
		t.Errorf("expected EstimatedAvgWeightG > InitialAvgWeightG (%.1f), got %.1f",
			bio.AvgWeightG, bio.EstimatedAvgWeightG)
	}
	if bio.EstimationAnchor != "stocking" {
		t.Errorf("expected EstimationAnchor=stocking, got %q", bio.EstimationAnchor)
	}
	if bio.EstimationQuality == "" {
		t.Error("expected EstimationQuality to be set")
	}
}

// TestStateVectorEstimationSwitchesToSamplingAnchor — stocking + sampling 후
// state vector 의 EstimationAnchor = "sampling".
func TestStateVectorEstimationSwitchesToSamplingAnchor(t *testing.T) {
	s := newTestServerWithApp(t)

	stockedAt := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(time.RFC3339Nano)
	wStock := postStocking(t, s, "tank_sv2", map[string]any{
		"species":              "연어",
		"growth_stage":         "growout",
		"initial_count":        500,
		"initial_avg_weight_g": 100.0,
		"stocked_at":           stockedAt,
		"operator_id":          "op1",
	})
	if wStock.Code != http.StatusOK {
		t.Fatalf("stocking: %d", wStock.Code)
	}

	// sampling (30일 전)
	sampledAt := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	wSample := postSampling(t, s, "tank_sv2", map[string]any{
		"sampled_count": 20,
		"avg_weight_g":  280.0, // stocking W₀=100g 보다 큰 값
		"sampled_at":    sampledAt,
		"recorded_by":   "op1",
	})
	if wSample.Code != http.StatusOK {
		t.Fatalf("sampling: %d %s", wSample.Code, wSample.Body.String())
	}

	req := httptest.NewRequest("GET", "/v1/tanks/tank_sv2/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_sv2")
	if err != nil {
		t.Fatalf("buildTankStateVector: %v", err)
	}

	bio := v.BiologicalContext
	if bio.EstimationAnchor != "sampling" {
		t.Errorf("expected EstimationAnchor=sampling after sampling, got %q", bio.EstimationAnchor)
	}

	// ProjectionResult 의 AnchorWeightG 도 sampling 값이어야 한다
	// (state vector 에선 EstimatedAvgWeightG 만 노출되므로 /weight-projection 으로 확인)
	wProj := getWeightProjection(t, s, "tank_sv2")
	var projResp map[string]any
	json.Unmarshal(wProj.Body.Bytes(), &projResp)
	proj := projResp["projection"].(map[string]any)
	if proj["anchor_source"] != "sampling" {
		t.Errorf("expected anchor_source=sampling in /weight-projection, got %v", proj["anchor_source"])
	}
	if anchorW, _ := proj["anchor_weight_g"].(float64); anchorW != 280.0 {
		t.Errorf("expected anchor_weight_g=280, got %v", proj["anchor_weight_g"])
	}

	// 빈 store 에서의 /weight-projection 라우트 검증 (t.Context 없는 환경 대비)
	_ = storage.Store(nil) // import 유지용
}
