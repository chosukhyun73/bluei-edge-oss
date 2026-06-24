package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/learned_safety"
	"bluei.kr/edge/internal/storage"
)

// handlePostOperatorDispute handles POST /v1/operator/disputes.
// Body: { decision_id, tank_id, dispute_type, comment }
func (s *Server) handlePostOperatorDispute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DecisionID  string `json:"decision_id"`
		TankID      string `json:"tank_id"`
		DisputeType string `json:"dispute_type"`
		Comment     string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON", "")
		return
	}
	if body.DecisionID == "" || body.TankID == "" || body.DisputeType == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "decision_id, tank_id, dispute_type required", "")
		return
	}

	now := time.Now().UTC()
	d := &storage.OperatorDispute{
		DisputeID:   common.NewID("disp"),
		DecisionID:  body.DecisionID,
		TankID:      body.TankID,
		DisputeType: body.DisputeType,
		Comment:     body.Comment,
		DisputedAt:  now,
		CreatedAt:   now,
	}
	if err := s.store.InsertOperatorDispute(r.Context(), d); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// Phase 1e-A — AIScheduler 즉시 hook.
	// 운영자 이의제기 = AI 결정 반박 → schedule 즉시 재계산 (5분 polling 지연 해소).
	if s.aiScheduler != nil && body.TankID != "" {
		go func(tankID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.aiScheduler.RebuildScheduleForTank(ctx, tankID); err != nil {
				slog.Warn("ai_scheduler rebuild after operator_dispute failed", "tank_id", tankID, "error", err)
			}
		}(body.TankID)
	}

	writeJSON(w, http.StatusCreated, d)
}

// handleListOperatorDisputes handles GET /v1/operator/disputes?limit=50.
func (s *Server) handleListOperatorDisputes(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50, 200)
	items, err := s.store.ListOperatorDisputes(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*storage.OperatorDispute{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// handleListLearnedRules handles GET /v1/learned-rules.
func (s *Server) handleListLearnedRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListLearnedRules(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = []*storage.LearnedRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// handleLearnedRulesRoute routes /v1/learned-rules/{id}/disable and /v1/learned-rules/mine.
func (s *Server) handleLearnedRulesRoute(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// POST /v1/learned-rules/mine
	if strings.HasSuffix(path, "/mine") && r.Method == http.MethodPost {
		s.handleMineLearnedRules(w, r)
		return
	}

	// POST /v1/learned-rules/{id}/disable
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/disable") {
		// extract {id}: path = /v1/learned-rules/{id}/disable
		trimmed := strings.TrimPrefix(path, "/v1/learned-rules/")
		ruleID := strings.TrimSuffix(trimmed, "/disable")
		if ruleID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "rule_id required", "")
			return
		}
		if err := s.store.SetLearnedRuleEnabled(r.Context(), ruleID, false); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rule_id": ruleID, "enabled": false})
		return
	}

	// POST /v1/learned-rules/{id}/enable — C-3l dashboard toggle 지원
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/enable") {
		trimmed := strings.TrimPrefix(path, "/v1/learned-rules/")
		ruleID := strings.TrimSuffix(trimmed, "/enable")
		if ruleID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "rule_id required", "")
			return
		}
		if err := s.store.SetLearnedRuleEnabled(r.Context(), ruleID, true); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rule_id": ruleID, "enabled": true})
		return
	}

	http.NotFound(w, r)
}

// handleMineLearnedRules runs MineFromDisputes on recent disputes and persists new rules.
// POST /v1/learned-rules/mine
//
// 2026-05-20: 같은 condition_json 의 규칙이 이미 (enabled 또는 disabled) 존재하면 skip.
// 이전: 호출할 때마다 같은 패턴이 N개 중복 생성됨 (운영자가 mining 2번 누르면 즉시 중복).
// 운영자가 명시적으로 disable 한 규칙도 skip — 운영자 의도 존중.
func (s *Server) handleMineLearnedRules(w http.ResponseWriter, r *http.Request) {
	disputes, err := s.store.ListOperatorDisputes(r.Context(), 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	// 기존 규칙 condition_json 집합 (enabled + disabled 모두).
	existing, err := s.store.ListLearnedRules(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	existingCond := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingCond[e.ConditionJSON] = struct{}{}
	}

	newRules := learned_safety.MineFromDisputes(disputes)
	var inserted, skipped int
	for _, rule := range newRules {
		if _, dup := existingCond[rule.ConditionJSON]; dup {
			skipped++
			continue
		}
		if err := s.store.InsertLearnedRule(r.Context(), rule); err != nil {
			continue
		}
		existingCond[rule.ConditionJSON] = struct{}{} // 같은 mining 호출 안에서도 중복 방지
		inserted++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"disputes_checked": len(disputes),
		"rules_mined":      len(newRules),
		"rules_inserted":   inserted,
		"rules_skipped":    skipped,
	})
}
