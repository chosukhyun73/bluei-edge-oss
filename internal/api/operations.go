package api

import (
	"context"
	"net/http"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	rtime "bluei.kr/edge/internal/runtime"
)

func (s *Server) handleOperationalStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	profiles, err := s.store.ListTankProfiles(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	devices, err := s.store.ListDeviceStatuses(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	cameras, err := s.store.ListCameraStatuses(ctx, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	alerts, err := s.store.ListOpenAlerts(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	unsynced, _ := s.store.UnsyncedCount(ctx)
	oldestAge, _ := s.store.OldestUnsyncedAge(ctx)
	pendingBatches, _ := s.store.CountPendingBatches(ctx)
	failedBatches, _ := s.store.CountFailedBatches(ctx)

	deviceCounts := statusCounter()
	for _, d := range devices {
		deviceCounts.add(stringValue(d["status"]))
	}
	cameraCounts := statusCounter()
	for _, c := range cameras {
		cameraCounts.add(c.Status)
	}
	alertCounts := severityCounter()
	for _, a := range alerts {
		alertCounts.add(a.Severity)
	}

	services := s.app.Health.All()
	serviceCounts := statusCounter()
	serviceOut := make(map[string]any, len(services))
	for name, ss := range services {
		serviceCounts.add(ss.Status)
		m := map[string]any{"status": ss.Status}
		if ss.Detail != "" {
			m["detail"] = ss.Detail
		}
		if ss.LastEventAt != nil {
			m["last_event_at"] = common.FormatTime(*ss.LastEventAt)
		}
		serviceOut[name] = m
	}

	overall := deriveOperationalStatus(alertCounts, serviceCounts, deviceCounts, cameraCounts, failedBatches)

	writeJSON(w, http.StatusOK, map[string]any{
		"site_id":      s.app.SiteID(),
		"edge_id":      s.app.EdgeID(),
		"version":      rtime.Version,
		"generated_at": common.FormatTime(common.NowUTC()),
		"overall": map[string]any{
			"status": overall,
			"basis":  "critical alerts, service health, device/camera status, and sync failures",
		},
		"tanks": map[string]any{
			"count": len(profiles),
		},
		"devices": map[string]any{
			"count":  len(devices),
			"status": deviceCounts,
		},
		"cameras": map[string]any{
			"count":  len(cameras),
			"status": cameraCounts,
		},
		"alerts": map[string]any{
			"open_count": len(alerts),
			"severity":   alertCounts,
		},
		"sync": map[string]any{
			"mode":                    s.cfg.Sync.Mode,
			"endpoint_configured":     s.cfg.Sync.Endpoint != nil,
			"unsynced_events":         unsynced,
			"pending_batches":         pendingBatches,
			"failed_batches":          failedBatches,
			"oldest_unsynced_age_sec": int(oldestAge.Seconds()),
		},
		"services": map[string]any{
			"overall": s.app.Health.OverallHealth(),
			"counts":  serviceCounts,
			"items":   serviceOut,
		},
	})
}

type countMap map[string]int

func statusCounter() countMap {
	return countMap{
		"ok":       0,
		"starting": 0,
		"disabled": 0,
		"online":   0,
		"degraded": 0,
		"down":     0,
		"failed":   0,
		"unknown":  0,
	}
}

func severityCounter() countMap {
	return countMap{
		"info":     0,
		"warning":  0,
		"critical": 0,
	}
}

func (c countMap) add(status string) {
	if status == "" {
		status = "unknown"
	}
	if _, ok := c[status]; !ok {
		c[status] = 0
	}
	c[status]++
}

func deriveOperationalStatus(alerts, services, devices, cameras countMap, failedBatches int) string {
	if alerts[events.SeverityCritical] > 0 || services["failed"] > 0 || devices[events.DeviceStatusDown] > 0 || cameras[events.DeviceStatusDown] > 0 {
		return "critical"
	}
	if alerts[events.SeverityWarning] > 0 || services["degraded"] > 0 || devices[events.DeviceStatusDegraded] > 0 || cameras[events.DeviceStatusDegraded] > 0 || failedBatches > 0 {
		return "degraded"
	}
	return "ok"
}
