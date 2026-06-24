package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"bluei.kr/edge/internal/media"
	"bluei.kr/edge/internal/storage"
)

func (s *Server) handleCameraStatuses(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.store.ListCameraStatuses(r.Context(), r.URL.Query().Get("tank_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":             len(statuses),
		"items":             statuses,
		"runtime_readiness": cameraRuntimeReadiness(),
	})
}

func cameraRuntimeReadiness() media.RuntimeReadiness {
	return media.CheckRuntime(media.ExecRuntimeProbe{})
}

func (s *Server) updateCameraStatus(cameraID, tankID, status string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	detailsJSON, _ := json.Marshal(details)
	_ = s.store.UpsertCameraStatus(context.Background(), &storage.CurrentCameraStatus{
		CameraID:       cameraID,
		TankID:         tankID,
		Status:         status,
		IngestFPS:      numberFromDetails(details, "target_fps"),
		LastFrameAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ReconnectCount: int(numberFromDetails(details, "reconnect_count")),
		DroppedFrames:  int(numberFromDetails(details, "dropped_frames")),
		Details:        details,
	}, string(detailsJSON))
}

func numberFromDetails(details map[string]any, key string) float64 {
	v, ok := details[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
