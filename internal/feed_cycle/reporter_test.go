package feed_cycle

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

// reporterFakeStore — reporterStore 구현. 이벤트는 event_type 별 canned 목록.
type reporterFakeStore struct {
	cycle   *storage.FeedCycle
	events  map[string][]*storage.Event
	cameras []*storage.CameraProfile
}

func (f *reporterFakeStore) ListCameraProfiles(_ context.Context, _ string) ([]*storage.CameraProfile, error) {
	return f.cameras, nil
}

func (f *reporterFakeStore) GetFeedCycle(_ context.Context, cycleID string) (*storage.FeedCycle, error) {
	if f.cycle != nil && f.cycle.CycleID == cycleID {
		return f.cycle, nil
	}
	return nil, nil
}

func (f *reporterFakeStore) QueryEvents(_ context.Context, filter storage.EventFilter) ([]*storage.Event, error) {
	return f.events[filter.EventType], nil
}

// reporterFakeSink — 적재된 이벤트 캡처.
type reporterFakeSink struct {
	mu      sync.Mutex
	entries []struct {
		EventType string
		CorrID    string
		Payload   map[string]any
	}
}

func (f *reporterFakeSink) AppendEvent(_ context.Context, _, _, _, eventType, corrID string, payload any) (int64, error) {
	b, _ := json.Marshal(payload)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, struct {
		EventType string
		CorrID    string
		Payload   map[string]any
	}{eventType, corrID, p})
	return 1, nil
}

func mkEvent(t *testing.T, payload map[string]any) *storage.Event {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return &storage.Event{PayloadJSON: string(b)}
}

func testReporter(store reporterStore, sink reportSink) *Reporter {
	r := NewReporter(store, sink)
	r.pollInterval = time.Millisecond
	r.maxWait = 50 * time.Millisecond
	return r
}

func completedCycleRow() *storage.FeedCycle {
	started := time.Date(2026, 7, 5, 0, 57, 0, 0, time.UTC)
	completed := started.Add(3*time.Minute + 32*time.Second)
	return &storage.FeedCycle{
		CycleID:            "cycle_test_01",
		TankID:             "tank_01",
		Mode:               "adaptive",
		Priority:           "ai_advisory",
		TargetAmountG:      169,
		PulsesExecuted:     7,
		TotalAmountG:       197.4,
		TerminationReason:  "satiation_or_target",
		StartedAt:          started,
		CompletedAt:        &completed,
		ActualTotalAmountG: 0,
	}
}

// 비전 결과가 전부 도착한 경우 — 조기 확정 + 요약 필드 검증.
func TestReporterFullVisionResults(t *testing.T) {
	store := &reporterFakeStore{
		cycle: completedCycleRow(),
		events: map[string][]*storage.Event{
			events.EventMediaClipStored: {
				mkEvent(t, map[string]any{"clip_id": "clip_a", "camera_id": "cam_top",
					"evidence": map[string]any{"phase": "feeding", "cycle_id": "cycle_test_01"}}),
				mkEvent(t, map[string]any{"clip_id": "clip_b", "camera_id": "cam_uw",
					"evidence": map[string]any{"phase": "feeding", "cycle_id": "cycle_test_01"}}),
			},
			events.EventVisionObservationRecorded: {
				mkEvent(t, map[string]any{"camera_id": "cam_top", "model_version": "v1",
					"scores":   map[string]any{"feeding_activity": 0.4},
					"evidence": map[string]any{"cycle_id": "cycle_test_01"}}),
				mkEvent(t, map[string]any{"camera_id": "cam_uw", "model_version": "v1",
					"scores":   map[string]any{"feeding_activity": 0.6},
					"evidence": map[string]any{"cycle_id": "cycle_test_01"}}),
				// 다른 사이클 — 제외돼야 함.
				mkEvent(t, map[string]any{"camera_id": "cam_top",
					"scores":   map[string]any{"feeding_activity": 0.9},
					"evidence": map[string]any{"cycle_id": "cycle_other"}}),
			},
			vision_pipeline.EventAgitationRecorded: {
				// 피크 +100% (baseline 2 → peak 4), 진정 도달 → 놀람 아님.
				mkEvent(t, map[string]any{"clip_id": "clip_a", "cycle_id": "cycle_test_01",
					"camera_id": "cam_top", "n_seconds": 200, "baseline": 2.0, "peak": 4.0,
					"peak_at_s": 20, "calm_reached": true}),
				// 말미(90%) 피크 + 진정 미도달 → 놀람 의심.
				mkEvent(t, map[string]any{"clip_id": "clip_b", "cycle_id": "cycle_test_01",
					"camera_id": "cam_uw", "n_seconds": 200, "baseline": 2.0, "peak": 3.0,
					"peak_at_s": 180, "calm_reached": false}),
			},
		},
	}
	sink := &reporterFakeSink{}

	testReporter(store, sink).OnCycleComplete(context.Background(), "cycle_test_01")

	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 report event, got %d", len(sink.entries))
	}
	got := sink.entries[0]
	if got.EventType != events.EventFeedCycleReport || got.CorrID != "cycle_test_01" {
		t.Fatalf("unexpected event type/corr: %s / %s", got.EventType, got.CorrID)
	}
	p := got.Payload
	if p["tank_id"] != "tank_01" || p["pulses_executed"].(float64) != 7 {
		t.Errorf("cycle facts mismatch: %v", p)
	}
	if p["clip_count"].(float64) != 2 {
		t.Errorf("clip_count = %v, want 2", p["clip_count"])
	}
	lrcn := p["lrcn"].(map[string]any)
	if lrcn["mean_score"].(float64) != 0.5 {
		t.Errorf("lrcn mean_score = %v, want 0.5", lrcn["mean_score"])
	}
	if n := len(lrcn["per_camera"].([]any)); n != 2 {
		t.Errorf("lrcn per_camera = %d, want 2 (다른 사이클 제외)", n)
	}
	ag := p["agitation"].(map[string]any)
	if ag["max_peak_rise_frac"].(float64) != 1.0 {
		t.Errorf("max_peak_rise_frac = %v, want 1.0", ag["max_peak_rise_frac"])
	}
	if ag["startle_suspect"].(bool) != true {
		t.Errorf("startle_suspect = %v, want true (cam_uw 말미 피크)", ag["startle_suspect"])
	}
}

// 비전 결과가 끝내 안 오는 경우 — maxWait 후 있는 것만으로 보고 (lrcn/agitation null).
func TestReporterTimeoutWithoutVision(t *testing.T) {
	store := &reporterFakeStore{cycle: completedCycleRow(), events: map[string][]*storage.Event{}}
	sink := &reporterFakeSink{}

	start := time.Now()
	testReporter(store, sink).OnCycleComplete(context.Background(), "cycle_test_01")
	if time.Since(start) > 5*time.Second {
		t.Fatalf("reporter did not respect maxWait")
	}

	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 report event, got %d", len(sink.entries))
	}
	p := sink.entries[0].Payload
	if p["clip_count"].(float64) != 0 {
		t.Errorf("clip_count = %v, want 0", p["clip_count"])
	}
	if p["lrcn"] != nil || p["agitation"] != nil {
		t.Errorf("expected null vision blocks, got lrcn=%v agitation=%v", p["lrcn"], p["agitation"])
	}
	if p["termination_reason"] != "satiation_or_target" {
		t.Errorf("termination_reason = %v", p["termination_reason"])
	}
}

// 사이클 행이 없으면 보고하지 않음 (warn 만).
func TestReporterUnknownCycle(t *testing.T) {
	store := &reporterFakeStore{events: map[string][]*storage.Event{}}
	sink := &reporterFakeSink{}
	testReporter(store, sink).OnCycleComplete(context.Background(), "cycle_missing")
	if len(sink.entries) != 0 {
		t.Fatalf("expected no report, got %d", len(sink.entries))
	}
}
