package sync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

type fakeApp struct{}

func (fakeApp) AppendEvent(ctx context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error) {
	return 0, nil
}

func openSyncTestStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "sync-test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := storage.Migrate(store, filepath.Join("..", "..", "migrations", "001_init.sql")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateBatchAndBuildEnvelope(t *testing.T) {
	ctx := context.Background()
	store := openSyncTestStore(t)
	now := time.Now().UTC()

	appendEvent := func(id, eventType string, seqTime time.Time) {
		t.Helper()
		_, err := store.AppendEvent(ctx, &storage.Event{
			EventID:       id,
			EventType:     eventType,
			SchemaVersion: "1.0",
			SiteID:        "site_test",
			EdgeID:        "edge_test",
			RecordedAt:    seqTime,
			SourceModule:  "collector",
			PayloadJSON:   `{"ok":true}`,
			EventJSON:     `{"event_id":"` + id + `","event_type":"` + eventType + `","schema_version":"1.0","site_id":"site_test","edge_id":"edge_test","source":{"module":"collector"},"recorded_at":"2026-05-03T09:00:00Z","payload":{"ok":true}}`,
		})
		if err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}

	// Use backend-supported event_types so envelope transform keeps both events.
	// Backend GX10SyncEvent.event_type Literal accepts only a fixed set; edge
	// event_types outside that set are dropped at BuildBatchEnvelope.
	appendEvent("evt_sync_001", "feeding.recorded", now.Add(-1*time.Minute))
	appendEvent("evt_sync_002", "device.health.updated", now)

	if err := createBatch(ctx, fakeApp{}, store, 10); err != nil {
		t.Fatalf("createBatch: %v", err)
	}
	batch, err := store.GetPendingSyncBatch(ctx)
	if err != nil {
		t.Fatalf("GetPendingSyncBatch: %v", err)
	}
	if batch == nil {
		t.Fatal("expected pending batch")
	}

	envelope, err := BuildBatchEnvelope(ctx, store, batch)
	if err != nil {
		t.Fatalf("BuildBatchEnvelope: %v", err)
	}
	if envelope.BatchID != batch.BatchID {
		t.Fatalf("batch id mismatch: %s != %s", envelope.BatchID, batch.BatchID)
	}
	if envelope.SiteID != "site_test" || envelope.EdgeID != "edge_test" {
		t.Fatalf("unexpected site/edge: %#v", envelope)
	}
	if envelope.EventCount != 2 || len(envelope.Events) != 2 {
		t.Fatalf("expected 2 events, got count=%d len=%d", envelope.EventCount, len(envelope.Events))
	}
	if envelope.Events[0]["event_id"] != "evt_sync_001" || envelope.Events[1]["event_id"] != "evt_sync_002" {
		t.Fatalf("events not ordered by sequence: %#v", envelope.Events)
	}

	if err := createBatch(ctx, fakeApp{}, store, 10); err != nil {
		t.Fatalf("second createBatch: %v", err)
	}
	pending, err := store.CountPendingBatches(ctx)
	if err != nil {
		t.Fatalf("CountPendingBatches: %v", err)
	}
	if pending != 1 {
		t.Fatalf("expected no duplicate batch for already batched events, pending=%d", pending)
	}
}
