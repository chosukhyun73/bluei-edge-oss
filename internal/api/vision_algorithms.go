package api

import (
	"net/http"

	"bluei.kr/edge/internal/config"
)

const defaultVisionAlgorithmsPath = "configs/vision-algorithms.example.yaml"

func (s *Server) handleVisionAlgorithms(w http.ResponseWriter, r *http.Request) {
	vc, err := config.LoadVisionAlgorithms(defaultVisionAlgorithmsPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VISION_CONFIG_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"algorithm_library_version": vc.AlgorithmLibraryVersion,
		"items":                     vc.Algorithms,
		"count":                     len(vc.Algorithms),
	})
}

func (s *Server) handleVisionTankApplications(w http.ResponseWriter, r *http.Request) {
	vc, err := config.LoadVisionAlgorithms(defaultVisionAlgorithmsPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "VISION_CONFIG_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": vc.TankApplications,
		"count": len(vc.TankApplications),
	})
}
