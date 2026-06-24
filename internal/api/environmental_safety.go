package api

import (
	"encoding/json"
	"net/http"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

// handleGetEnvironmentalCurrent handles GET /v1/environmental/current?site_id=...
func (s *Server) handleGetEnvironmentalCurrent(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("site_id")
	if siteID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_SITE_ID", "site_id required", "")
		return
	}
	items, err := s.store.ListRecentEnvironmentalSnapshots(r.Context(), siteID, 1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if len(items) == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no environmental snapshot for site", "")
		return
	}
	writeJSON(w, http.StatusOK, items[0])
}

// handleGetEnvironmentalHistory handles GET /v1/environmental/history?site_id=...&limit=50
func (s *Server) handleGetEnvironmentalHistory(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("site_id")
	if siteID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_SITE_ID", "site_id required", "")
		return
	}
	limit := intParam(r, "limit", 50, 200)
	items, err := s.store.ListRecentEnvironmentalSnapshots(r.Context(), siteID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*storage.EnvironmentalSnapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// handlePostEnvironmentalSnapshot handles POST /v1/environmental/snapshot (manual injection).
// Body: { site_id, wind_speed_ms, wave_height_m, tide_phase, tide_minutes_to_low, temperature_c }
func (s *Server) handlePostEnvironmentalSnapshot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SiteID           string   `json:"site_id"`
		WindSpeedMS      *float64 `json:"wind_speed_ms"`
		WaveHeightM      *float64 `json:"wave_height_m"`
		TidePhase        string   `json:"tide_phase"`
		TideMinutesToLow *int     `json:"tide_minutes_to_low"`
		TemperatureC     *float64 `json:"temperature_c"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON", "")
		return
	}
	if body.SiteID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "site_id required", "")
		return
	}

	snap := &storage.EnvironmentalSnapshot{
		SnapshotID:       common.NewID("envsnap"),
		SiteID:           body.SiteID,
		WindSpeedMS:      body.WindSpeedMS,
		WaveHeightM:      body.WaveHeightM,
		TidePhase:        body.TidePhase,
		TideMinutesToLow: body.TideMinutesToLow,
		TemperatureC:     body.TemperatureC,
		RecordedAt:       time.Now().UTC(),
		Source:           "manual",
	}
	if err := s.store.InsertEnvironmentalSnapshot(r.Context(), snap); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, snap)
}
