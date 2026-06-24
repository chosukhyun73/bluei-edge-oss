package api

import (
	"net/http"
	"time"
)

func (s *Server) handleWaterQualityBuckets(w http.ResponseWriter, r *http.Request) {
	tankID := r.URL.Query().Get("tank_id")
	if tankID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_TANK_ID", "tank_id query parameter is required", "")
		return
	}
	limit := intParam(r, "limit", 120, 2000)
	var sincePtr, untilPtr *time.Time
	if since := r.URL.Query().Get("since"); since != "" {
		parsed, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_SINCE", "since must be RFC3339 timestamp", "")
			return
		}
		sincePtr = &parsed
	}
	if until := r.URL.Query().Get("until"); until != "" {
		parsed, err := time.Parse(time.RFC3339Nano, until)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_UNTIL", "until must be RFC3339 timestamp", "")
			return
		}
		untilPtr = &parsed
	}

	items, err := s.store.ListWaterQualityBuckets(r.Context(), tankID, sincePtr, untilPtr, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id": tankID,
		"items":   items,
		"count":   len(items),
	})
}

func (s *Server) handleFeedingImpactRoute(w http.ResponseWriter, r *http.Request) {
	feedingID := commandIDFromPathWithPrefix(r.URL.Path, "/v1/feedings/impact/")
	if feedingID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/feedings/impact/{feeding_id}", "")
		return
	}
	item, err := s.store.GetFeedingImpactAnalysis(r.Context(), feedingID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "feeding impact analysis not found", "")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func commandIDFromPathWithPrefix(path, prefix string) string {
	if len(path) <= len(prefix) || path[:len(prefix)] != prefix {
		return ""
	}
	return path[len(prefix):]
}
