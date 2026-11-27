package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

func appendAt(t *testing.T, store storage.Store, id, eventType string, at time.Time) {
	t.Helper()
	_, err := store.AppendEvent(context.Background(), &storage.Event{
		EventID:       id,
		EventType:     eventType,
		SchemaVersion: "1.0",
		SiteID:        "site_test",
		EdgeID:        "edge_test",
		RecordedAt:    at,
		SourceModule:  "collector",
		PayloadJSON:   `{"v":1}`,
		EventJSON:     fmt.Sprintf(`{"event_id":%q}`, id),
	})
	if err != nil {
		t.Fatalf("AppendEvent %s: %v", id, err)
	}
}

// EvidencePhase 필터: payload 의 evidence.phase 로 분리 조회되는지 검증.
// (상시 baseline 이 희소 feeding 클립을 최근 N건 윈도우 밖으로 밀어내던 버그 방지.)
func TestQueryEventsEvidencePhaseFilter(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC)

	appendPhase := func(id, phase string) {
		t.Helper()
		_, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: "media.clip.stored", SchemaVersion: "1.0",
			SiteID: "site_test", EdgeID: "edge_test", RecordedAt: at, SourceModule: "vision",
			PayloadJSON: fmt.Sprintf(`{"clip_id":%q,"evidence":{"phase":%q}}`, id, phase),
			EventJSON:   fmt.Sprintf(`{"event_id":%q}`, id),
		})
		if err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	// baseline 다수 + feeding 소수 (실제 상황 모사).
	for i := 0; i < 20; i++ {
		appendPhase(fmt.Sprintf("base_%d", i), "baseline")
	}
	appendPhase("feed_1", "feeding")
	appendPhase("feed_2", "feeding")

	feeding, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: "media.clip.stored", EvidencePhase: "feeding", Limit: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(feeding) != 2 {
		t.Errorf("feeding phase 필터 결과 %d건, want 2 (baseline 에 파묻히면 안 됨)", len(feeding))
	}
	baseline, _ := store.QueryEvents(ctx, storage.EventFilter{
		EventType: "media.clip.stored", EvidencePhase: "baseline", Limit: 200,
	})
	if len(baseline) != 20 {
		t.Errorf("baseline phase 필터 결과 %d건, want 20", len(baseline))
	}

	// PayloadClipID 필터: 특정 급이 클립을 baseline 다수에 파묻혀도 Limit 1 로 직접 조회.
	// (play/exclude 핸들러가 recent-N 스캔 대신 이 필터로 404 방지.)
	one, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: "media.clip.stored", PayloadClipID: "feed_2", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].EventID != "feed_2" {
		t.Errorf("clip_id 필터 결과 %d건, want feed_2 1건", len(one))
	}
}

func TestSelectAndDeleteEventsOlderThan(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	old1 := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	old2 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 5, 23, 8, 0, 0, 0, time.UTC)

	appendAt(t, store, "evt_old1", "sensor.reading.recorded", old1)
	appendAt(t, store, "evt_old2", "sensor.reading.recorded", old2)
	appendAt(t, store, "evt_recent", "sensor.reading.recorded", recent)
	appendAt(t, store, "evt_health_old", "device.health.updated", old1) // other type — must be untouched
	appendAt(t, store, "evt_runtime", "runtime.started", old1)          // never a retention target

	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	// Select returns only the two old sensor events, ascending by sequence.
	got, err := store.SelectEventsOlderThan(ctx, "sensor.reading.recorded", storage.MetricFilter{}, cutoff, 100)
	if err != nil {
		t.Fatalf("SelectEventsOlderThan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 old sensor events, got %d", len(got))
	}
	if got[0].Sequence >= got[1].Sequence {
		t.Fatalf("expected ascending sequence, got %d then %d", got[0].Sequence, got[1].Sequence)
	}

	// Delete bounded to the batch's max sequence removes exactly those two.
	maxSeq := got[len(got)-1].Sequence
	n, err := store.DeleteEventsOlderThanUpToSeq(ctx, "sensor.reading.recorded", storage.MetricFilter{}, cutoff, maxSeq)
	if err != nil {
		t.Fatalf("DeleteEventsOlderThanUpToSeq: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 deleted, got %d", n)
	}

	// Recent sensor event survives.
	remaining, err := store.QueryEvents(ctx, storage.EventFilter{EventType: "sensor.reading.recorded"})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(remaining) != 1 || remaining[0].EventID != "evt_recent" {
		t.Fatalf("expected only evt_recent to remain, got %+v", remaining)
	}

	// Other event types untouched.
	health, _ := store.QueryEvents(ctx, storage.EventFilter{EventType: "device.health.updated"})
	if len(health) != 1 {
		t.Fatalf("device.health.updated must be untouched, got %d", len(health))
	}
	rt, _ := store.QueryEvents(ctx, storage.EventFilter{EventType: "runtime.started"})
	if len(rt) != 1 {
		t.Fatalf("runtime.started must be untouched, got %d", len(rt))
	}
}

func TestSelectEventsOlderThan_LimitAndPagination(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		appendAt(t, store, fmt.Sprintf("evt_%d", i), "sensor.reading.recorded", base.Add(time.Duration(i)*time.Hour))
	}
	cutoff := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	got, err := store.SelectEventsOlderThan(ctx, "sensor.reading.recorded", storage.MetricFilter{}, cutoff, 2)
	if err != nil {
		t.Fatalf("SelectEventsOlderThan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
}

// Metric 필터: 수온만 장기 보존하는 이중 룰(수온 365일 / 그 외 1일)이
// 서로의 행을 침범하지 않는지 검증.
func TestSelectAndDeleteEventsOlderThan_MetricFilter(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	old := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	appendMetric := func(id, metric string) {
		t.Helper()
		_, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: "sensor.reading.recorded", SchemaVersion: "1.0",
			SiteID: "site_test", EdgeID: "edge_test", RecordedAt: old, SourceModule: "collector",
			PayloadJSON: fmt.Sprintf(`{"metric":%q,"value":1}`, metric),
			EventJSON:   fmt.Sprintf(`{"event_id":%q}`, id),
		})
		if err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	appendMetric("evt_temp", "water_temperature")
	appendMetric("evt_ph", "ph")
	appendMetric("evt_do", "dissolved_oxygen")
	appendAt(t, store, "evt_no_metric", "sensor.reading.recorded", old) // metric 필드 없음

	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	include := storage.MetricFilter{Include: []string{"water_temperature"}}
	exclude := storage.MetricFilter{Exclude: []string{"water_temperature"}}

	// Include: 수온만.
	got, err := store.SelectEventsOlderThan(ctx, "sensor.reading.recorded", include, cutoff, 100)
	if err != nil {
		t.Fatalf("SelectEventsOlderThan(include): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("include filter: expected 1 temp event, got %d", len(got))
	}

	// Exclude: 수온 제외 나머지 전부 (metric 없는 행 포함).
	got, err = store.SelectEventsOlderThan(ctx, "sensor.reading.recorded", exclude, cutoff, 100)
	if err != nil {
		t.Fatalf("SelectEventsOlderThan(exclude): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("exclude filter: expected 3 non-temp events, got %d", len(got))
	}

	// Exclude 룰의 delete 는 수온 행을 남긴다.
	maxSeq := got[len(got)-1].Sequence
	n, err := store.DeleteEventsOlderThanUpToSeq(ctx, "sensor.reading.recorded", exclude, cutoff, maxSeq)
	if err != nil {
		t.Fatalf("DeleteEventsOlderThanUpToSeq(exclude): %v", err)
	}
	if n != 3 {
		t.Fatalf("exclude delete: expected 3 deleted, got %d", n)
	}
	remaining, err := store.QueryEvents(ctx, storage.EventFilter{EventType: "sensor.reading.recorded"})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(remaining) != 1 || remaining[0].EventID != "evt_temp" {
		t.Fatalf("expected only evt_temp to remain, got %+v", remaining)
	}
}
