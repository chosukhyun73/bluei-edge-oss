package api

// 어장 바깥 환경 이어 받기 (서버 → GX10, 해상 장기 환경 분석 2년, 2026-11-17).
//
// 서버 /gx10/farm-environment 에서 어장 위치의 기상청 부이 30분(바람·풍향·파고·기온),
// 관측소 일별 수온(최저·최고·대표), 오늘·내일 물때 예보를 받아 site_env_observations 에 저장한다.
// 부이가 수십 km 밖 바깥 바다라 참고값 → 해상 안전 판정(environmental_snapshots)에는 넣지 않는다
// (운영자 결정 2026-11-17). 앱 수조가 연결된 GX10 만 부른다(farm-history 와 같은 30분 주기).

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

const (
	farmEnvCursorFile = "farm_env_cursor.json"
	farmEnvMaxPages   = 150 // 부이 2년 ≈ 3.5만 칸 / 페이지 약 330칸
)

type farmEnvCursor struct {
	Marine string `json:"marine,omitempty"`
	Daily  string `json:"daily,omitempty"`
}

func (s *Server) farmEnvCursorPath() string {
	return filepath.Join(s.cfg.Edge.DataDir, farmEnvCursorFile)
}

func (s *Server) loadFarmEnvCursor() farmEnvCursor {
	var c farmEnvCursor
	if b, err := os.ReadFile(s.farmEnvCursorPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func (s *Server) saveFarmEnvCursor(c farmEnvCursor) {
	b, _ := json.Marshal(c)
	if err := os.WriteFile(s.farmEnvCursorPath(), b, 0o600); err != nil {
		slog.Warn("farm-env: cursor save failed", "err", err)
	}
}

func (s *Server) importFarmEnvironment(ctx context.Context, creds config.DeviceCreds) {
	siteID := s.cfg.Site.SiteID
	if siteID == "" {
		return
	}
	headers := map[string]string{"Authorization": "Bearer " + creds.AccessToken}
	if creds.NodeCode != "" {
		headers["X-GX10-Node"] = creds.NodeCode
	}
	cur := s.loadFarmEnvCursor()
	total := 0
	for page := 0; page < farmEnvMaxPages; page++ {
		out, code, err := cloudJSON(ctx, http.MethodPost, s.cloudBase()+"/gx10/farm-environment",
			map[string]any{"since": map[string]string{"marine": cur.Marine, "daily": cur.Daily}}, headers)
		if err != nil {
			slog.Error("farm-env import failed", "err", err)
			break
		}
		if code != http.StatusOK {
			slog.Error("farm-env import rejected", "status", code, "resp", out)
			break
		}
		obs, lastSlot, lastDay := farmEnvObservations(siteID, out, page == 0)
		if err := s.store.UpsertSiteEnvObservations(ctx, obs); err != nil {
			slog.Error("farm-env: save failed", "err", err)
			break // 커서를 진행하지 않는다 — 다음 회차에 다시
		}
		total += len(obs)
		if lastSlot != "" {
			cur.Marine = lastSlot
		}
		if lastDay != "" {
			cur.Daily = lastDay
		}
		s.saveFarmEnvCursor(cur)
		marine, _ := out["marine"].(map[string]any)
		if more, _ := marine["has_more"].(bool); !more {
			break
		}
	}
	slog.Info("farm-env import ok", "saved", total)
}

// farmEnvObservations — 서버 응답 → 저장할 관측. 물때는 첫 페이지에서만(매번 같은 오늘·내일).
func farmEnvObservations(siteID string, out map[string]any, withTide bool) ([]storage.SiteEnvObservation, string, string) {
	var obs []storage.SiteEnvObservation
	add := func(metric string, at time.Time, v any, unit, source string, st map[string]any) {
		f, ok := v.(float64)
		if !ok {
			return
		}
		o := storage.SiteEnvObservation{SiteID: siteID, Metric: metric, ObservedAt: at, Value: f, Unit: unit,
			Source: source, StationID: asString(st["station_id"]), StationName: asString(st["station_name"])}
		if d, ok := st["distance_km"].(float64); ok {
			o.DistanceKM = &d
		}
		obs = append(obs, o)
	}
	lastSlot, lastDay := "", ""
	marine, _ := out["marine"].(map[string]any)
	rows, _ := marine["rows"].([]any)
	for _, it := range rows {
		r, ok := it.(map[string]any)
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, asString(r["observed_at"]))
		if err != nil {
			continue
		}
		lastSlot = asString(r["observed_at"])
		add("wind_speed", at, r["wind_speed_ms"], "m/s", "kma_buoy", r)
		add("wind_direction", at, r["wind_direction_deg"], "deg", "kma_buoy", r)
		add("wave_height", at, r["wave_height_m"], "m", "kma_buoy", r)
		add("air_temperature", at, r["air_temp_c"], "°C", "kma_buoy", r)
	}
	daily, _ := out["daily_water"].(map[string]any)
	drows, _ := daily["rows"].([]any)
	for _, it := range drows {
		r, ok := it.(map[string]any)
		if !ok {
			continue
		}
		day, err := time.Parse("2006-01-02", asString(r["date"]))
		if err != nil {
			continue
		}
		lastDay = asString(r["date"])
		at := day.Add(3 * time.Hour) // 그날 정오(KST)
		src := "nifs_daily"
		if asString(r["quality"]) != "RAW" {
			src = "satellite_daily"
		}
		add("sea_temp_daily_min", at, r["min_c"], "°C", src, r)
		add("sea_temp_daily_max", at, r["max_c"], "°C", src, r)
		add("sea_temp_daily_avg", at, r["avg_c"], "°C", src, r)
	}
	if tide, _ := out["tide"].(map[string]any); withTide && tide != nil {
		kst := time.FixedZone("KST", 9*3600)
		st := map[string]any{"station_id": tide["station_code"]}
		days, _ := tide["days"].([]any)
		for _, it := range days {
			d, _ := it.(map[string]any)
			evs, _ := d["events"].([]any)
			for _, e := range evs {
				ev, _ := e.(map[string]any)
				at, err := time.ParseInLocation("2006-01-02 15:04", asString(d["date"])+" "+asString(ev["time"]), kst)
				if err != nil {
					continue
				}
				metric := "tide_low"
				if asString(ev["kind"]) == "HIGH" {
					metric = "tide_high"
				}
				lvl, ok := ev["level_cm"].(float64)
				if !ok { // 서버는 문자열("162.0")로 준다
					f, err := strconv.ParseFloat(asString(ev["level_cm"]), 64)
					if err != nil {
						continue
					}
					lvl = f
				}
				add(metric, at, lvl, "cm", "khoa_tide_forecast", st)
			}
		}
	}
	return obs, lastSlot, lastDay
}
