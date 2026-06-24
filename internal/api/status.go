package api

import (
	"context"
	"net/http"
	"time"

	"bluei.kr/edge/internal/common"
	rtime "bluei.kr/edge/internal/runtime"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	uptime := time.Since(s.app.StartedAt())

	// Alert counts
	alerts, _ := s.store.ListOpenAlerts(ctx)
	alertCounts := map[string]int{"info": 0, "warning": 0, "critical": 0}
	for _, a := range alerts {
		alertCounts[a.Severity]++
	}

	// Sync status
	unsyncedCount, _ := s.store.UnsyncedCount(ctx)
	oldestAge, _ := s.store.OldestUnsyncedAge(ctx)

	svcs := s.app.Health.All()
	svcOut := make(map[string]any, len(svcs))
	for name, ss := range svcs {
		m := map[string]any{"status": ss.Status}
		if ss.LastEventAt != nil {
			m["last_event_at"] = common.FormatTime(*ss.LastEventAt)
		}
		svcOut[name] = m
	}

	// Inject sync detail
	syncDetail := map[string]any{
		"status":                  "offline",
		"unsynced_events":         unsyncedCount,
		"oldest_unsynced_age_sec": int(oldestAge.Seconds()),
	}
	if s, ok := svcs["sync"]; ok {
		syncDetail["status"] = s.Status
	}
	svcOut["sync"] = syncDetail

	writeJSON(w, http.StatusOK, map[string]any{
		"site_id":     s.app.SiteID(),
		"edge_id":     s.app.EdgeID(),
		"version":     rtime.Version,
		"started_at":  common.FormatTime(s.app.StartedAt()),
		"uptime_sec":  int(uptime.Seconds()),
		"mode":        s.cfg.Edge.Mode,
		"health":      s.app.Health.OverallHealth(),
		"services":    svcOut,
		"open_alerts": alertCounts,
	})
}
