package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/biomass"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// ── 라우트 디스패치 ──────────────────────────────────────────────────────────

// handleTankSamplingRoute — /sampling 디스패치 (POST + GET).
func (s *Server) handleTankSamplingRoute(w http.ResponseWriter, r *http.Request) {
	tankID := samplingTankID(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/tanks/{tank_id}/sampling", "")
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handlePostSampling(w, r, tankID)
	case http.MethodGet:
		s.handleGetSampling(w, r, tankID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func samplingTankID(path string) string {
	if !strings.HasSuffix(path, "/sampling") {
		return ""
	}
	trimmed := strings.TrimSuffix(path, "/sampling")
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

// ── POST /v1/tanks/{id}/sampling ─────────────────────────────────────────────

type tankSamplingRequest struct {
	SampledCount  int     `json:"sampled_count"`
	AvgWeightG    float64 `json:"avg_weight_g"`
	StdWeightG    float64 `json:"std_weight_g"`
	MinWeightG    float64 `json:"min_weight_g"`
	MaxWeightG    float64 `json:"max_weight_g"`
	HealthScore   int     `json:"health_score"`
	HealthNotes   string  `json:"health_notes"`
	AbnormalCount int     `json:"abnormal_count"`
	SampledAt     string  `json:"sampled_at"`
	RecordedBy    string  `json:"recorded_by"`
}

func (s *Server) handlePostSampling(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankSamplingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	// 필수 필드
	if req.SampledCount <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SAMPLED_COUNT",
			"sampled_count must be > 0", "")
		return
	}
	if req.AvgWeightG <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_AVG_WEIGHT",
			"avg_weight_g must be > 0", "")
		return
	}

	// 기본값
	if req.RecordedBy == "" {
		req.RecordedBy = "operator"
	}
	if req.SampledAt == "" {
		req.SampledAt = common.NowUTC().Format(time.RFC3339Nano)
	}

	// HealthScore sanity (0은 "미지정"이므로 통과, 1~10 사이여야 의미 있음)
	if req.HealthScore < 0 || req.HealthScore > 10 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_HEALTH_SCORE",
			"health_score must be 0~10", "")
		return
	}

	// 체중 범위 sanity
	if req.MinWeightG > 0 && req.MinWeightG > req.AvgWeightG {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_WEIGHT_RANGE",
			"min_weight_g must be <= avg_weight_g", "")
		return
	}
	if req.MaxWeightG > 0 && req.MaxWeightG < req.AvgWeightG {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_WEIGHT_RANGE",
			"max_weight_g must be >= avg_weight_g", "")
		return
	}

	// 활성 lineage 권장 (필수 아님) — stocking_id 자동 채움
	var stockingID string
	var warnings []string
	lc, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err == nil && lc != nil && lc.Status == "active" {
		stockingID = lc.ActiveStockingID
	} else {
		// 출하 후/입식 전 상태 — sampling 자체는 거부하지 않음
		warnings = append(warnings, "활성 lineage 없음 — stocking_id 미연결 (입식 전 마지막 sampling 허용)")
	}

	samplingID := common.NewID("sampling")
	now := common.NowUTC()

	payload := events.TankSamplingRecordedPayload{
		SamplingID:    samplingID,
		TankID:        tankID,
		StockingID:    stockingID,
		SampledCount:  req.SampledCount,
		AvgWeightG:    req.AvgWeightG,
		StdWeightG:    req.StdWeightG,
		MinWeightG:    req.MinWeightG,
		MaxWeightG:    req.MaxWeightG,
		HealthScore:   req.HealthScore,
		HealthNotes:   req.HealthNotes,
		AbnormalCount: req.AbnormalCount,
		SampledAt:     req.SampledAt,
		RecordedBy:    req.RecordedBy,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_PAYLOAD", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "sampling", tankID,
		events.EventTankSamplingRecorded, samplingID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	// projection upsert
	sampledAt, _ := time.Parse(time.RFC3339Nano, req.SampledAt)
	proj := &storage.TankSampling{
		TankID:           tankID,
		LatestSamplingID: samplingID,
		StockingID:       stockingID,
		SampledCount:     req.SampledCount,
		AvgWeightG:       req.AvgWeightG,
		HealthNotes:      req.HealthNotes,
		SampledAt:        sampledAt,
		RecordedBy:       req.RecordedBy,
		UpdatedAt:        now,
	}
	if req.StdWeightG > 0 {
		v := req.StdWeightG
		proj.StdWeightG = &v
	}
	if req.MinWeightG > 0 {
		v := req.MinWeightG
		proj.MinWeightG = &v
	}
	if req.MaxWeightG > 0 {
		v := req.MaxWeightG
		proj.MaxWeightG = &v
	}
	if req.HealthScore != 0 {
		v := req.HealthScore
		proj.HealthScore = &v
	}
	if req.AbnormalCount > 0 {
		v := req.AbnormalCount
		proj.AbnormalCount = &v
	}
	if err := s.store.UpsertTankSampling(r.Context(), proj); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 자율 모드 변경 X — 일상 운영 작업

	// D-4: sampling 시점 자동 FCR 보정 (best-effort — 실패해도 sampling 응답 거부 X)
	calResult, calErr := biomass.CalibrateFromSampling(r.Context(), s.store, tankID, samplingID)
	if calErr != nil {
		slog.Warn("fcr calibration error", "tank_id", tankID, "error", calErr)
	}
	var fcrCalibrationResp map[string]any
	if calResult.Performed {
		cal := &storage.TankFCRCalibration{
			TankID:          tankID,
			StockingID:      stockingID,
			SamplingID:      samplingID,
			DefaultFCR:      calResult.DefaultFCR,
			ObservedFCR:     calResult.ObservedFCR,
			CalibratedFCR:   calResult.CalibratedFCR,
			DeviationPct:    calResult.DeviationPct,
			CumulativeFeedG: calResult.CumulativeFeedG,
			DeltaBiomassG:   calResult.DeltaBiomassG,
			CalibratedAt:    now,
		}
		if err := s.store.UpsertTankFCRCalibration(r.Context(), cal); err == nil {
			// audit event 적재
			auditPayload := events.TankFCRCalibratedPayload{
				TankID:          tankID,
				StockingID:      stockingID,
				SamplingID:      samplingID,
				DefaultFCR:      cal.DefaultFCR,
				ObservedFCR:     cal.ObservedFCR,
				CalibratedFCR:   cal.CalibratedFCR,
				DeviationPct:    cal.DeviationPct,
				CumulativeFeedG: cal.CumulativeFeedG,
				DeltaBiomassG:   cal.DeltaBiomassG,
				CalibratedAt:    now.Format(time.RFC3339Nano),
			}
			if err := auditPayload.Validate(); err == nil {
				_, _ = s.app.AppendEvent(r.Context(), "api", "fcr_calibration", tankID,
					events.EventTankFCRCalibrated, common.NewID("calibration"), auditPayload)
			}
		} else {
			slog.Warn("fcr calibration upsert failed", "tank_id", tankID, "error", err)
		}
		fcrCalibrationResp = map[string]any{
			"performed":      true,
			"default_fcr":    calResult.DefaultFCR,
			"observed_fcr":   calResult.ObservedFCR,
			"calibrated_fcr": calResult.CalibratedFCR,
			"deviation_pct":  calResult.DeviationPct,
		}
	} else {
		fcrCalibrationResp = map[string]any{
			"performed": false,
			"reason":    calResult.Reason,
		}
	}

	resp := map[string]any{
		"ok":            true,
		"sequence":      seq,
		"sampling_id":   samplingID,
		"tank_id":       tankID,
		"stocking_id":   stockingID,
		"sampled_count": req.SampledCount,
		"avg_weight_g":  req.AvgWeightG,
		"sampled_at":    req.SampledAt,
		"recorded_by":   req.RecordedBy,
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	resp["fcr_calibration"] = fcrCalibrationResp
	writeJSON(w, http.StatusOK, resp)
}

// ── GET /v1/tanks/{id}/sampling ───────────────────────────────────────────────

func (s *Server) handleGetSampling(w http.ResponseWriter, r *http.Request, tankID string) {
	// 최신 projection
	current, err := s.store.GetTankSampling(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// events history (최근 50건)
	evts, _ := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankSamplingRecorded,
		Limit:     50,
	})

	type histItem struct {
		SamplingID   string  `json:"sampling_id"`
		SampledAt    string  `json:"sampled_at"`
		AvgWeightG   float64 `json:"avg_weight_g"`
		SampledCount int     `json:"sampled_count"`
		HealthScore  int     `json:"health_score,omitempty"`
		StockingID   string  `json:"stocking_id,omitempty"`
		RecordedBy   string  `json:"recorded_by"`
	}
	var history []histItem
	for _, e := range evts {
		var p events.TankSamplingRecordedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID {
			continue
		}
		history = append(history, histItem{
			SamplingID:   p.SamplingID,
			SampledAt:    p.SampledAt,
			AvgWeightG:   p.AvgWeightG,
			SampledCount: p.SampledCount,
			HealthScore:  p.HealthScore,
			StockingID:   p.StockingID,
			RecordedBy:   p.RecordedBy,
		})
	}

	// sampled_at 기준 최신 우선 정렬
	sort.Slice(history, func(i, j int) bool {
		return history[i].SampledAt > history[j].SampledAt
	})
	if history == nil {
		history = []histItem{}
	}

	var currentMap any = nil
	if current != nil {
		currentMap = samplingToMap(current)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id": tankID,
		"current": currentMap,
		"history": history,
	})
}

func samplingToMap(ts *storage.TankSampling) map[string]any {
	m := map[string]any{
		"tank_id":            ts.TankID,
		"latest_sampling_id": ts.LatestSamplingID,
		"stocking_id":        ts.StockingID,
		"sampled_count":      ts.SampledCount,
		"avg_weight_g":       ts.AvgWeightG,
		"health_notes":       ts.HealthNotes,
		"sampled_at":         fmtAPITime(ts.SampledAt),
		"recorded_by":        ts.RecordedBy,
		"updated_at":         fmtAPITime(ts.UpdatedAt),
	}
	if ts.StdWeightG != nil {
		m["std_weight_g"] = *ts.StdWeightG
	}
	if ts.MinWeightG != nil {
		m["min_weight_g"] = *ts.MinWeightG
	}
	if ts.MaxWeightG != nil {
		m["max_weight_g"] = *ts.MaxWeightG
	}
	if ts.HealthScore != nil {
		m["health_score"] = *ts.HealthScore
	}
	if ts.AbnormalCount != nil {
		m["abnormal_count"] = *ts.AbnormalCount
	}
	return m
}

// latestSamplingForTank — state vector 등에서 사용하는 내부 helper.
func (s *Server) latestSamplingForTank(ctx context.Context, tankID string) (*storage.TankSampling, error) {
	return s.store.GetTankSampling(ctx, tankID)
}
