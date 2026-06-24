package api

import (
	"context"
	"net/http"
	"time"

	"bluei.kr/edge/internal/common"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "alive",
		"edge_id": s.app.EdgeID(),
		"time":    common.FormatTime(common.NowUTC()),
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{
		"config": "ok",
	}
	degradedReasons := []string{}

	// storage ping
	if err := s.store.Ping(ctx); err != nil {
		checks["storage"] = "failed"
		degradedReasons = append(degradedReasons, "storage unavailable: "+err.Error())
	} else {
		checks["storage"] = "ok"
	}

	// check services
	for name, svc := range s.app.Health.All() {
		switch svc.Status {
		case "failed":
			checks[name] = "failed"
			degradedReasons = append(degradedReasons, name+" failed: "+svc.Detail)
		case "degraded", "offline":
			checks[name] = svc.Status
			degradedReasons = append(degradedReasons, name+": "+svc.Detail)
		default:
			checks[name] = svc.Status
		}
	}

	// Determine overall readiness
	statusCode := http.StatusOK
	statusLabel := "ready"
	for _, v := range checks {
		if v == "failed" {
			statusCode = http.StatusServiceUnavailable
			statusLabel = "not_ready"
			break
		}
	}
	if statusCode == http.StatusOK && len(degradedReasons) > 0 {
		statusLabel = "degraded"
	}

	writeJSON(w, statusCode, map[string]any{
		"status":          statusLabel,
		"checks":          checks,
		"degraded_reason": degradedReasons,
	})
}
