package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 앱의 기존 어장(해상 가두리)에 붙기: 앱 어장 화면에서 승인 → self-register 가 linked_existing → 운영 중 가두리 받아와 입식 이어받기
// → push 때 platform_tank_id 로 짝지어 올리기 (docs/53 1단계).

type fakeCloud struct {
	mu     sync.Mutex
	bodies map[string][]map[string]any
	linked bool // 앱 어장 화면에서 승인됨(서버가 linked_existing=true)
}

func newFakeCloud(t *testing.T) (*fakeCloud, *httptest.Server) {
	fc := &fakeCloud{bodies: map[string][]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fc.mu.Lock()
		fc.bodies[r.URL.Path] = append(fc.bodies[r.URL.Path], body)
		fc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/gx10/self-register":
			if fc.linked {
				_, _ = w.Write([]byte(`{"ok":true,"farm":{"id":"farm-sea","domain":"cageFarm"},"idempotent":true,"linked_existing":true}`))
			} else {
				_, _ = w.Write([]byte(`{"ok":true,"farm":{"id":"farm-new","domain":"landBased"},"idempotent":false}`))
			}
		case "/gx10/farm-tanks":
			_, _ = w.Write([]byte(`{"ok":true,"farm":{"id":"farm-sea","name":"천양수산","domain":"cageFarm",
				"license_no":"통영양식 제1699호","operator_name":"조석현","address":"경상남도 통영시 산양읍 삼덕지선",
				"boundary":[{"lat":34.787,"lng":128.377},{"lat":34.784,"lng":128.379}]},
				"tanks":[{"platform_tank_id":"1772720998135","tank_code":"수조-03","species":"돌돔","count":10000,
				"avg_weight_g":20,"stocking_date":"2026-03-05","lot_code":"LOT-STOCKING-SB-1772720998135-C1",
				"species_code":"OLF","species_model":{"name_ko":"돌돔","temp_opt_lo":22.0,"temp_opt_hi":25.0,"sgr_c":0.5,"sgr_k":0.35,"fcr_base":1.25,"version":"obsidian"},
				"growth_profiles":[],"risk_rules":[{"severity":"CRITICAL","condition_expr":"water_temp_c >= 28","action_text":"급이 중단"}]}]}`))
		case "/gx10/farm-history":
			since, _ := body["since"].(map[string]any)
			if since["1772720998135"] == "2026-03-10T03:00:00+00:00" {
				_, _ = w.Write([]byte(`{"ok":true,"tanks":[{"platform_tank_id":"1772720998135","gx10_tank_id":"p_1772720998135",
					"readings":[],"has_more":false,"daily_records":[{"date":"2026-03-08","feed_amount_kg":4.4,"feed_type":"PELLET","mortality":0},
					{"date":"2026-03-09","feed_amount_kg":0,"mortality":3}]}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"tanks":[{"platform_tank_id":"1772720998135","gx10_tank_id":"p_1772720998135",
				"station":{"id":"ftgp7","name":"통영 곤리"},
				"readings":[{"observed_at":"2026-03-06T03:00:00+00:00","water_temp_c":11.0,"do_mg_l":null,"source":"satellite_cmems","station_id":"ty004"},
				{"observed_at":"2026-03-10T03:00:00+00:00","water_temp_c":11.6,"do_mg_l":8.1,"source":"nifs_station","station_id":"ftgp7","station_name":"통영 곤리"}],
				"has_more":true,"daily_records":[{"date":"2026-03-08","feed_amount_kg":4.4,"feed_type":"PELLET","mortality":0},
				{"date":"2026-03-09","feed_amount_kg":0,"mortality":3}]}]}`))
		case "/gx10/tanks":
			_, _ = w.Write([]byte(`{"ok":true,"count":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return fc, srv
}

func TestLinkExistingFarmImportsTanksAndStocking(t *testing.T) {
	s := newTestServerWithApp(t)
	fc, srv := newFakeCloud(t)
	fc.linked = true
	ep := srv.URL + "/gx10/sync/batches"
	s.cfg.Sync.Endpoint = &ep
	s.cfg.Edge.DataDir = t.TempDir() // 이어 받기 커서 파일
	s.cfg.Site.SiteID = "cheonyang_01"
	ctx := context.Background()
	creds := config.DeviceCreds{NodeCode: "gx10-t", AccessToken: "tok"}

	s.syncIdentityToCloud(ctx, creds)

	tp, _ := s.store.GetTankProfile(ctx, "p_1772720998135")
	if tp == nil || tp.PlatformTankID != "1772720998135" || tp.DisplayName != "수조-03" ||
		tp.SystemType != "marine_cage" || tp.FishCount != 10000 {
		t.Fatalf("imported profile wrong: %+v", tp)
	}
	lc, _ := s.store.GetTankLifecycle(ctx, "p_1772720998135")
	if lc == nil || lc.Status != "active" || lc.LotNo != "LOT-STOCKING-SB-1772720998135-C1" ||
		lc.InitialCount != 10000 || lc.StockedAt.Format("2006-01-02") != "2026-03-05" {
		t.Fatalf("imported lifecycle wrong: %+v", lc)
	}
	evs, _ := s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventTankStockingRecorded, Limit: 10})
	if len(evs) != 1 {
		t.Fatalf("want 1 stocking event, got %d", len(evs))
	}
	var p events.TankStockingRecordedPayload
	_ = json.Unmarshal([]byte(evs[0].PayloadJSON), &p)
	if p.ImportedFrom != "platform" {
		t.Errorf("stocking event must be marked imported_from=platform, got %q", p.ImportedFrom)
	}

	// 어장·현장 목록 — 대시보드 "등록된 농장 없음" 방지.
	farms, _ := s.store.ListFarms(ctx)
	if len(farms) != 1 || farms[0].FarmID != "farm-sea" || farms[0].LicenseNo != "통영양식 제1699호" ||
		farms[0].Operator != "조석현" {
		t.Fatalf("imported farm wrong: %+v", farms)
	}
	sites, _ := s.store.ListSites(ctx, "farm-sea")
	if len(sites) != 1 || sites[0]["site_id"] != "cheonyang_01" || sites[0]["name"] != "천양수산" {
		t.Fatalf("imported site wrong: %+v", sites)
	}

	// 현장·수조 탭은 그룹을 골라야 수조가 보인다 → 어장 이름 그룹에 가두리가 들어가야 한다.
	g, _ := s.store.GetGroupProfile(ctx, "cheonyang_01")
	inGroup, _ := s.store.ListTanksByGroup(ctx, "cheonyang_01")
	if g == nil || g.Name != "천양수산" || len(inGroup) != 1 || inGroup[0].TankID != "p_1772720998135" {
		t.Fatalf("imported tank must be in farm group: group=%+v tanks=%d", g, len(inGroup))
	}

	push := fc.bodies["/gx10/tanks"][0]["tanks"].([]any)
	if len(push) != 1 || push[0].(map[string]any)["platform_tank_id"] != "1772720998135" {
		t.Fatalf("push must carry platform_tank_id, got %v", push)
	}

	// 재연결(재시작) 시 중복 입식 없음.
	s.syncIdentityToCloud(ctx, creds)
	evs, _ = s.store.QueryEvents(ctx, storage.EventFilter{EventType: events.EventTankStockingRecorded, Limit: 10})
	if len(evs) != 1 {
		t.Errorf("re-sync must not duplicate stocking, got %d events", len(evs))
	}
}

// 어장 지정 없이 승인(기존 흐름) → 새 어장, 앱 수조 받아오기 없음.
func TestNotLinkedKeepsOldBehaviour(t *testing.T) {
	s := newTestServerWithApp(t)
	fc, srv := newFakeCloud(t)
	ep := srv.URL + "/gx10/sync/batches"
	s.cfg.Sync.Endpoint = &ep
	s.cfg.Edge.DataDir = t.TempDir() // 이어 받기 커서 파일
	s.syncIdentityToCloud(context.Background(), config.DeviceCreds{AccessToken: "tok"})
	if len(fc.bodies["/gx10/farm-tanks"]) != 0 {
		t.Error("farm-tanks must not be called unless linked to an existing farm")
	}
}

// 그룹 없이 먼저 가져온 수조(2026-11-17 천양수산)도 다음 동기화 때 어장 그룹에 들어간다.
func TestExistingImportedTankGetsFarmGroup(t *testing.T) {
	s := newTestServerWithApp(t)
	fc, srv := newFakeCloud(t)
	fc.linked = true
	ep := srv.URL + "/gx10/sync/batches"
	s.cfg.Sync.Endpoint = &ep
	s.cfg.Edge.DataDir = t.TempDir()
	s.cfg.Site.SiteID = "cheonyang_01"
	ctx := context.Background()
	if err := s.store.UpsertTankProfile(ctx, &storage.TankProfile{TankID: "p_1772720998135",
		PlatformTankID: "1772720998135", DisplayName: "수조-03", Species: "돌돔", FishCount: 10000,
		AvgWeightG: 20, SystemType: "marine_cage"}); err != nil {
		t.Fatal(err)
	}
	s.syncIdentityToCloud(ctx, config.DeviceCreds{NodeCode: "gx10-t", AccessToken: "tok"})
	tp, _ := s.store.GetTankProfile(ctx, "p_1772720998135")
	if tp.GroupID != "cheonyang_01" || tp.FishCount != 10000 || tp.DisplayName != "수조-03" {
		t.Fatalf("existing tank must join farm group and keep its values: %+v", tp)
	}
	// 먼저 들어온 수조도 입식 어종 기준(돌돔 22–25°C)으로 갱신 — 연어 기준 띠 없앰.
	if len(tp.TargetRanges) != 1 || tp.TargetRanges[0].Metric != "water_temperature" ||
		*tp.TargetRanges[0].Min != 22 || *tp.TargetRanges[0].Max != 25 {
		t.Fatalf("species temp range wrong: %+v", tp.TargetRanges)
	}
	if tp.Metadata["species_code"] != "OLF" || tp.Metadata["risk_rules"] == nil {
		t.Fatalf("species basis must be kept in metadata: %+v", tp.Metadata)
	}
}
