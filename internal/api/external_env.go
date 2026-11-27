package api

// GET /v1/external-env?hours=24 — 외부 환경 시계열 (NIFS 연안 수온 등).
// 그룹개요 수온 차트의 "수취구 권역 수온" 오버레이가 소비 (2026-07-17).

import (
	"encoding/json"
	"net/http"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

func (s *Server) handleExternalEnv(w http.ResponseWriter, r *http.Request) {
	hours := intParam(r, "hours", 24, 24*14)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	evs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventExternalEnvRecorded, Since: &since, Limit: hours * 8,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	type point struct {
		ObservedAt  string  `json:"observed_at"`
		Value       float64 `json:"value"`
		Metric      string  `json:"metric"`
		StationCd   string  `json:"station_cd"`
		StationName string  `json:"station_name"`
	}
	items := make([]point, 0, len(evs))
	for _, e := range evs {
		var p point
		if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil && p.ObservedAt != "" {
			items = append(items, p)
		}
	}
	// QueryEvents 는 sequence DESC — 차트용으로 시간 오름차순 반전.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "hours": hours})
}
