package api

import (
	"net/http"
	"strings"

	"bluei.kr/edge/internal/biomass"
)

// weightProjectionTankID — /v1/tanks/{id}/weight-projection 에서 tank_id 추출.
func weightProjectionTankID(path string) string {
	const suffix = "/weight-projection"
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

// handleTankWeightProjection — GET /v1/tanks/{id}/weight-projection.
// 활성 lifecycle 없으면 200 + ok=false + quality="no_lifecycle".
func (s *Server) handleTankWeightProjection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tankID := weightProjectionTankID(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{tank_id}/weight-projection", "")
		return
	}

	proj, ok, err := biomass.LoadAndProject(r.Context(), s.store, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	if !ok {
		// no_lifecycle — 200 + ok=false
		writeJSON(w, http.StatusOK, map[string]any{
			"tank_id": tankID,
			"ok":      false,
			"quality": proj.Quality,
			"notes":   proj.Notes,
		})
		return
	}

	// lifecycle 에서 species/growth_stage/count 메타 조회
	lc, _ := s.store.GetTankLifecycle(r.Context(), tankID)
	var species, growthStage string
	var currentCount int
	if lc != nil {
		species = lc.Species
		growthStage = lc.GrowthStage
		currentCount = lc.InitialCount // mortality 모델 없음 — N₀ 고수
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":       tankID,
		"ok":            true,
		"species":       species,
		"growth_stage":  growthStage,
		"current_count": currentCount,
		"projection":    proj,
	})
}
