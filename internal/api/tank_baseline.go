package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision"
)

// handleTankBaselineRoute serves /v1/tanks/{id}/baseline/{score|status}.
func (s *Server) handleTankBaselineRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/tanks/"), "/")
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[1] != "baseline" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{id}/baseline/{score|status}", "")
		return
	}
	tankID := parts[0]
	switch parts[2] {
	case "score":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleTankBaselineScore(w, r, tankID)
	case "status":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleTankBaselineStatus(w, r, tankID)
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{id}/baseline/{score|status}", "")
	}
}

// handleTankBaselineScore — 가장 최근 시점에 대해 baseline anomaly score 산출.
// 모델이 없으면 409, 실행 실패는 500. 성공 시 events 에 적재 + JSON 응답.
func (s *Server) handleTankBaselineScore(w http.ResponseWriter, r *http.Request, tankID string) {
	active, err := vision.ActiveTankBaseline(tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MANIFEST_ERROR", err.Error(), "")
		return
	}
	if active.ActiveWeightsPath == "" {
		writeError(w, http.StatusConflict, "NO_BASELINE_MODEL",
			"이 Cage/Tank에 학습된 baseline 모델이 없습니다. [AI 가르치기 → tank-baseline] 으로 먼저 학습하세요.", "")
		return
	}

	dbPath := s.cfg.Storage.SQLitePath
	if dbPath == "" {
		dbPath = "var/bluei-edge/edge.db"
	}
	scorer := baseline.NewScorer(dbPath)
	sr, err := scorer.Score(r.Context(), tankID, active.ActiveWeightsPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SCORE_FAILED", err.Error(), "")
		return
	}
	if sr.EvaluatedAt == "" {
		sr.EvaluatedAt = common.FormatTime(common.NowUTC())
	}

	payload := events.TankBaselineScoredPayload{
		TankID:       tankID,
		ModelDir:     active.ActiveWeightsPath,
		JobID:        active.ActiveJobID,
		AnomalyScore: sr.AnomalyScore,
		P95Threshold: sr.P95Threshold,
		P99Threshold: sr.P99Threshold,
		Verdict:      sr.Verdict,
		FeatureDiff:  sr.FeatureDiff,
		EvaluatedAt:  sr.EvaluatedAt,
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_SCORE", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "baseline", tankID,
		events.EventTankBaselineScored, tankID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	// verdict 변화 → 대시보드 알림 raise/update/close. 실패해도 score 자체는 유효.
	if alertErr := baseline.MaybeRaiseOrCloseAlert(r.Context(), s.app, s.store, payload); alertErr != nil {
		// 응답에 경고만 포함, 200 OK 는 유지
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"sequence":      seq,
			"score":         payload,
			"alert_warning": alertErr.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"sequence": seq,
		"score":    payload,
	})
}

// handleTankBaselineStatus — 활성 baseline 모델 메타 + 가장 최근 score 이벤트.
func (s *Server) handleTankBaselineStatus(w http.ResponseWriter, r *http.Request, tankID string) {
	active, err := vision.ActiveTankBaseline(tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MANIFEST_ERROR", err.Error(), "")
		return
	}
	resp := map[string]any{
		"tank_id":      tankID,
		"has_model":    active.ActiveWeightsPath != "",
		"active_state": active,
	}
	if last, ok := s.latestBaselineScore(r.Context(), tankID); ok {
		resp["latest_score"] = last
	}
	writeJSON(w, http.StatusOK, resp)
}

// latestBaselineScore — events 에서 이 Cage/Tank의 가장 최근 score 한 건 추출.
func (s *Server) latestBaselineScore(ctx context.Context, tankID string) (events.TankBaselineScoredPayload, bool) {
	es, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankBaselineScored,
		Limit:     200,
	})
	if err != nil {
		return events.TankBaselineScoredPayload{}, false
	}
	for _, e := range es {
		var p events.TankBaselineScoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID {
			return p, true
		}
	}
	return events.TankBaselineScoredPayload{}, false
}
