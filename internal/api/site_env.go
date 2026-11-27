package api

// GET /v1/site-env/daily?days=730 — 해상 장기 환경 분석(최대 2년) 일별 최저·평균·최고 (2026-11-17).
//   - tank: 수조 수온·DO (관측 시각 기준. 해상 가두리는 관측소 값을 그 가두리 값으로 쓴다)
//   - site: 바깥 환경 — 기상청 부이 바람·파고·기온, 관측소 일별 수온(sea_temp_daily_*)
//   - tide: 오늘·내일 물때 예보, sources: 지금 쓰는 부이·관측소(거리)
// 부이는 참고값이라 위험일 기준(풍속 12m/s, 파고 2m)은 표시용이다(안전 판정 X).

import (
	"net/http"
	"time"
)

func (s *Server) handleSiteEnvDaily(w http.ResponseWriter, r *http.Request) {
	days := intParam(r, "days", 365, 731)
	siteID := r.URL.Query().Get("site_id")
	if siteID == "" {
		siteID = s.cfg.Site.SiteID
	}
	loc := time.FixedZone("KST", 9*3600)
	if tz := s.cfg.Site.Timezone; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	now := time.Now()
	_, offset := now.In(loc).Zone()
	y, m, d := now.In(loc).Date()
	since := time.Date(y, m, d, 0, 0, 0, 0, loc).AddDate(0, 0, -days)

	tank, err := s.store.AggregateTankSensorDailyByObserved(r.Context(), since, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	site, err := s.store.AggregateSiteEnvDaily(r.Context(), siteID, since, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	tide, _ := s.store.ListSiteEnvObservations(r.Context(), siteID, "", now.Add(-3*time.Hour), now.Add(48*time.Hour))
	tideOut := []map[string]any{}
	for _, o := range tide {
		if o.Metric == "tide_high" || o.Metric == "tide_low" {
			tideOut = append(tideOut, map[string]any{"kind": o.Metric, "at": o.ObservedAt, "level_cm": o.Value})
		}
	}
	sources := map[string]any{}
	for metric, key := range map[string]string{"wind_speed": "buoy", "sea_temp_daily_avg": "daily_water"} {
		recent, _ := s.store.ListSiteEnvObservations(r.Context(), siteID, metric, now.AddDate(0, 0, -3), now.Add(time.Hour))
		if n := len(recent); n > 0 {
			o := recent[n-1]
			sources[key] = map[string]any{"station_id": o.StationID, "station_name": o.StationName,
				"distance_km": o.DistanceKM, "source": o.Source, "observed_at": o.ObservedAt}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"site_id":    siteID,
		"days":       days,
		"since":      since.Format("2006-01-02"),
		"tank":       tank,
		"site":       site,
		"tide":       tideOut,
		"sources":    sources,
		"thresholds": map[string]float64{"wind_speed": 12, "wave_height": 2},
	})
}
