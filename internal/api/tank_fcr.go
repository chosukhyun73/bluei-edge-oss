package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/biomass"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// handleTankFCRRoute — /v1/tanks/{id}/fcr 디스패치 (POST 직접입력 + GET 조회).
// 운영자가 FCR 수치를 직접 지정 → calibration override 로 저장. 추정 평균중량 계산이
// 이 값을 사용한다(projector OverrideFCR). sampling 자동 역산과 달리 실측 체중 불필요.
func (s *Server) handleTankFCRRoute(w http.ResponseWriter, r *http.Request) {
	tankID := fcrTankID(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/fcr", "")
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handlePostFCR(w, r, tankID)
	case http.MethodGet:
		s.handleGetFCR(w, r, tankID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func fcrTankID(path string) string {
	if !strings.HasSuffix(path, "/fcr") {
		return ""
	}
	trimmed := strings.TrimSuffix(path, "/fcr")
	if !strings.HasPrefix(trimmed, "/v1/tanks/") {
		return ""
	}
	id := strings.Trim(strings.TrimPrefix(trimmed, "/v1/tanks/"), "/")
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

type tankFCRRequest struct {
	FCR        float64 `json:"fcr"`
	Note       string  `json:"note"`
	RecordedBy string  `json:"recorded_by"`
}

func (s *Server) handlePostFCR(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankFCRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.FCR < biomass.MinReasonableFCR || req.FCR > biomass.MaxReasonableFCR {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_FCR",
			fmt.Sprintf("fcr must be within [%.1f, %.1f]", biomass.MinReasonableFCR, biomass.MaxReasonableFCR), "")
		return
	}
	if req.RecordedBy == "" {
		req.RecordedBy = "operator"
	}

	// active lifecycle 필수 — override 는 같은 stocking cycle 에만 유효(projector 조건).
	lc, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if lc == nil || lc.Status != "active" {
		writeError(w, http.StatusUnprocessableEntity, "NO_ACTIVE_LIFECYCLE",
			"활성 입식(lineage)이 없어 FCR 지정 불가", "")
		return
	}

	defaultFCR, _ := biomass.LookupFCR(lc.Species, lc.GrowthStage)
	var deviationPct float64
	if defaultFCR > 0 {
		deviationPct = (req.FCR - defaultFCR) / defaultFCR * 100.0
	}
	now := common.NowUTC()

	cal := &storage.TankFCRCalibration{
		TankID:          tankID,
		StockingID:      lc.ActiveStockingID,
		SamplingID:      "manual", // 운영자 직접 입력 표식 (sampling 역산 아님)
		DefaultFCR:      defaultFCR,
		ObservedFCR:     req.FCR, // 수동 지정 = observed 로도 기록
		CalibratedFCR:   req.FCR,
		DeviationPct:    deviationPct,
		CumulativeFeedG: 0,
		DeltaBiomassG:   0,
		CalibratedAt:    now,
	}
	if err := s.store.UpsertTankFCRCalibration(r.Context(), cal); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 추정 평균중량 스냅샷 즉시 재계산 — 운영자 FCR 조정은 다음 worker(매시간)까지
	// 기다리지 않고 급이 계산에 바로 반영되어야 함 (2026-07-18). best-effort.
	if _, err := biomass.SnapshotForTank(r.Context(), s.store, tankID, now); err != nil {
		slog.Warn("fcr manual: 스냅샷 즉시 갱신 실패", "tank_id", tankID, "error", err)
	}

	// audit event (best-effort)
	auditPayload := events.TankFCRCalibratedPayload{
		TankID:        tankID,
		StockingID:    lc.ActiveStockingID,
		SamplingID:    "manual",
		DefaultFCR:    defaultFCR,
		ObservedFCR:   req.FCR,
		CalibratedFCR: req.FCR,
		DeviationPct:  deviationPct,
		CalibratedAt:  now.Format(time.RFC3339Nano),
		Notes:         strings.TrimSpace("운영자 직접 입력 (" + req.RecordedBy + ") " + req.Note),
	}
	if err := auditPayload.Validate(); err == nil {
		if _, err := s.app.AppendEvent(r.Context(), "api", "fcr_manual", tankID,
			events.EventTankFCRCalibrated, common.NewID("calibration"), auditPayload); err != nil {
			slog.Warn("fcr manual audit append failed", "tank_id", tankID, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"tank_id":        tankID,
		"stocking_id":    lc.ActiveStockingID,
		"calibrated_fcr": req.FCR,
		"default_fcr":    defaultFCR,
		"deviation_pct":  deviationPct,
		"source":         "manual",
		"recorded_by":    req.RecordedBy,
	})
}

func (s *Server) handleGetFCR(w http.ResponseWriter, r *http.Request, tankID string) {
	lc, _ := s.store.GetTankLifecycle(r.Context(), tankID)
	var species, stage, stockingID string
	if lc != nil {
		species, stage, stockingID = lc.Species, lc.GrowthStage, lc.ActiveStockingID
	}
	defaultFCR, known := biomass.LookupFCR(species, stage)

	resp := map[string]any{
		"tank_id":     tankID,
		"default_fcr": defaultFCR,
		"fcr_known":   known,
	}
	cal, err := s.store.GetTankFCRCalibration(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if cal != nil && cal.StockingID == stockingID && stockingID != "" {
		// 같은 cycle 의 override 만 유효 (projector 와 동일 조건).
		resp["calibrated_fcr"] = cal.CalibratedFCR
		resp["observed_fcr"] = cal.ObservedFCR
		resp["deviation_pct"] = cal.DeviationPct
		resp["source"] = fcrSource(cal.SamplingID)
		resp["calibrated_at"] = fmtAPITime(cal.CalibratedAt)
	} else {
		resp["source"] = "default"
	}
	writeJSON(w, http.StatusOK, resp)
}

// fcrSource — sampling_id 로 override 출처 구분.
func fcrSource(samplingID string) string {
	if samplingID == "manual" {
		return "manual"
	}
	return "sampling"
}
