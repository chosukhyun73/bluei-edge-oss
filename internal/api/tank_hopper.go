package api

// 호퍼 잔량 원장 API (2026-07-17 사료 소진 사태 재발 방지)
//   POST /v1/tanks/{tank_id}/hopper/refill  {"amount_g": 5000, "note": "..."} — 보충 기록
//   GET  /v1/tanks/{tank_id}/hopper                                          — 잔량 추정

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/feed_cycle"
)

const hopperRefillMaxG = 50000 // 50kg — 오입력 방지 상한

func (s *Server) handleTankHopperRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/v1/tanks/")
	if idx := strings.Index(rel, "/hopper"); idx >= 0 {
		rel = rel[:idx]
	}
	tankID := strings.Trim(rel, "/")
	if tankID == "" || strings.Contains(tankID, "/") {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "tank_id is required", "")
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/hopper/refill") && r.Method == http.MethodPost:
		s.handleHopperRefill(w, r, tankID)
	case strings.HasSuffix(r.URL.Path, "/hopper/recalibrate") && r.Method == http.MethodPost:
		s.handleHopperRecalibrate(w, r, tankID)
	case strings.HasSuffix(r.URL.Path, "/hopper") && r.Method == http.MethodGet:
		s.handleHopperEstimate(w, r, tankID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHopperRefill(w http.ResponseWriter, r *http.Request, tankID string) {
	var req struct {
		AmountG float64 `json:"amount_g"`
		Note    string  `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.AmountG <= 0 || req.AmountG > hopperRefillMaxG {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_AMOUNT",
			"amount_g must be in (0, 50000]", "")
		return
	}
	payload := map[string]any{
		"tank_id":     tankID,
		"amount_g":    req.AmountG,
		"note":        req.Note,
		"refilled_at": common.FormatTime(common.NowUTC()),
	}
	if _, err := s.app.AppendEvent(r.Context(), "api", "hopper", tankID,
		events.EventFeederHopperRefilled, "", payload); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	est, err := feed_cycle.EstimateHopper(r.Context(), s.store, tankID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hopper": est})
}

// handleHopperRecalibrate — 운영자 실측으로 원장 잔량을 확정한다.
// refill 은 누적 가산이라 "실제로는 5kg 인데 원장이 8kg" 같은 개루프 드리프트를 못 고친다
// (2026-07-28 실사례). 재보정은 그 시점 잔량을 기준선으로 재설정하고, 이전 기록은
// 삭제하지 않고 계산에서만 제외한다 — 이벤트 원장은 append-only 를 유지한다.
func (s *Server) handleHopperRecalibrate(w http.ResponseWriter, r *http.Request, tankID string) {
	var req struct {
		RemainingG *float64 `json:"remaining_g"`
		Note       string   `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.RemainingG == nil || *req.RemainingG < 0 || *req.RemainingG > hopperRefillMaxG {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_AMOUNT",
			"remaining_g must be in [0, 50000]", "")
		return
	}
	payload := map[string]any{
		"tank_id":         tankID,
		"remaining_g":     *req.RemainingG,
		"note":            req.Note,
		"recalibrated_at": common.FormatTime(common.NowUTC()),
	}
	if _, err := s.app.AppendEvent(r.Context(), "api", "hopper", tankID,
		events.EventFeederHopperRecalibrated, "", payload); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	est, err := feed_cycle.EstimateHopper(r.Context(), s.store, tankID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "hopper": est})
}

func (s *Server) handleHopperEstimate(w http.ResponseWriter, r *http.Request, tankID string) {
	est, err := feed_cycle.EstimateHopper(r.Context(), s.store, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, est)
}
