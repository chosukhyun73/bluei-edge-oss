package api

// 앱 수조의 지난 기록 이어 받기 (앱 → GX10, docs/53 1단계).
//
// 기존 앱 어장(해상 가두리)에 붙은 GX10 은 서버 /gx10/farm-history 에서 짝지어진 수조의
// 관측소 수온·DO(30분), 관측값 없는 날의 위성 수온, 급이·폐사 기록을 받아 GX10 이벤트로
// 저장한다. 해상 가두리는 GX10 센서가 없어 이 관측소 값이 그 가두리의 수온이다(운영자 결정
// 2026-11-17). 앱 분석과 같은 값을 쓰도록 GX10 이 직접 NIFS 를 읽지 않고 서버를 거친다.
//
// - 이벤트 ID 를 결정적으로 만들어 다시 받아도 중복 저장하지 않는다.
// - 모든 기록에 imported_from=platform 을 남겨, 서버로 다시 올라가 두 번 쌓이지 않게 한다.
// - 폐사는 기록만 남기고 마릿수는 건드리지 않는다(마릿수는 앱에서 받은 값이 정본).
// - 실패는 로그만 — 다음 회차에 커서부터 이어 받는다.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
)

const (
	farmHistoryInterval   = 30 * time.Minute
	farmHistoryMaxPages   = 50
	farmHistoryCursorFile = "farm_history_cursor.json"
	platformImportDevice  = "platform_import"
)

func (s *Server) farmHistoryCursorPath() string {
	return filepath.Join(s.cfg.Edge.DataDir, farmHistoryCursorFile)
}

func (s *Server) loadFarmHistoryCursor() map[string]string {
	cur := map[string]string{}
	if b, err := os.ReadFile(s.farmHistoryCursorPath()); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	return cur
}

func (s *Server) saveFarmHistoryCursor(cur map[string]string) {
	b, _ := json.Marshal(cur)
	if err := os.WriteFile(s.farmHistoryCursorPath(), b, 0o600); err != nil {
		slog.Warn("farm-history: cursor save failed", "err", err)
	}
}

// hasPlatformLinkedTanks — 앱에서 가져온 수조가 있을 때만 이어 받는다(육상 GX10 등은 호출 안 함).
func (s *Server) hasPlatformLinkedTanks(ctx context.Context) bool {
	tanks, err := s.store.ListTankProfiles(ctx)
	if err != nil {
		return false
	}
	for _, t := range tanks {
		if t.PlatformTankID != "" {
			return true
		}
	}
	return false
}

// RunFarmHistoryLoop — 30분마다 앱 수조 기록을 이어 받는다. main.go 에서 go 로 실행.
func (s *Server) RunFarmHistoryLoop(ctx context.Context) {
	t := time.NewTicker(farmHistoryInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			creds, err := config.LoadDeviceCreds(s.cfg.Edge.DataDir)
			if err != nil || creds.AccessToken == "" || !s.hasPlatformLinkedTanks(ctx) {
				continue
			}
			s.importFarmHistory(ctx, creds)
			s.importFarmEnvironment(ctx, creds) // 바깥 환경(부이·일별 수온·물때)
		}
	}
}

func (s *Server) importFarmHistory(ctx context.Context, creds config.DeviceCreds) {
	headers := map[string]string{"Authorization": "Bearer " + creds.AccessToken}
	if creds.NodeCode != "" {
		headers["X-GX10-Node"] = creds.NodeCode
	}
	cur := s.loadFarmHistoryCursor()
	total := 0
	for page := 0; page < farmHistoryMaxPages; page++ {
		out, code, err := cloudJSON(ctx, http.MethodPost, s.cloudBase()+"/gx10/farm-history",
			map[string]any{"since": cur}, headers)
		if err != nil {
			slog.Error("farm-history import failed", "err", err)
			break
		}
		if code != http.StatusOK {
			slog.Error("farm-history import rejected", "status", code, "resp", out)
			break
		}
		more := false
		items, _ := out["tanks"].([]any)
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			n, last, hasMore := s.applyFarmHistoryTank(ctx, m)
			total += n
			if last != "" {
				cur[asString(m["platform_tank_id"])] = last
			}
			more = more || hasMore
		}
		s.saveFarmHistoryCursor(cur)
		if !more {
			break
		}
	}
	slog.Info("farm-history import ok", "recorded", total)
}

// applyFarmHistoryTank — 한 수조분 저장. (새로 저장한 건수, 마지막 observed_at, has_more)
func (s *Server) applyFarmHistoryTank(ctx context.Context, m map[string]any) (int, string, bool) {
	pid := asString(m["platform_tank_id"])
	tankID := asString(m["gx10_tank_id"])
	if pid == "" || tankID == "" {
		return 0, "", false
	}
	if p, err := s.store.GetTankProfile(ctx, tankID); err != nil || p == nil {
		slog.Warn("farm-history: GX10 에 없는 수조 — 건너뜀", "tank_id", tankID)
		return 0, "", false
	}
	type rec struct {
		id, eventType string
		payload       any
	}
	var recs []rec
	last := ""
	readings, _ := m["readings"].([]any)
	for _, it := range readings {
		r, ok := it.(map[string]any)
		if !ok {
			continue
		}
		obsRaw := asString(r["observed_at"])
		obs, err := time.Parse(time.RFC3339Nano, obsRaw)
		if err != nil {
			continue
		}
		last = obsRaw
		source := asString(r["source"])
		sensorID := "platform_" + source
		if sid := asString(r["station_id"]); sid != "" && source == "nifs_station" {
			sensorID = "station_" + sid
		}
		raw := map[string]any{"imported_from": "platform", "source": source,
			"station_id": r["station_id"], "station_name": r["station_name"]}
		for _, mt := range []struct {
			key, metric, unit string
		}{{"water_temp_c", events.MetricWaterTemperature, "°C"}, {"do_mg_l", events.MetricDissolvedOxygen, "mg/L"}} {
			v, ok := r[mt.key].(float64)
			if !ok {
				continue
			}
			id := fmt.Sprintf("imp_%s_%s_%s_%s", pid, mt.metric, source, obs.UTC().Format("20060102T150405Z"))
			val := v
			recs = append(recs, rec{id, events.EventSensorReadingRecorded, events.SensorReadingPayload{
				ReadingID: id, SensorID: sensorID, DeviceID: platformImportDevice,
				Metric: mt.metric, Value: &val, Unit: mt.unit, Quality: events.QualityOK,
				ObservedAt: obs.UTC().Format(time.RFC3339Nano),
				Location:   events.Location{TankID: tankID, PlatformTankID: pid},
				Raw:        raw,
			}})
		}
	}
	daily, _ := m["daily_records"].([]any)
	for _, it := range daily {
		d, ok := it.(map[string]any)
		if !ok {
			continue
		}
		day, err := time.Parse("2006-01-02", asString(d["date"]))
		if err != nil {
			continue
		}
		at := day.Add(3 * time.Hour).UTC().Format(time.RFC3339Nano) // 그날 정오(KST) — 앱 일별 기록엔 시각이 없다
		if kg := asFloat(d["feed_amount_kg"]); kg > 0 {
			id := fmt.Sprintf("imp_feed_%s_%s", pid, day.Format("20060102"))
			recs = append(recs, rec{id, events.EventFeedingRecorded, events.FeedingRecordedPayload{
				FeedingID: id, TankID: tankID, Source: events.FeedingSourceManual,
				FeedAmountG: kg * 1000, FeedType: asString(d["feed_type"]), FedAt: at,
				RecordedBy: platformImportDevice, Quality: events.QualityOK,
				Evidence: map[string]any{"imported_from": "platform"},
			}})
		}
		if dead := int(asFloat(d["mortality"])); dead > 0 {
			id := fmt.Sprintf("imp_mort_%s_%s", pid, day.Format("20060102"))
			recs = append(recs, rec{id, events.EventTankMortalityRecorded, events.TankMortalityRecordedPayload{
				MortalityID: id, TankID: tankID, DeadCount: dead, ObservedAt: at,
				OperatorID: platformImportDevice, Notes: "imported_from=platform",
			}})
		}
	}
	if len(recs) == 0 {
		hasMore, _ := m["has_more"].(bool)
		return 0, last, hasMore
	}
	ids := make([]string, len(recs))
	for i, r := range recs {
		ids[i] = r.id
	}
	existing, err := s.store.FilterExistingEventIDs(ctx, ids)
	if err != nil {
		slog.Error("farm-history: existing check failed", "err", err)
		return 0, "", false // 커서를 진행하지 않는다 — 다음 회차에 다시
	}
	n := 0
	for _, r := range recs {
		if existing[r.id] {
			continue
		}
		if _, err := s.app.AppendEventWithID(ctx, r.id, "api", "platform_import", platformImportDevice,
			r.eventType, r.id, r.payload); err != nil {
			slog.Error("farm-history: append failed", "event_id", r.id, "err", err)
			return n, "", false
		}
		n++
	}
	hasMore, _ := m["has_more"].(bool)
	return n, last, hasMore
}
