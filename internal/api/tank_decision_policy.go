package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

// tankDecisionPolicyIDFromPath — /v1/tanks/{id}/decision-policy 에서 tank_id 추출.
func tankDecisionPolicyIDFromPath(path string) string {
	if !strings.HasPrefix(path, "/v1/tanks/") || !strings.HasSuffix(path, "/decision-policy") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/tanks/"), "/decision-policy")
	return strings.Trim(id, "/")
}

type tankDecisionPolicyRequest struct {
	AutoExecuteEnabled bool   `json:"auto_execute_enabled"`
	GraceMinutes       int    `json:"grace_minutes"`
	UpdatedBy          string `json:"updated_by"`
}

func (s *Server) handleTankDecisionPolicy(w http.ResponseWriter, r *http.Request) {
	tankID := tankDecisionPolicyIDFromPath(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{tank_id}/decision-policy", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getTankDecisionPolicy(w, r, tankID)
	case http.MethodPost:
		s.postTankDecisionPolicy(w, r, tankID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// getTankDecisionPolicy — Cage/Tank별 정책 반환.
// 행 없으면 시스템 기본값 + source="system_default".
func (s *Server) getTankDecisionPolicy(w http.ResponseWriter, r *http.Request, tankID string) {
	p, err := s.store.GetTankDecisionPolicy(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if p == nil {
		// 시스템 fallback
		writeJSON(w, http.StatusOK, map[string]any{
			"tank_id":              tankID,
			"auto_execute_enabled": s.cfg.DecisionPolicy.AutoExecuteEnabled,
			"grace_minutes":        effectiveGraceMinutes(s.cfg.DecisionPolicy.GraceMinutes),
			"source":               "system_default",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id":              p.TankID,
		"auto_execute_enabled": p.AutoExecuteEnabled,
		"grace_minutes":        p.GraceMinutes,
		"updated_at":           fmtAPITime(p.UpdatedAt),
		"updated_by":           p.UpdatedBy,
		"source":               "tank_override",
	})
}

// postTankDecisionPolicy — Cage/Tank별 정책 업데이트.
// 이전 값과 동일하면 changed=false, 다르면 upsert + changed=true.
func (s *Server) postTankDecisionPolicy(w http.ResponseWriter, r *http.Request, tankID string) {
	var req tankDecisionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	// grace_minutes 검증 — 0 은 "설정 안 함" 이 아니라 0 값이므로 거부.
	if req.GraceMinutes < 1 || req.GraceMinutes > 1440 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GRACE_MINUTES",
			"grace_minutes must be between 1 and 1440", "")
		return
	}

	if req.UpdatedBy == "" {
		req.UpdatedBy = "operator"
	}

	// 현재 값 조회 — 동일하면 쓰기 생략.
	prev, err := s.store.GetTankDecisionPolicy(r.Context(), tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if prev != nil &&
		prev.AutoExecuteEnabled == req.AutoExecuteEnabled &&
		prev.GraceMinutes == req.GraceMinutes {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"changed": false,
			"current": map[string]any{
				"tank_id":              prev.TankID,
				"auto_execute_enabled": prev.AutoExecuteEnabled,
				"grace_minutes":        prev.GraceMinutes,
				"updated_at":           fmtAPITime(prev.UpdatedAt),
				"updated_by":           prev.UpdatedBy,
				"source":               "tank_override",
			},
		})
		return
	}

	now := common.NowUTC()
	row := &storage.TankDecisionPolicy{
		TankID:             tankID,
		AutoExecuteEnabled: req.AutoExecuteEnabled,
		GraceMinutes:       req.GraceMinutes,
		UpdatedAt:          now,
		UpdatedBy:          req.UpdatedBy,
	}
	if err := s.store.UpsertTankDecisionPolicy(r.Context(), row); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"changed": true,
		"applied": map[string]any{
			"tank_id":              row.TankID,
			"auto_execute_enabled": row.AutoExecuteEnabled,
			"grace_minutes":        row.GraceMinutes,
			"updated_at":           fmtAPITime(row.UpdatedAt),
			"updated_by":           row.UpdatedBy,
			"source":               "tank_override",
		},
	})
}

// effectiveDecisionPolicy — Cage/Tank별 override 가 있으면 그걸, 없으면 시스템 기본값.
func (s *Server) effectiveDecisionPolicy(ctx context.Context, tankID string) (enabled bool, graceMinutes int) {
	p, err := s.store.GetTankDecisionPolicy(ctx, tankID)
	if err == nil && p != nil {
		return p.AutoExecuteEnabled, p.GraceMinutes
	}
	return s.cfg.DecisionPolicy.AutoExecuteEnabled, effectiveGraceMinutes(s.cfg.DecisionPolicy.GraceMinutes)
}

// EffectiveDecisionPolicy — public wrapper for external callers (e.g., baseline.DecisionTimer).
func (s *Server) EffectiveDecisionPolicy(ctx context.Context, tankID string) (bool, int) {
	return s.effectiveDecisionPolicy(ctx, tankID)
}

// effectiveGraceMinutes — grace_minutes=0 이면 시스템 기본값 10 으로 보정.
func effectiveGraceMinutes(n int) int {
	if n < 1 {
		return 10
	}
	return n
}
