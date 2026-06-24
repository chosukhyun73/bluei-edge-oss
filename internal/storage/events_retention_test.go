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
	got, err := store.SelectEventsOlderThan(ctx, "sensor.reading.recorded", cutoff, 100)
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
	n, err := store.DeleteEventsOlderThanUpToSeq(ctx, "sensor.reading.recorded", cutoff, maxSeq)
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

	got, err := store.SelectEventsOlderThan(ctx, "sensor.reading.recorded", cutoff, 2)
	if err != nil {
		t.Fatalf("SelectEventsOlderThan: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit 2, got %d", len(got))
	}
}
