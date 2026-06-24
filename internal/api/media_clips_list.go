package api

import (
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

func (s *Server) handleRecentMediaClips(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 20, 100)
	tankID := r.URL.Query().Get("tank_id")
	cameraID := r.URL.Query().Get("camera_id")
	items, err := s.listMediaClips(r, tankID, cameraID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "tank_id": tankID, "camera_id": cameraID})
}

func (s *Server) listMediaClips(r *http.Request, tankID, cameraID string, limit int) ([]map[string]any, error) {
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventMediaClipStored, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.MediaClipStoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if tankID != "" && payload.TankID != tankID {
			continue
		}
		if cameraID != "" && payload.CameraID != cameraID {
			continue
		}
		out = append(out, map[string]any{"sequence": e.Sequence, "event_id": e.EventID, "recorded_at": common.FormatTime(e.RecordedAt), "payload": payload})
	}
	return out, nil
}
