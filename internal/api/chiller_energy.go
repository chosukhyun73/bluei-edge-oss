package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

const chillerDayKeyPrefix = "chiller.energy.day." // + YYYY-MM-DD → kWh

// IntegrateChillerKWh integrates chiller_power_kw readings (kW × Δt) into kWh.
// 30분 이상 갭(오프라인)은 적산에서 제외. 워커(일별 롤업)와 핸들러가 공유.
func IntegrateChillerKWh(evs []*storage.Event) float64 {
	type pt struct {
		t  time.Time
		kw float64
	}
	pts := make([]pt, 0, len(evs))
	for _, e := range evs {
		var p events.SensorReadingPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.Value == nil || p.Metric != events.MetricChillerPower {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, p.ObservedAt)
		if err != nil {
			continue
		}
		pts = append(pts, pt{t, *p.Value})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].t.Before(pts[j].t) })
	var kwh float64
	for i := 1; i < len(pts); i++ {
		if dt := pts[i].t.Sub(pts[i-1].t).Hours(); dt > 0 && dt < 0.5 {
			kwh += pts[i-1].kw * dt
		}
	}
	return kwh
}

// handleChillerEnergy — GET /v1/chiller/energy?device_id=chiller_1&since=RFC3339
// 24h kW 시계열 + 현재 kW + 누적 kWh(1일/1개월/전체). 누적은 일별 롤업(runtime_kv) 합산.
func (s *Server) handleChillerEnergy(w http.ResponseWriter, r *http.Request) {
	deviceID := r.URL.Query().Get("device_id")
	if deviceID == "" {
		deviceID = "chiller_1"
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		if p, err := time.Parse(time.RFC3339Nano, v); err == nil {
			since = p
		}
	}

	evs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		DeviceID:  deviceID,
		Since:     &since,
		Limit:     5000,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	type point struct {
		Ts       string   `json:"ts"`
		PowerKW  *float64 `json:"power_kw"`
		CurrentA *float64 `json:"current_a"`
	}
	byTs := map[string]*point{}
	for _, e := range evs {
		var p events.SensorReadingPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.Value == nil {
			continue
		}
		pt := byTs[p.ObservedAt]
		if pt == nil {
			pt = &point{Ts: p.ObservedAt}
			byTs[p.ObservedAt] = pt
		}
		v := *p.Value
		switch p.Metric {
		case events.MetricChillerPower:
			pt.PowerKW = &v
		case events.MetricChillerCurrent:
			pt.CurrentA = &v
		}
	}
	pts := make([]*point, 0, len(byTs))
	for _, pt := range byTs {
		pts = append(pts, pt)
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].Ts < pts[j].Ts })

	var latestKW *float64
	for i := len(pts) - 1; i >= 0; i-- {
		if pts[i].PowerKW != nil {
			latestKW = pts[i].PowerKW
			break
		}
	}

	// 누적 kWh — 일별 롤업(워커가 5분마다 갱신) 합산.
	var totalKWh, monthKWh, dayKWh float64
	if kvmap, err := s.store.KVListPrefix(r.Context(), chillerDayKeyPrefix); err == nil {
		today := time.Now().Format("2006-01-02")
		monthAgo := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
		for k, v := range kvmap {
			f, perr := strconv.ParseFloat(v, 64)
			if perr != nil {
				continue
			}
			date := strings.TrimPrefix(k, chillerDayKeyPrefix)
			totalKWh += f
			if date >= monthAgo {
				monthKWh += f
			}
			if date == today {
				dayKWh = f
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"device_id": deviceID,
		"items":     pts,
		"latest_kw": latestKW,
		"day_kwh":   dayKWh,
		"month_kwh": monthKWh,
		"total_kwh": totalKWh,
		"count":     len(pts),
	})
}
