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

// sensor.reading.recorded 는 백엔드가 env_readings 로 투영하는 핵심 수질 타입이다.
// 이 매핑이 빠지면 실시간 수온/DO/pH 가 앱에 전혀 안 뜬다(2026-07-17 실장애).
func TestAllowlist_SensorReadingRecordedMapped(t *testing.T) {
	got, ok := mapEdgeEventTypeToBackend("sensor.reading.recorded")
	if !ok || got != "sensor.reading.recorded" {
		t.Fatalf("sensor.reading.recorded must map to backend; got %q ok=%v", got, ok)
	}
}

// sync.* 자기 메타이벤트(sync.batch.created 등)는 배치 대상에서 제외돼야 한다.
// 포함되면 배치 생성마다 재유입되는 자기증식 루프로 큐를 오염시킨다.
func TestBatching_ExcludesSyncMetaEvents(t *testing.T) {
	store := openSyncTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	appendEvent := func(id, eventType string, seqTime time.Time) {
		if _, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: eventType, SchemaVersion: "1.0",
			SiteID: "site_test", EdgeID: "edge_test", RecordedAt: seqTime,
			SourceModule: "sync",
			PayloadJSON:  `{"ok":true}`,
			EventJSON:    `{"event_id":"` + id + `","event_type":"` + eventType + `","payload":{"ok":true}}`,
		}); err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	appendEvent("evt_meta_1", "sync.batch.created", now.Add(-2*time.Minute))
	appendEvent("evt_real_1", "sensor.reading.recorded", now.Add(-1*time.Minute))
	appendEvent("evt_meta_2", "sync.batch.acknowledged", now)

	unsynced, err := store.ListUnsyncedUnbatchedEvents(ctx, nil, 100)
	if err != nil {
		t.Fatalf("ListUnsyncedUnbatchedEvents: %v", err)
	}
	if len(unsynced) != 1 || unsynced[0].EventID != "evt_real_1" {
		ids := make([]string, len(unsynced))
		for i, e := range unsynced {
			ids[i] = e.EventID + ":" + e.EventType
		}
		t.Fatalf("expected only the non-sync event, got %v", ids)
	}
}

// CollapseRedundantReadings 는 (수조,지표)별 최신 1건만 남기고 나머지 sensor.reading.recorded
// 를 synced 마킹해야 한다. 백엔드 30분 버킷 특성상 중복 raw 전송은 낭비이므로 현재값 스냅샷만 보낸다.
func TestCollapseRedundantReadings(t *testing.T) {
	store := openSyncTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	add := func(id, tank, metric string, sec int) {
		payload := `{"metric":"` + metric + `","value":1,"unit":"x","location":{"tank_id":"` + tank + `"}}`
		if _, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: "sensor.reading.recorded", SchemaVersion: "1.0",
			SiteID: "s", EdgeID: "e", RecordedAt: now.Add(time.Duration(sec) * time.Second),
			SourceModule: "collector", PayloadJSON: payload,
			EventJSON: `{"event_id":"` + id + `","payload":` + payload + `}`,
		}); err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	// tank_01/water_temperature 3건(최신=t3), tank_01/ph 1건, tank_02/water_temperature 2건(최신=w2).
	add("t1", "tank_01", "water_temperature", 1)
	add("t2", "tank_01", "water_temperature", 2)
	add("t3", "tank_01", "water_temperature", 3)
	add("p1", "tank_01", "ph", 1)
	add("w1", "tank_02", "water_temperature", 1)
	add("w2", "tank_02", "water_temperature", 2)

	// device.health 3건(같은 device) → 최신만 남고 2건 collapse.
	for i, id := range []string{"d1", "d2", "d3"} {
		if _, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: "device.health.updated", SchemaVersion: "1.0",
			SiteID: "s", EdgeID: "e", RecordedAt: now.Add(time.Duration(i) * time.Second),
			SourceModule: "collector", SourceDevice: "wq_1team",
			PayloadJSON: `{"device_id":"wq_1team","status":"online"}`,
			EventJSON:   `{"event_id":"` + id + `"}`,
		}); err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}

	n, err := store.CollapseRedundantReadings(ctx)
	if err != nil {
		t.Fatalf("CollapseRedundantReadings: %v", err)
	}
	if n != 5 { // env t1,t2,w1(3) + device d1,d2(2) = 5
		t.Fatalf("collapsed count: got %d, want 5", n)
	}
	remaining, err := store.ListUnsyncedUnbatchedEvents(ctx, []string{"sensor.reading.recorded"}, 100)
	if err != nil {
		t.Fatalf("ListUnsyncedUnbatchedEvents: %v", err)
	}
	got := map[string]bool{}
	for _, e := range remaining {
		got[e.EventID] = true
	}
	if len(got) != 3 || !got["t3"] || !got["p1"] || !got["w2"] {
		t.Fatalf("expected {t3,p1,w2} to remain, got %v", got)
	}
}

// createBatch 는 백엔드 지원 타입만 큐에 넣어야 한다(배치 밀도). media.clip 등
// 미지원 대용량 타입이 큐를 다시 막지 않게 한다.
func TestBatching_OnlySupportedTypesQueued(t *testing.T) {
	store := openSyncTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	appendEvent := func(id, eventType string, seqTime time.Time) {
		if _, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: eventType, SchemaVersion: "1.0",
			SiteID: "site_test", EdgeID: "edge_test", RecordedAt: seqTime,
			SourceModule: "collector", PayloadJSON: `{"ok":true}`,
			EventJSON: `{"event_id":"` + id + `","event_type":"` + eventType + `","payload":{"ok":true}}`,
		}); err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	appendEvent("evt_clip", "media.clip.stored", now.Add(-3*time.Minute))
	appendEvent("evt_env", "sensor.reading.recorded", now.Add(-2*time.Minute))
	appendEvent("evt_snap", "sensor.reading.snapshot", now.Add(-1*time.Minute))

	got, err := store.ListUnsyncedUnbatchedEvents(ctx, supportedEdgeEventTypes(), 100)
	if err != nil {
		t.Fatalf("ListUnsyncedUnbatchedEvents: %v", err)
	}
	if len(got) != 1 || got[0].EventID != "evt_env" {
		ids := make([]string, len(got))
		for i, e := range got {
			ids[i] = e.EventType
		}
		t.Fatalf("expected only supported sensor.reading.recorded, got %v", ids)
	}
}

// ACK 된 배치의 이벤트는 다시 배치에 뽑히면 안 된다.
// 결함(2026-07-19~10-17): ACK 후 이벤트를 synced 로 표시하지 않아, 가장 오래된
// 10건이 매 배치에 다시 뽑혀 새 이벤트가 영영 전송되지 않았다(선두 차단).
func TestAcknowledgedBatchEventsAreNotReselected(t *testing.T) {
	ctx := context.Background()
	store := openSyncTestStore(t)
	now := time.Now().UTC()

	appendFeeding := func(id string, at time.Time) {
		t.Helper()
		_, err := store.AppendEvent(ctx, &storage.Event{
			EventID: id, EventType: "feeding.recorded", SchemaVersion: "1.0",
			SiteID: "site_test", EdgeID: "edge_test", RecordedAt: at, SourceModule: "collector",
			PayloadJSON: `{"ok":true}`,
			EventJSON:   `{"event_id":"` + id + `","event_type":"feeding.recorded","schema_version":"1.0","site_id":"site_test","edge_id":"edge_test","source":{"module":"collector"},"recorded_at":"2026-07-17T09:00:00Z","payload":{"ok":true}}`,
		})
		if err != nil {
			t.Fatalf("AppendEvent %s: %v", id, err)
		}
	}
	appendFeeding("evt_old_1", now.Add(-2*time.Minute))
	appendFeeding("evt_old_2", now.Add(-1*time.Minute))

	if err := createBatch(ctx, fakeApp{}, store, 2); err != nil {
		t.Fatalf("createBatch: %v", err)
	}
	batch, err := store.GetPendingSyncBatch(ctx)
	if err != nil || batch == nil {
		t.Fatalf("GetPendingSyncBatch: batch=%v err=%v", batch, err)
	}
	markBatchAcknowledged(ctx, fakeApp{}, store, batch.BatchID, "remote_1")

	appendFeeding("evt_new_1", now)
	if err := createBatch(ctx, fakeApp{}, store, 2); err != nil {
		t.Fatalf("second createBatch: %v", err)
	}
	next, err := store.GetPendingSyncBatch(ctx)
	if err != nil || next == nil {
		t.Fatalf("expected a new pending batch: batch=%v err=%v", next, err)
	}
	events, err := store.GetSyncBatchEvents(ctx, next.BatchID)
	if err != nil {
		t.Fatalf("GetSyncBatchEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventID != "evt_new_1" {
		ids := make([]string, 0, len(events))
		for _, e := range events {
			ids = append(ids, e.EventID)
		}
		t.Fatalf("acked events were reselected; next batch = %v, want [evt_new_1]", ids)
	}
}

// 생산 기록(docs/53 2단계): 6종이 서버 타입으로 매핑되고, 분조는 출발 수조로 짝짓고,
// 앱에서 이어받은 입식은 올리지 않는다.
func TestProductionEventsMappedForUpload(t *testing.T) {
	want := map[string]string{
		"tank.stocking.recorded":  "tank.stocking",
		"tank.harvest.recorded":   "tank.harvest",
		"tank.transfer.recorded":  "tank.transfer",
		"tank.treatment.recorded": "tank.treatment",
		"tank.mortality.recorded": "tank.mortality",
		"tank.sampling.recorded":  "tank.sampling",
	}
	for edge, backend := range want {
		got, ok := mapEdgeEventTypeToBackend(edge)
		if !ok || got != backend {
			t.Fatalf("%s → %q ok=%v, want %q", edge, got, ok, backend)
		}
	}

	now := time.Now().UTC()
	transfer := &storage.Event{EventID: "evt_tr", EventType: "tank.transfer.recorded", RecordedAt: now,
		PayloadJSON: `{"transfer_id":"tr1","transfer_type":"split","from_tank_id":"p_1","to_tank_id":"t2","moved_count":10}`}
	out, ok := transformEventForBackend(transfer)
	if !ok || out["gx10_tank_id"] != "p_1" || out["event_type"] != "tank.transfer" {
		t.Fatalf("transfer: ok=%v out=%v", ok, out)
	}

	imported := &storage.Event{EventID: "evt_imp", EventType: "tank.stocking.recorded", RecordedAt: now,
		PayloadJSON: `{"stocking_id":"s1","tank_id":"p_1","imported_from":"platform"}`}
	if _, ok := transformEventForBackend(imported); ok {
		t.Fatal("imported_from=platform stocking must not be uploaded")
	}

	own := &storage.Event{EventID: "evt_own", EventType: "tank.stocking.recorded", RecordedAt: now,
		PayloadJSON: `{"stocking_id":"s2","tank_id":"t3","species":"OLF","initial_count":100}`}
	if out, ok := transformEventForBackend(own); !ok || out["gx10_tank_id"] != "t3" {
		t.Fatalf("GX10 stocking must be uploaded: ok=%v out=%v", ok, out)
	}
}
