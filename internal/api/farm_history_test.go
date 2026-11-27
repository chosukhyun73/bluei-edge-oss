package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 앱 어장에 붙으면 앱 수조의 관측소 수온·DO·위성 수온·급이·폐사가 GX10 기록으로 들어오고,
// 다시 받아도 중복되지 않으며, 마릿수는 바뀌지 않는다.
func TestLinkedFarmImportsHistory(t *testing.T) {
	s := newTestServerWithApp(t)
	fc, srv := newFakeCloud(t)
	fc.linked = true
	ep := srv.URL + "/gx10/sync/batches"
	s.cfg.Sync.Endpoint = &ep
	s.cfg.Edge.DataDir = t.TempDir() // 이어 받기 커서 파일
	ctx := context.Background()
	creds := config.DeviceCreds{NodeCode: "gx10-t", AccessToken: "tok"}

	s.syncIdentityToCloud(ctx, creds)

	// 첫 페이지 has_more → 커서(마지막 observed_at)로 두 번째 호출.
	calls := fc.bodies["/gx10/farm-history"]
	if len(calls) != 2 {
		t.Fatalf("want 2 farm-history calls (paging), got %d", len(calls))
	}
	if got := calls[1]["since"].(map[string]any)["1772720998135"]; got != "2026-03-10T03:00:00+00:00" {
		t.Fatalf("second call must send cursor, got %v", got)
	}

	readings, _ := s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventSensorReadingRecorded, Limit: 50})
	if len(readings) != 3 { // 위성 수온 1 + 관측소 수온 1 + 관측소 DO 1
		t.Fatalf("want 3 readings, got %d", len(readings))
	}
	var sat events.SensorReadingPayload
	for _, e := range readings {
		var p events.SensorReadingPayload
		_ = json.Unmarshal([]byte(e.PayloadJSON), &p)
		if p.Raw["imported_from"] != "platform" || p.Location.TankID != "p_1772720998135" {
			t.Fatalf("reading must be marked platform import on the tank: %+v", p)
		}
		if p.Raw["source"] == "satellite_cmems" {
			sat = p
		}
	}
	if sat.Metric != events.MetricWaterTemperature || *sat.Value != 11.0 || sat.ObservedAt != "2026-03-06T03:00:00Z" {
		t.Fatalf("satellite reading wrong: %+v", sat)
	}
	env, _ := s.store.ListTankEnvironment(ctx, "p_1772720998135")
	if len(env) == 0 {
		t.Fatal("current tank environment must be projected from imported readings")
	}

	feeds, _ := s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventFeedingRecorded, Limit: 10})
	morts, _ := s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventTankMortalityRecorded, Limit: 10})
	if len(feeds) != 1 || len(morts) != 1 { // 급이 0kg 날은 급이 기록 없음, 폐사 0 날은 폐사 기록 없음
		t.Fatalf("want 1 feeding + 1 mortality, got %d + %d", len(feeds), len(morts))
	}
	var f events.FeedingRecordedPayload
	_ = json.Unmarshal([]byte(feeds[0].PayloadJSON), &f)
	if f.FeedAmountG != 4400 || f.FedAt != "2026-03-08T03:00:00Z" || f.Evidence["imported_from"] != "platform" {
		t.Fatalf("feeding wrong: %+v", f)
	}
	tp, _ := s.store.GetTankProfile(ctx, "p_1772720998135")
	if tp.FishCount != 10000 {
		t.Errorf("imported mortality must not change fish count (app count is authoritative), got %d", tp.FishCount)
	}

	if _, err := os.Stat(s.farmHistoryCursorPath()); err != nil {
		t.Fatalf("cursor must be saved: %v", err)
	}

	// 다시 받아도(재시작) 중복 없음.
	_ = os.Remove(s.farmHistoryCursorPath())
	s.syncIdentityToCloud(ctx, creds)
	readings, _ = s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventSensorReadingRecorded, Limit: 50})
	feeds, _ = s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventFeedingRecorded, Limit: 10})
	if len(readings) != 3 || len(feeds) != 1 {
		t.Errorf("re-import must not duplicate, got %d readings %d feeds", len(readings), len(feeds))
	}
}
