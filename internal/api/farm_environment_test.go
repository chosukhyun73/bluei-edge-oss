package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
)

// 서버의 바깥 환경(부이·일별 수온·물때)을 현장 관측으로 저장하고, 커서로 이어 받으며,
// 해상 안전 판정(environmental_snapshots)에는 넣지 않는다.
func TestImportFarmEnvironment(t *testing.T) {
	s := newTestServer(t)
	s.cfg.Site.SiteID = "cheonyang_01"
	s.cfg.Edge.DataDir = t.TempDir()
	var mu sync.Mutex
	var sinces []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		sinces = append(sinces, body["since"].(map[string]any))
		n := len(sinces)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = w.Write([]byte(`{"ok":true,
			"marine":{"has_more":true,"rows":[
			  {"observed_at":"2024-11-17T00:00:00+00:00","station_id":"22188","station_name":"통영","distance_km":45.9,
			   "wind_speed_ms":7.0,"wind_direction_deg":300,"wave_height_m":0.5,"air_temp_c":null}]},
			"daily_water":{"rows":[{"date":"2024-11-17","station_id":"ty004","station_name":"통영 영운","distance_km":4.75,
			   "min_c":19.4,"max_c":20.2,"avg_c":19.8,"quality":"RAW"}]},
			"tide":{"station_code":"DT_0014","days":[{"date":"2026-11-18","events":[
			   {"kind":"HIGH","time":"03:01","level_cm":"162.0"},{"kind":"LOW","time":"08:32","level_cm":"110.0"}]}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"marine":{"has_more":false,"rows":[
			  {"observed_at":"2024-11-17T00:30:00+00:00","station_id":"22188","wind_speed_ms":6.5,"wave_height_m":0.6}]},
			"daily_water":{"rows":[]},"tide":{"station_code":"DT_0014","days":[]}}`))
	}))
	t.Cleanup(srv.Close)
	ep := srv.URL + "/gx10/sync/batches"
	s.cfg.Sync.Endpoint = &ep
	ctx := context.Background()

	s.importFarmEnvironment(ctx, config.DeviceCreds{AccessToken: "tok"})

	if len(sinces) != 2 || sinces[1]["marine"] != "2024-11-17T00:00:00+00:00" || sinces[1]["daily"] != "2024-11-17" {
		t.Fatalf("second call must carry cursors, got %v", sinces)
	}
	all, err := s.store.ListSiteEnvObservations(ctx, "cheonyang_01", "", time.Unix(0, 0), time.Now().Add(72*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, o := range all {
		got[o.Metric]++
	}
	want := map[string]int{"wind_speed": 2, "wind_direction": 1, "wave_height": 2, // 기온 null → 저장 안 함
		"sea_temp_daily_min": 1, "sea_temp_daily_max": 1, "sea_temp_daily_avg": 1, "tide_high": 1, "tide_low": 1}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("metric %s: want %d got %d (all %v)", k, v, got[k], got)
		}
	}
	tide, _ := s.store.ListSiteEnvObservations(ctx, "cheonyang_01", "tide_high", time.Unix(0, 0), time.Now().Add(72*time.Hour))
	if tide[0].ObservedAt.UTC().Format(time.RFC3339) != "2026-11-17T18:01:00Z" || tide[0].Value != 162 {
		t.Fatalf("tide high 03:01 KST 162cm, got %+v", tide[0])
	}
	daily, _ := s.store.ListSiteEnvObservations(ctx, "cheonyang_01", "sea_temp_daily_avg", time.Unix(0, 0), time.Now())
	if daily[0].Source != "nifs_daily" || daily[0].StationName != "통영 영운" || daily[0].ObservedAt.UTC().Hour() != 3 {
		t.Fatalf("daily row, got %+v", daily[0])
	}
	snaps, _ := s.store.ListRecentEnvironmentalSnapshots(ctx, "cheonyang_01", 5)
	if len(snaps) != 0 {
		t.Fatalf("buoy values must not feed the safety gate, got %d snapshots", len(snaps))
	}

	// 다시 받아도 덮어쓰기라 늘지 않는다.
	sinces = nil
	s.saveFarmEnvCursor(farmEnvCursor{})
	s.importFarmEnvironment(ctx, config.DeviceCreds{AccessToken: "tok"})
	again, _ := s.store.ListSiteEnvObservations(ctx, "cheonyang_01", "", time.Unix(0, 0), time.Now().Add(72*time.Hour))
	if len(again) != len(all) {
		t.Fatalf("re-import must not duplicate: %d → %d", len(all), len(again))
	}
}
