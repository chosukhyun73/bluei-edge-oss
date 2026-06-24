package api

import (
	"net/http"
	"time"

	"bluei.kr/edge/internal/storage"
)

// handleListArbiterDecisions returns the most recent arbiter decisions.
//
// C-4 (본선 5/27 시연용): 5-G Progressive Autonomy 의 priority/decision 흐름을
// 운영자가 한눈에 보도록 한다. 표시 위치는 dashboard 의 "안전학습" sub-tab.
//
// GET /v1/arbiter/decisions?limit=50&tank_id=&priority=&since=
//   - limit:    1~200 (default 50)
//   - tank_id:  filter by tank
//   - priority: filter by priority label
//   - "manual_override" (운영자 수동)
//   - "ai_advisory"     (AI 권고)
//   - "ai_autonomous"   (AI 자율)
//   - since:    RFC3339 timestamp — client 가 폴링 incremental 용 (서버는 단순 필터)
//
// 응답: { items: [...], count }
//
// 분류 규칙 (frontend 와 통일):
//   - source=operator_manual / operator_schedule → manual_override
//   - source=ai_advisory                         → ai_advisory
//   - source=ai_autonomous                       → ai_autonomous
//
// preempted_cycle_id 가 있으면 응답에 echo — frontend conflict 강조용.
func (s *Server) handleListArbiterDecisions(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50, 200)
	tankID := r.URL.Query().Get("tank_id")
	priorityFilter := r.URL.Query().Get("priority")
	sinceStr := r.URL.Query().Get("since")

	var sinceFilter time.Time
	hasSince := false
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			sinceFilter = t
			hasSince = true
		}
	}

	// storage 측에서 tank_id, limit 만 1차 필터 — 나머지는 메모리에서 적용.
	// (decisions 테이블은 50~수백 행 수준이라 in-memory 필터 비용 무시 가능)
	rows, err := s.store.ListArbiterDecisions(r.Context(), tankID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		priority := arbiterPriorityLabel(d.Source)
		if priorityFilter != "" && priority != priorityFilter {
			continue
		}
		if hasSince && d.DecidedAt.Before(sinceFilter) {
			continue
		}
		items = append(items, arbiterDecisionView(d, priority))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

// arbiterPriorityLabel maps storage source string → dashboard priority enum.
func arbiterPriorityLabel(source string) string {
	switch source {
	case "operator_manual", "operator_schedule":
		return "manual_override"
	case "ai_advisory":
		return "ai_advisory"
	case "ai_autonomous":
		return "ai_autonomous"
	default:
		return source
	}
}

// arbiterDecisionView serializes ArbiterDecision for the dashboard.
// 응답 필드명은 frontend 의 ArbiterDecision 타입과 일치시킨다.
func arbiterDecisionView(d *storage.ArbiterDecision, priority string) map[string]any {
	out := map[string]any{
		"decision_id":        d.DecisionID,
		"tank_id":            d.TankID,
		"source":             d.Source,
		"priority":           priority,
		"accepted":           d.Accepted,
		"decision":           arbiterDecisionLabel(d.Accepted, d.PreemptedCycleID),
		"recorded_at":        d.DecidedAt.Format(time.RFC3339),
		"submitted_at":       d.SubmittedAt.Format(time.RFC3339),
		"resulting_cycle_id": d.ResultingCycleID,
		"existing_cycle_id":  d.ExistingCycleID,
		"preempted_cycle_id": d.PreemptedCycleID,
		"intent_id":          d.IntentID,
		"rejection_reason":   d.RejectionReason,
	}
	return out
}

// arbiterDecisionLabel returns the high-level decision verb the dashboard renders.
//   - accepted + preempted_cycle_id → "preempt"
//   - accepted                      → "accept"
//   - !accepted                     → "reject"
func arbiterDecisionLabel(accepted bool, preemptedCycleID string) string {
	if !accepted {
		return "reject"
	}
	if preemptedCycleID != "" {
		return "preempt"
	}
	return "accept"
}
