package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// postSampling — POST /v1/tanks/{id}/sampling 호출 helper.
func postSampling(t *testing.T, s *Server, tankID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/tanks/"+tankID+"/sampling", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTankSamplingRoute(w, req)
	return w
}

// getSampling — GET /v1/tanks/{id}/sampling 호출 helper.
func getSampling(t *testing.T, s *Server, tankID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/tanks/"+tankID+"/sampling", nil)
	w := httptest.NewRecorder()
	s.handleTankSamplingRoute(w, req)
	return w
}

// defaultSamplingBody — 최소 유효 sampling 바디.
func defaultSamplingBody() map[string]any {
	return map[string]any{
		"sampled_count": 30,
		"avg_weight_g":  200.0,
		"health_score":  8,
		"sampled_at":    time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
		"recorded_by":   "op1",
	}
}

// TestPostSamplingHappyPathWithLifecycle — 입식 후 sampling → 200,
// projection upsert, stocking_id 자동 채움.
func TestPostSamplingHappyPathWithLifecycle(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 입식 먼저
	wStock := postStocking(t, s, "tank_01", defaultStockingBody())
	if wStock.Code != http.StatusOK {
		t.Fatalf("stocking: %d %s", wStock.Code, wStock.Body.String())
	}
	var stockResp map[string]any
	json.Unmarshal(wStock.Body.Bytes(), &stockResp)
	activeStockingID, _ := stockResp["stocking_id"].(string)

	// sampling 입력
	w := postSampling(t, s, "tank_01", defaultSamplingBody())
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true: %v", resp)
	}
	samplingID, _ := resp["sampling_id"].(string)
	if samplingID == "" {
		t.Fatal("missing sampling_id")
	}

	// stocking_id 자동 채움 확인
	respStockingID, _ := resp["stocking_id"].(string)
	if respStockingID != activeStockingID {
		t.Errorf("stocking_id mismatch: got %q want %q", respStockingID, activeStockingID)
	}

	// projection upsert 확인
	proj, err := s.store.GetTankSampling(ctx, "tank_01")
	if err != nil || proj == nil {
		t.Fatalf("GetTankSampling: err=%v proj=%v", err, proj)
	}
	if proj.LatestSamplingID != samplingID {
		t.Errorf("projection sampling_id: got %q want %q", proj.LatestSamplingID, samplingID)
	}
	if proj.StockingID != activeStockingID {
		t.Errorf("projection stocking_id: got %q want %q", proj.StockingID, activeStockingID)
	}

	// warnings 없어야 함 (활성 lifecycle 있으므로)
	if _, ok := resp["warnings"]; ok {
		t.Errorf("expected no warnings with active lifecycle, got %v", resp["warnings"])
	}

	// audit 이벤트 확인
	evts, _ := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "tank.sampling.recorded",
		Limit:     10,
	})
	if len(evts) < 1 {
		t.Fatal("expected sampling audit event")
	}
}

// TestPostSamplingWithoutLifecycle — 입식 없이 sampling → 200 (거부 X),
// warning note 포함, stocking_id 빈 문자.
func TestPostSamplingWithoutLifecycle(t *testing.T) {
	s := newTestServerWithApp(t)

	w := postSampling(t, s, "tank_01", defaultSamplingBody())
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true: %v", resp)
	}
	// stocking_id 빈 문자
	stockingID, _ := resp["stocking_id"].(string)
	if stockingID != "" {
		t.Errorf("expected empty stocking_id without lifecycle, got %q", stockingID)
	}
	// warnings 포함
	warnings, _ := resp["warnings"].([]any)
	if len(warnings) == 0 {
		t.Errorf("expected warnings without active lifecycle, got %v", resp)
	}
}

// TestPostSamplingMissingFields — sampled_count=0 → 422, avg_weight_g=0 → 422.
func TestPostSamplingMissingFields(t *testing.T) {
	s := newTestServerWithApp(t)

	// sampled_count=0
	body1 := defaultSamplingBody()
	body1["sampled_count"] = 0
	w1 := postSampling(t, s, "tank_01", body1)
	if w1.Code != http.StatusUnprocessableEntity {
		t.Errorf("sampled_count=0: expected 422, got %d: %s", w1.Code, w1.Body.String())
	}

	// avg_weight_g=0
	body2 := defaultSamplingBody()
	body2["avg_weight_g"] = 0.0
	w2 := postSampling(t, s, "tank_01", body2)
	if w2.Code != http.StatusUnprocessableEntity {
		t.Errorf("avg_weight_g=0: expected 422, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestPostSamplingInvalidWeightRange — min > avg → 422 INVALID_WEIGHT_RANGE.
func TestPostSamplingInvalidWeightRange(t *testing.T) {
	s := newTestServerWithApp(t)

	body := defaultSamplingBody()
	body["min_weight_g"] = 300.0 // avg=200, min=300 이면 min > avg
	w := postSampling(t, s, "tank_01", body)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != "INVALID_WEIGHT_RANGE" {
		t.Errorf("expected INVALID_WEIGHT_RANGE, got %v", resp)
	}
}

// TestPostSamplingInvalidHealthScore — health_score=15 → 422 INVALID_HEALTH_SCORE.
func TestPostSamplingInvalidHealthScore(t *testing.T) {
	s := newTestServerWithApp(t)

	body := defaultSamplingBody()
	body["health_score"] = 15
	w := postSampling(t, s, "tank_01", body)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != "INVALID_HEALTH_SCORE" {
		t.Errorf("expected INVALID_HEALTH_SCORE, got %v", resp)
	}
}

// TestGetSamplingHistory — sampling 2건 입력 → history 배열에 2건 (시간 역순).
func TestGetSamplingHistory(t *testing.T) {
	s := newTestServerWithApp(t)

	// 첫 번째: 2시간 전
	body1 := defaultSamplingBody()
	body1["sampled_at"] = time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	body1["avg_weight_g"] = 100.0
	w1 := postSampling(t, s, "tank_01", body1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first sampling: %d %s", w1.Code, w1.Body.String())
	}

	// 두 번째: 1시간 전 (더 최신)
	body2 := defaultSamplingBody()
	body2["sampled_at"] = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	body2["avg_weight_g"] = 120.0
	w2 := postSampling(t, s, "tank_01", body2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second sampling: %d %s", w2.Code, w2.Body.String())
	}

	// GET history
	wGet := getSampling(t, s, "tank_01")
	if wGet.Code != http.StatusOK {
		t.Fatalf("GET sampling: %d %s", wGet.Code, wGet.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(wGet.Body.Bytes(), &resp)
	history, _ := resp["history"].([]any)
	if len(history) != 2 {
		t.Fatalf("expected 2 history items, got %d: %v", len(history), history)
	}
	// history[0] 이 더 최신 (avg_weight_g=120)
	first, _ := history[0].(map[string]any)
	if first["avg_weight_g"] != 120.0 {
		t.Errorf("first history item should be newest (120.0), got %v", first["avg_weight_g"])
	}
}

// TestStateVectorIncludesLastSampling — sampling 후 state vector BiologicalContext 채워짐.
func TestStateVectorIncludesLastSampling(t *testing.T) {
	s := newTestServerWithApp(t)

	// 입식 + sampling
	postStocking(t, s, "tank_01", defaultStockingBody())
	postSampling(t, s, "tank_01", defaultSamplingBody())

	req := httptest.NewRequest(http.MethodGet, "/v1/tanks/tank_01/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_01")
	if err != nil {
		t.Fatalf("buildTankStateVector: %v", err)
	}
	bc := v.BiologicalContext
	if bc.LastSampledAt == "" {
		t.Error("expected LastSampledAt to be set after sampling")
	}
	if bc.LastSampledAvgWeight != 200.0 {
		t.Errorf("LastSampledAvgWeight: got %f want 200.0", bc.LastSampledAvgWeight)
	}
}

// TestStateVectorOverdueSampling — sampling 시각 40일 전 → notes 에 "권장" 포함.
func TestStateVectorOverdueSampling(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 입식
	postStocking(t, s, "tank_01", defaultStockingBody())

	// 40일 전 sampling 을 UpsertTankSampling 으로 직접 주입
	sampledAt40 := time.Now().UTC().Add(-40 * 24 * time.Hour)
	if err := s.store.UpsertTankSampling(ctx, &storage.TankSampling{
		TankID:           "tank_01",
		LatestSamplingID: "sampling_old",
		SampledCount:     20,
		AvgWeightG:       150.0,
		SampledAt:        sampledAt40,
		RecordedBy:       "op1",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertTankSampling: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tanks/tank_01/state-vector", nil)
	v, err := s.buildTankStateVector(req, "tank_01")
	if err != nil {
		t.Fatalf("buildTankStateVector: %v", err)
	}
	bc := v.BiologicalContext
	foundNote := false
	for _, n := range bc.Notes {
		if samplingContains(n, "권장") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Errorf("expected '권장' note for overdue sampling, notes: %v", bc.Notes)
	}
	if bc.DaysSinceSampling < 35 {
		t.Errorf("expected DaysSinceSampling >= 35, got %d", bc.DaysSinceSampling)
	}
}

func samplingContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestPostSamplingTriggersCalibration — 30일 입식 + 충분한 급이 후 sampling →
// 응답에 fcr_calibration.performed=true.
func TestPostSamplingTriggersCalibration(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()

	// 입식 (30일 전으로 직접 주입)
	stockedAt := time.Now().UTC().Add(-30 * 24 * time.Hour)
	lc := &storage.TankLifecycle{
		TankID:            "tank_cal",
		ActiveStockingID:  "stocking_cal_01",
		Species:           "참돔",
		GrowthStage:       "juvenile",
		InitialCount:      1000,
		InitialAvgWeightG: 50.0,
		StockedAt:         stockedAt,
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	if err := s.store.UpsertTankLifecycle(ctx, lc); err != nil {
		t.Fatalf("upsert lifecycle: %v", err)
	}

	// feeding 이벤트 직접 DB 삽입 (fed_at 제어: 입식 15일 후)
	importFeedingEvent(t, s, "tank_cal", 10000.0, stockedAt.Add(15*24*time.Hour))

	// sampling POST — avg_weight=58g (ΔBiomass=8000g, FCR=10000/8000=1.25)
	body := map[string]any{
		"sampled_count": 30,
		"avg_weight_g":  58.0,
		"sampled_at":    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
		"recorded_by":   "op1",
	}
	w := postSampling(t, s, "tank_cal", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	calResp, ok := resp["fcr_calibration"].(map[string]any)
	if !ok {
		t.Fatalf("fcr_calibration missing from response: %v", resp)
	}
	if calResp["performed"] != true {
		t.Errorf("expected performed=true, got %v (reason=%v)", calResp["performed"], calResp["reason"])
	}
	if calResp["calibrated_fcr"] == nil {
		t.Error("expected calibrated_fcr in response")
	}
}

// TestPostSamplingCalibrationFailsGracefully — stocking 없이 sampling →
// fcr_calibration.performed=false + reason 포함, sampling 자체는 200.
func TestPostSamplingCalibrationFailsGracefully(t *testing.T) {
	s := newTestServerWithApp(t)

	w := postSampling(t, s, "tank_nograce", defaultSamplingBody())
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even without lifecycle, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)

	calResp, ok := resp["fcr_calibration"].(map[string]any)
	if !ok {
		t.Fatalf("fcr_calibration missing: %v", resp)
	}
	if calResp["performed"] != false {
		t.Errorf("expected performed=false without lifecycle, got %v", calResp["performed"])
	}
	if calResp["reason"] == "" || calResp["reason"] == nil {
		t.Errorf("expected reason in calibration response, got %v", calResp)
	}
}

// importFeedingEvent — feeding 이벤트를 store 에 직접 AppendEvent.
// fed_at 타임스탬프를 자유롭게 제어하기 위해 raw event 삽입.
func importFeedingEvent(t *testing.T, s *Server, tankID string, amountG float64, fedAt time.Time) {
	t.Helper()
	p := events.FeedingRecordedPayload{
		FeedingID:   "feed_" + fedAt.Format("20060102150405"),
		TankID:      tankID,
		Source:      events.FeedingSourceManual,
		FeedAmountG: amountG,
		FedAt:       fedAt.Format(time.RFC3339Nano),
		Quality:     events.QualityOK,
	}
	b, _ := json.Marshal(p)
	evt := &storage.Event{
		EventID:       p.FeedingID,
		EventType:     events.EventFeedingRecorded,
		SchemaVersion: "1.0",
		SiteID:        "site_test",
		EdgeID:        "edge_test",
		RecordedAt:    fedAt,
		SourceModule:  "test",
		PayloadJSON:   string(b),
		EventJSON:     string(b),
	}
	if _, err := s.store.AppendEvent(context.Background(), evt); err != nil {
		t.Fatalf("importFeedingEvent: %v", err)
	}
}
