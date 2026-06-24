package api

import (
	"net/http"
	"strings"
)

// handleTankLiveWeight — GET /v1/tanks/{tank_id}/live-weight.
// tank 의 feeder controller 를 찾아 UDP weight cache 반환.
// 응답: { tank_id, controller_id, grams, raw, mode, rssi, age_ms } 또는 404 (없음).
//
// dashboard 폴링용 (1Hz 권장). UDP push 주기와 일치.
func (s *Server) handleTankLiveWeight(w http.ResponseWriter, r *http.Request) {
	const suffix = "/live-weight"
	rel := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tanks/"), suffix)
	tankID := strings.Trim(rel, "/")
	if tankID == "" || strings.Contains(tankID, "/") {
		writeError(w, http.StatusBadRequest, "INVALID_PATH",
			"expected /v1/tanks/{tank_id}/live-weight", "")
		return
	}
	if s.liveWeight == nil {
		writeError(w, http.StatusServiceUnavailable, "LIVE_WEIGHT_UNAVAILABLE", "udp listener not wired", "")
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
	grams, rawGrams, raw, mode, rssi, ageMs, ok := s.liveWeight.GetLiveWeight(controllerID)
	if !ok {
		writeError(w, http.StatusNotFound, "NO_WEIGHT", "no UDP weight packet received for "+controllerID, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":       tankID,
		"controller_id": controllerID,
		"grams":         grams,
		"raw_grams":     rawGrams,
		"raw":           raw,
		"mode":          mode,
		"rssi":          rssi,
		"age_ms":        ageMs,
	})
}
