package api

import (
	"net/http"

	"bluei.kr/edge/internal/predictive"
	"bluei.kr/edge/internal/storage"
)

// handleGetPredictiveForecast returns current NH3 headroom for a WTG (D-7 light).
// GET /v1/predictive/forecast?wtg_id=...
func (s *Server) handleGetPredictiveForecast(w http.ResponseWriter, r *http.Request) {
	wtgID := r.URL.Query().Get("wtg_id")
	if wtgID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_WTG_ID", "wtg_id query parameter is required", "")
		return
	}

	// Load WTGs for this site and find the requested one.
	groups, err := s.store.ListWTGs(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	var found bool
	for _, g := range groups {
		if g.WTGID != wtgID {
			continue
		}
		found = true

		headroom, err := predictive.ComputeHeadroom(r.Context(), s.store, g)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "COMPUTE_ERROR", err.Error(), "")
			return
		}

		status := "ok"
		cautionRatio := s.cfg.PredictiveSafety.NH3CautionRatio
		if cautionRatio == 0 {
			cautionRatio = 0.7
		}
		if headroom.MaxProcessingKgPerH > 0 {
			load := headroom.ActiveLoadKgPerH
			switch {
			case load >= headroom.MaxProcessingKgPerH:
				status = "breach"
			case load >= headroom.MaxProcessingKgPerH*cautionRatio:
				status = "caution"
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"wtg_id":               wtgID,
			"headroom_kg_per_h":    headroom.HeadroomKgPerH,
			"recent_load_kg_per_h": headroom.ActiveLoadKgPerH,
			"capacity_kg_per_h":    headroom.MaxProcessingKgPerH,
			"threshold":            headroom.MaxProcessingKgPerH * cautionRatio,
			"status":               status,
		})
		return
	}

	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "wtg_id not found", "")
	}
}

// handleGetPredictiveBlocks returns the most recent C-3p block audit rows.
// GET /v1/predictive/blocks?limit=50
func (s *Server) handleGetPredictiveBlocks(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50, 200)
	blocks, err := s.store.ListPredictiveBlocks(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if blocks == nil {
		blocks = []*storage.PredictiveBlock{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": blocks,
		"count": len(blocks),
	})
}
