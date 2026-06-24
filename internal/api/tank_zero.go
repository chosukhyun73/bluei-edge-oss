package api

import (
	"net/http"
	"strings"
)

// handleTankZero — POST /v1/tanks/{tank_id}/zero (set) / DELETE (clear).
// 빈 통 상태에서 운영자가 누르는 영점 버튼. 현재 안정값을 0 으로 정렬.
// piecewise cal 의 0점 raw drift 를 software offset 으로 보정.
func (s *Server) handleTankZero(w http.ResponseWriter, r *http.Request) {
	const suffix = "/zero"
	rel := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tanks/"), suffix)
	tankID := strings.Trim(rel, "/")
	if tankID == "" || strings.Contains(tankID, "/") {
		writeError(w, http.StatusBadRequest, "INVALID_PATH",
			"expected /v1/tanks/{tank_id}/zero", "")
		return
	}
	if s.liveWeight == nil {
		writeError(w, http.StatusServiceUnavailable, "LIVE_WEIGHT_UNAVAILABLE", "udp listener not wired", "")
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controllers, err := s.store.ListControllers(r.Context(), "active")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	var controllerID string
	for _, c := range controllers {
		if c.TankID == tankID {
			controllerID = c.ControllerID
			break
		}
	}
	if controllerID == "" {
		writeError(w, http.StatusNotFound, "NO_CONTROLLER", "no controller assigned to tank "+tankID, "")
		return
	}
	switch r.Method {
	case http.MethodPost:
		offset, ok := s.liveWeight.SetZero(controllerID)
		if !ok {
			writeError(w, http.StatusNotFound, "NO_WEIGHT", "no UDP weight packet received yet — wait a moment", "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tank_id":       tankID,
			"controller_id": controllerID,
			"zero_offset_g": offset,
			"applied":       true,
		})
	case http.MethodDelete:
		s.liveWeight.ClearZero(controllerID)
		writeJSON(w, http.StatusOK, map[string]any{
			"tank_id":       tankID,
			"controller_id": controllerID,
			"applied":       false,
		})
	}
}
