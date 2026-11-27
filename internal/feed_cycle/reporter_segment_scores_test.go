package feed_cycle

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/inference"
	"bluei.kr/edge/internal/storage"
)

// scoreSegments — 펄스 이벤트 + 탑뷰 클립 → 채점 요청 창 계산 → 이벤트 적재 검증.

type segScoreFakeStore struct {
	events []*storage.Event
}

func (f *segScoreFakeStore) GetFeedCycle(context.Context, string) (*storage.FeedCycle, error) {
	return nil, nil
}
func (f *segScoreFakeStore) ListCameraProfiles(context.Context, string) ([]*storage.CameraProfile, error) {
	return nil, nil
}
func (f *segScoreFakeStore) QueryEvents(_ context.Context, filter storage.EventFilter) ([]*storage.Event, error) {
	var out []*storage.Event
	for _, e := range f.events {
		if e.EventType == filter.EventType {
			out = append(out, e)
		}
	}
	return out, nil
}

type segScoreFakeSink struct {
	appended []struct {
		eventType string
		payload   any
	}
}

func (f *segScoreFakeSink) AppendEvent(_ context.Context, _, _, _, eventType, _ string, payload any) (int64, error) {
	f.appended = append(f.appended, struct {
		eventType string
		payload   any
	}{eventType, payload})
	return int64(len(f.appended)), nil
}

type fakeSegScorer struct {
	gotReq inference.LRCNSegmentScoresRequest
}

func (f *fakeSegScorer) SegmentScores(_ context.Context, req inference.LRCNSegmentScoresRequest) (inference.LRCNSegmentScoresResponse, error) {
	f.gotReq = req
	score1, score2 := 0.61, 0.32
	return inference.LRCNSegmentScoresResponse{
		ModelVersion: "best-r2-test",
		Scores: []inference.LRCNSegmentScore{
			{PulseNo: 1, FeedingScore: &score1},
			{PulseNo: 2, FeedingScore: &score2},
		},
		InferenceMs: 1234,
	}, nil
}

func TestScoreSegments_TopViewClipAndEvent(t *testing.T) {
	start := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	clipStart := start.Add(2 * time.Second)

	pulseEvent := func(no int, completedAt time.Time) *storage.Event {
		p, _ := json.Marshal(map[string]any{
			"cycle_id": "cyc_1", "pulse_no": no,
			"completed_at":       completedAt.Format(time.RFC3339Nano),
			"actual_duration_ms": 3000,
		})
		return &storage.Event{EventType: events.EventFeedPulseCompleted, PayloadJSON: string(p)}
	}
	clipEvent := func(cameraID string) *storage.Event {
		p, _ := json.Marshal(map[string]any{
			"clip_id": "clip_" + cameraID, "camera_id": cameraID,
			"uri":        "/var/captures/" + cameraID + ".mp4",
			"started_at": clipStart.Format(time.RFC3339Nano),
			"evidence":   map[string]any{"cycle_id": "cyc_1", "phase": "feeding"},
		})
		return &storage.Event{EventType: events.EventMediaClipStored, PayloadJSON: string(p)}
	}

	store := &segScoreFakeStore{events: []*storage.Event{
		pulseEvent(1, start.Add(10*time.Second)),
		pulseEvent(2, start.Add(80*time.Second)),
		clipEvent("cam_underwater_7_1"), // 수중 — 반드시 건너뛰어야 함
		clipEvent("cam_top_7_1"),
	}}
	sink := &segScoreFakeSink{}
	scorer := &fakeSegScorer{}
	rp := NewReporter(store, sink)
	rp.SetSegmentScorer(scorer)

	row := &storage.FeedCycle{CycleID: "cyc_1", TankID: "tank_01", StartedAt: start}
	rp.scoreSegments(context.Background(), row, start)

	// 수중 클립은 제외, 탑뷰 클립으로 요청됐는지
	if scorer.gotReq.ClipPath != "/var/captures/cam_top_7_1.mp4" {
		t.Fatalf("expected top-view clip path, got %q", scorer.gotReq.ClipPath)
	}
	if len(scorer.gotReq.Segments) != 2 {
		t.Fatalf("expected 2 segment windows, got %d", len(scorer.gotReq.Segments))
	}
	// 오프셋은 클립 시작 기준이어야 함 (세그먼트 절대시각 - 클립 시작)
	for _, w := range scorer.gotReq.Segments {
		if w.OffsetStartMs < 0 || w.OffsetEndMs <= w.OffsetStartMs {
			t.Fatalf("invalid window: %+v", w)
		}
	}

	// 이벤트 적재 검증
	if len(sink.appended) != 1 {
		t.Fatalf("expected 1 appended event, got %d", len(sink.appended))
	}
	if sink.appended[0].eventType != events.EventVisionSegmentScoresRecorded {
		t.Fatalf("unexpected event type %s", sink.appended[0].eventType)
	}
	payload := sink.appended[0].payload.(map[string]any)
	if payload["model_version"] != "best-r2-test" || payload["cycle_id"] != "cyc_1" {
		t.Fatalf("unexpected payload: %v", payload)
	}
	scores := payload["scores"].([]map[string]any)
	if len(scores) != 2 || scores[0]["feeding_score"].(float64) != 0.61 {
		t.Fatalf("unexpected scores: %v", scores)
	}
}

func TestScoreSegments_NoScorerOrNoClipIsNoop(t *testing.T) {
	store := &segScoreFakeStore{}
	sink := &segScoreFakeSink{}
	rp := NewReporter(store, sink)
	row := &storage.FeedCycle{CycleID: "cyc_x", TankID: "tank_01", StartedAt: time.Now()}

	// scorer 미설정 → noop
	rp.scoreSegments(context.Background(), row, row.StartedAt)
	// scorer 설정 + 데이터 없음 → noop (이벤트 적재 없음)
	rp.SetSegmentScorer(&fakeSegScorer{})
	rp.scoreSegments(context.Background(), row, row.StartedAt)

	if len(sink.appended) != 0 {
		t.Fatalf("expected no events, got %d: %v", len(sink.appended), sink.appended)
	}
}
