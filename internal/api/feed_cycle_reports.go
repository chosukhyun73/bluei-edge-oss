package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// handleFeedCycleReports — GET /v1/feed-cycles/reports?tank_id=&limit=
//
// 사이클 완료 자동 보고(feed.cycle.report) 최근 목록. 대시보드 "급이 보고" 카드가
// 폴링한다. payload 를 그대로 반환 (최신순).
func (s *Server) handleFeedCycleReports(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	tankID := r.URL.Query().Get("tank_id")

	// tank 필터는 Go 단 — 이벤트 자체가 사이클당 1건이라 스캔 윈도우(200)로 충분.
	evs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventFeedCycleReport,
		Limit:     200,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	items := make([]map[string]any, 0, limit)
	for _, e := range evs {
		var p map[string]any
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if tankID != "" {
			if tid, _ := p["tank_id"].(string); tid != tankID {
				continue
			}
		}
		items = append(items, p)
		if len(items) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
