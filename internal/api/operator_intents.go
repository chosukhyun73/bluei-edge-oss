package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/llm"
	"bluei.kr/edge/internal/storage"
)

// POST /v1/operator/intents
// GET  /v1/operator/intents?tank_id=...&limit=50
func (s *Server) handleOperatorIntentsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostOperatorIntent(w, r)
	case http.MethodGet:
		s.handleListOperatorIntents(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type operatorIntentRequest struct {
	TankID            string         `json:"tank_id"`
	RelatedCycleID    string         `json:"related_cycle_id"`
	RelatedDecisionID string         `json:"related_decision_id"`
	IntentType        string         `json:"intent_type"` // 'feed_now' | 'skip_cycle' | 'change_pattern' | 'general_note'
	Reason            string         `json:"reason"`
	Context           map[string]any `json:"context"`
}

func (s *Server) handlePostOperatorIntent(w http.ResponseWriter, r *http.Request) {
	var req operatorIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.IntentType == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_INTENT_TYPE", "intent_type is required", "")
		return
	}
	if req.Reason == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_REASON", "reason is required", "")
		return
	}

	ctxJSON := "{}"
	if req.Context != nil {
		if b, err := json.Marshal(req.Context); err == nil {
			ctxJSON = string(b)
		}
	}

	intent := &storage.OperatorIntent{
		IntentID:          common.NewID("intent"),
		TankID:            req.TankID,
		RelatedCycleID:    req.RelatedCycleID,
		RelatedDecisionID: req.RelatedDecisionID,
		IntentType:        req.IntentType,
		Reason:            req.Reason,
		ContextJSON:       ctxJSON,
		RecordedAt:        time.Now().UTC(),
	}

	if err := s.store.InsertOperatorIntent(r.Context(), intent); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// Phase 1e-A — AIScheduler 즉시 hook.
	// LLM enabled 시: apply endpoint 가 explicit trigger → 자동 rebuild 제거 (안전).
	// LLM disabled 시: 기존 즉시 rebuild 유지 (5분 polling 지연 해소).
	if s.aiScheduler != nil && req.TankID != "" && s.llmClient == nil {
		go func(tankID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.aiScheduler.RebuildScheduleForTank(ctx, tankID); err != nil {
				slog.Warn("ai_scheduler rebuild after operator_intent failed", "tank_id", tankID, "error", err)
			}
		}(req.TankID)
	}

	// Phase F.1 — LLM 종합 판단 hook (동기).
	// 운영자 reason + 컨텍스트 → LLM analysis. 응답에 포함하고 operator_intents.context_json
	// 에도 저장. LLM 실패 시 응답은 정상 (분석 없음).
	resp := map[string]any{"intent_id": intent.IntentID}
	if s.llmClient != nil && req.TankID != "" && req.Reason != "" {
		// API server global WriteTimeout(=request_timeout_sec) 가 짧아 LLM 호출 도중 connection 끊김.
		// 이 endpoint 만 per-response write deadline 해제 (camera_mjpeg 와 같은 패턴).
		if rc := http.NewResponseController(w); rc != nil {
			_ = rc.SetWriteDeadline(time.Time{})
		}
		// LLM 분석 timeout: primary(60s) + fallback(60s) 합산 가능 + 여유.
		// config 의 timeout_sec 보다 충분히 커야 fallback 까지 시도 가능.
		analysisCtx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		intentCtx := llm.GatherIntentContext(analysisCtx, s.store, req.TankID, req.Reason)
		prompt := llm.BuildOperatorIntentPrompt(intentCtx)

		analysis, err := s.llmClient.Analyze(analysisCtx, prompt)
		cancel()
		if err != nil {
			slog.Warn("llm analysis failed", "intent_id", intent.IntentID, "error", err)
		} else {
			// 백엔드 안전 검증 layer — LLM 단독 결정 금지.
			llm.ValidateAndEnforce(analysis)

			// operator_intents.context_json 에 분석 결과 저장 (best-effort).
			storeAnalysisToIntent(r.Context(), s.store, intent, analysis)

			// 응답에 포함.
			resp["llm_analysis"] = map[string]any{
				"can_apply":      analysis.CanApply,
				"reason":         analysis.Reason,
				"scope":          analysis.Scope,
				"blocked_by":     analysis.BlockedBy,
				"adjustment":     analysis.Adjustment,
				"explanation_ko": analysis.ExplanationKo,
				"confidence":     analysis.Confidence,
				"model_used":     analysis.ModelUsed,
				"fallback":       analysis.Fallback,
			}
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

// storeAnalysisToIntent — analysis 결과를 operator_intents.context_json 에 병합 저장.
// 기존 context (사용자 전달) 가 있으면 보존하고 "llm_analysis" 키 아래 추가.
// best-effort: 실패해도 응답 흐름은 막지 않는다.
func storeAnalysisToIntent(ctx context.Context, store storage.Store, intent *storage.OperatorIntent, a *llm.Analysis) {
	merged := map[string]any{}
	if intent.ContextJSON != "" {
		_ = json.Unmarshal([]byte(intent.ContextJSON), &merged)
	}
	merged["llm_analysis"] = map[string]any{
		"can_apply":      a.CanApply,
		"reason":         a.Reason,
		"scope":          a.Scope,
		"blocked_by":     a.BlockedBy,
		"adjustment":     a.Adjustment,
		"explanation_ko": a.ExplanationKo,
		"confidence":     a.Confidence,
		"model_used":     a.ModelUsed,
		"fallback":       a.Fallback,
	}
	b, err := json.Marshal(merged)
	if err != nil {
		slog.Warn("llm analysis json marshal failed", "intent_id", intent.IntentID, "error", err)
		return
	}
	// 새 ContextJSON 으로 UPDATE — storage 에 update API 가 없으면 best-effort 로 skip.
	if updater, ok := store.(operatorIntentContextUpdater); ok {
		if err := updater.UpdateOperatorIntentContext(ctx, intent.IntentID, string(b)); err != nil {
			slog.Warn("update operator_intent context failed", "intent_id", intent.IntentID, "error", err)
		}
	}
}

// operatorIntentContextUpdater — optional storage capability. sqliteStore 가 구현하면
// LLM 분석을 영구 저장. 없으면 응답만으로 노출.
type operatorIntentContextUpdater interface {
	UpdateOperatorIntentContext(ctx context.Context, intentID, contextJSON string) error
}

// handleOperatorIntentItemRoute — /v1/operator/intents/{id}/apply 와 같이
// intent_id 를 포함하는 서브 경로를 dispatch.
func (s *Server) handleOperatorIntentItemRoute(w http.ResponseWriter, r *http.Request) {
	// path: /v1/operator/intents/{id}/apply
	path := strings.TrimPrefix(r.URL.Path, "/v1/operator/intents/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 && parts[1] == "apply" && r.Method == http.MethodPost {
		s.handleApplyOperatorIntent(w, r, parts[0])
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// handleApplyOperatorIntent — POST /v1/operator/intents/{intent_id}/apply
// LLM analysis 의 adjustment 를 1회성 PolicyOverride 로 변환 → AIScheduler rebuild.
func (s *Server) handleApplyOperatorIntent(w http.ResponseWriter, r *http.Request, intentID string) {
	if intentID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "intent_id required", "")
		return
	}

	intent, err := s.store.GetOperatorIntent(r.Context(), intentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if intent == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "intent not found", "")
		return
	}

	// context_json 에서 llm_analysis 추출.
	var ctxMap map[string]any
	if err := json.Unmarshal([]byte(intent.ContextJSON), &ctxMap); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "BAD_STATE", "context_json parse failed", "")
		return
	}
	rawAnalysis, ok := ctxMap["llm_analysis"]
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "BAD_STATE", "llm_analysis not found in intent context", "")
		return
	}

	// map[string]any → re-marshal → unmarshal into typed struct for clarity.
	ab, err := json.Marshal(rawAnalysis)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "analysis re-marshal failed", "")
		return
	}
	var analysis llm.Analysis
	if err := json.Unmarshal(ab, &analysis); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "analysis unmarshal failed", "")
		return
	}

	// 운영자 강제 적용 옵션 — ?force=true 면 can_apply=false 라도 적용.
	// 운영 책임은 운영자에게 있음을 응답에 명시 (forced_by_operator=true).
	force := r.URL.Query().Get("force") == "true"
	if !analysis.CanApply && !force {
		writeError(w, http.StatusUnprocessableEntity, "BAD_STATE", "llm_analysis.can_apply is false — use ?force=true to override", "")
		return
	}

	// adjustment → PolicyOverride 변환. can_apply=false 이고 adjustment 비어있으면 nil.
	override := adjustmentToOverride(analysis.Adjustment)

	if s.aiScheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "SCHEDULER_UNAVAILABLE", "ai scheduler not running", "")
		return
	}

	applyCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.aiScheduler.RebuildScheduleForTankWithOverride(applyCtx, intent.TankID, override); err != nil {
		writeError(w, http.StatusInternalServerError, "SCHEDULER_ERROR", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"intent_id":          intentID,
		"applied_at":         time.Now().UTC().Format(time.RFC3339),
		"schedule_refreshed": true,
		"forced_by_operator": force,
	})
}

// adjustmentToOverride — LLM analysis.Adjustment map → PolicyOverride.
func adjustmentToOverride(adj map[string]any) *feed_cycle.PolicyOverride {
	if len(adj) == 0 {
		return nil
	}
	var out feed_cycle.PolicyOverride
	if v, ok := adj["max_daily_cycles_override"]; ok {
		switch n := v.(type) {
		case float64:
			i := int(n)
			out.MaxDailyCyclesOverride = &i
		}
	}
	if v, ok := adj["bsf_mode_override"]; ok {
		if s, ok := v.(string); ok && s != "" {
			out.BsfModeOverride = &s
		}
	}
	if v, ok := adj["get_factor"]; ok {
		if f, ok := v.(float64); ok && f > 0 {
			out.GetFactor = &f
		}
	}
	if v, ok := adj["min_interval_min"]; ok {
		switch n := v.(type) {
		case float64:
			i := int(n)
			out.MinIntervalMin = &i
		}
	}
	return &out
}

func (s *Server) handleListOperatorIntents(w http.ResponseWriter, r *http.Request) {
	tankID := r.URL.Query().Get("tank_id")
	limit := intParam(r, "limit", 50, 200)

	intents, err := s.store.ListOperatorIntents(r.Context(), tankID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if intents == nil {
		intents = []*storage.OperatorIntent{}
	}

	items := make([]map[string]any, len(intents))
	for i, intent := range intents {
		items[i] = operatorIntentView(intent)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func operatorIntentView(i *storage.OperatorIntent) map[string]any {
	return map[string]any{
		"intent_id":           i.IntentID,
		"operator_id":         i.OperatorID,
		"tank_id":             i.TankID,
		"related_cycle_id":    i.RelatedCycleID,
		"related_decision_id": i.RelatedDecisionID,
		"intent_type":         i.IntentType,
		"reason":              i.Reason,
		"recorded_at":         i.RecordedAt.Format(time.RFC3339),
	}
}
