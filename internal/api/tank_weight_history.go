package api

import (
	"net/http"
	"strconv"
	"strings"
)

// weightHistoryTankID — /v1/tanks/{id}/weight-history 에서 tank_id 추출.
func weightHistoryTankID(path string) string {
	const suffix = "/weight-history"
	if !strings.HasSuffix(path, suffix) {
		return ""
	}
	trimmed := strings.TrimSuffix(path, suffix)
	if !strings.HasPrefix(trimmed, "/v1/tanks/") {
		return ""
	}
	id := strings.TrimPrefix(trimmed, "/v1/tanks/")
	id = strings.Trim(id, "/")
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

// handleTankWeightHistory — GET /v1/tanks/{id}/weight-history?days=30
func (s *Server) handleTankWeightHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tankID := weightHistoryTankID(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{tank_id}/weight-history", "")
		return
	}

	days := 30
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 365 {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_DAYS_RANGE",
				"days must be an integer between 1 and 365", "")
			return
		}
		days = n
	}

	snaps, err := s.store.ListTankWeightHistory(r.Context(), tankID, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	type snapshotJSON struct {
		SnapshotDate        string  `json:"snapshot_date"`
		EstimatedAvgWeightG float64 `json:"estimated_avg_weight_g"`
		AnchorSource        string  `json:"anchor_source"`
		ExpectedFCR         float64 `json:"expected_fcr"`
		FCRSource           string  `json:"fcr_source"`
		CumulativeFeedG     float64 `json:"cumulative_feed_g"`
		Quality             string  `json:"quality"`
	}

	out := make([]snapshotJSON, len(snaps))
	for i, s := range snaps {
		out[i] = snapshotJSON{
			SnapshotDate:        s.SnapshotDate,
			EstimatedAvgWeightG: s.EstimatedAvgWeightG,
			AnchorSource:        s.AnchorSource,
			ExpectedFCR:         s.ExpectedFCR,
			FCRSource:           s.FCRSource,
			CumulativeFeedG:     s.CumulativeFeedG,
			Quality:             s.Quality,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":   tankID,
		"days":      days,
		"snapshots": out,
		"count":     len(out),
	})
}
