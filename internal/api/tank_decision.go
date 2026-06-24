package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// handleTankDecisionRoute — /v1/tanks/{id}/decisions/* 디스패치.
func (s *Server) handleTankDecisionRoute(w http.ResponseWriter, r *http.Request) {
	tankID := tankIDFromDecisionPath(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{tank_id}/decisions/...", "")
		return
	}

	// /decisions/proposed
	if strings.HasSuffix(r.URL.Path, "/decisions/proposed") {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleProposeDecision(w, r, tankID)
		return
	}

	// /decisions/pending
	if strings.HasSuffix(r.URL.Path, "/decisions/pending") {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleListPendingDecisions(w, r, tankID)
		return
	}

	// /decisions/{decision_id}/resolve
	if strings.HasSuffix(r.URL.Path, "/resolve") {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		decisionID := decisionIDFromResolvePath(r.URL.Path)
		if decisionID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND",
				"expected /v1/tanks/{id}/decisions/{decision_id}/resolve", "")
			return
		}
		s.handleResolveDecision(w, r, tankID, decisionID)
		return
	}

	writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown decisions sub-path", "")
}

// tankIDFromDecisionPath — /v1/tanks/{id}/decisions/... → tank_id.
func tankIDFromDecisionPath(path string) string {
	const prefix = "/v1/tanks/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	idx := strings.Index(rest, "/decisions")
	if idx < 0 {
		return ""
	}
	id := rest[:idx]
	return strings.Trim(id, "/")
}

// decisionIDFromResolvePath — /v1/tanks/{id}/decisions/{decision_id}/resolve → decision_id.
// 경로가 /resolve 로 끝나지 않으면 빈 문자열 반환.
func decisionIDFromResolvePath(path string) string {
	if !strings.HasSuffix(path, "/resolve") {
		return ""
	}
	const marker = "/decisions/"
	idx := strings.LastIndex(path, marker)
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(marker):]
	rest = strings.TrimSuffix(rest, "/resolve")
	rest = strings.Trim(rest, "/")
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// proposeDecisionRequest — AI 가 결정을 제안할 때의 요청 바디.
type proposeDecisionRequest struct {
	DecisionKind    string         `json:"decision_kind"`
	DecisionData    map[string]any `json:"decision_data,omitempty"`
	ProposingSource string         `json:"proposing_source"`
}

// handleProposeDecision — AI 가 결정 제안. 라우팅 계산 후 이벤트 적재.
func (s *Server) handleProposeDecision(w http.ResponseWriter, r *http.Request, tankID string) {
	var req proposeDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}
	if req.DecisionKind == "" {
		writeError(w, http.StatusUnprocessableEntity, "MISSING_DECISION_KIND",
			"decision_kind is required", "")
		return
	}
	switch req.DecisionKind {
	case "feeding", "oxygen_supply", "water_exchange", "pump_adjust", "monitoring":
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_DECISION_KIND",
			"decision_kind must be one of: feeding|oxygen_supply|water_exchange|pump_adjust|monitoring", "")
		return
	}
	if req.ProposingSource == "" {
		req.ProposingSource = "ai.unknown"
	}

	// 라우팅 입력 수집 (confidence + mode)
	in, err := baseline.LoadRoutingInputs(r.Context(), s.store, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ROUTING_INPUT_ERROR", err.Error(), "")
		return
	}
	route, reasoning := baseline.DecideRoute(in)

	// C-3: 라우팅 전 안전 게이트 — critical alert 가 있으면 강제 rejected.
	// 보수적 정책: 운영자가 alert 를 해소해야만 자율 운영 재개 가능.
	gate, gateErr := baseline.EvaluateSafetyGate(r.Context(), s.store, tankID)
	if gateErr == nil && gate.Blocked {
		route = baseline.RouteRejected
		reasoning = "safety_gate_blocked: " + gate.Detail
	}

	now := common.NowUTC()
	decisionID := common.NewID("decision")
	payload := events.TankDecisionRoutedPayload{
		DecisionID:      decisionID,
		TankID:          tankID,
		DecisionKind:    req.DecisionKind,
		DecisionData:    req.DecisionData,
		ProposingSource: req.ProposingSource,
		Confidence:      in.Confidence,
		AutonomousMode:  in.AutonomousMode,
		Route:           string(route),
		Reasoning:       reasoning,
		ProposedAt:      now.Format(time.RFC3339Nano),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusInternalServerError, "PAYLOAD_INVALID", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "decision_routing", tankID,
		events.EventTankDecisionRouted, decisionID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	// pending_approval → 대시보드 알림
	if route == baseline.RoutePendingApproval {
		count := s.countPendingDecisions(r, tankID) + 1
		_ = baseline.RaiseDecisionPendingAlert(r.Context(), s.app, s.store, tankID, count)
	}

	// C-3: auto_executed → 실제 control command 발행 시도 (feeding 만 지원).
	var execResult *control.CommandResult
	var execErr error
	if route == baseline.RouteAutoExecuted {
		execResult, execErr = s.executeAutonomousAction(
			r.Context(), tankID, decisionID, req.DecisionKind, req.DecisionData, "auto_executed")
		if execErr != nil {
			// 명령 발행 실패 → audit + 자율 모드 자동 다운그레이드
			s.recordAutonomousActionBlocked(r.Context(), decisionID, tankID,
				req.DecisionKind, "submission_failed", execErr.Error(), nil)
			s.autoDowngradeMode(r.Context(), tankID, "control_failure", execErr.Error())
		} else if execResult != nil {
			// 실제 명령 발행됨
			s.recordAutonomousActionExecuted(r.Context(), decisionID, tankID,
				req.DecisionKind, execResult.CommandID, execResult.Status, "auto_executed", req.DecisionData)
		}
		// execResult == nil && execErr == nil → audit_only (feeding 외 kind)
	}

	resp := map[string]any{
		"ok":          true,
		"sequence":    seq,
		"decision_id": decisionID,
		"route":       string(route),
		"reasoning":   reasoning,
		"confidence":  in.Confidence,
		"mode":        in.AutonomousMode,
		"proposed_at": payload.ProposedAt,
	}
	if route == baseline.RouteAutoExecuted {
		if execErr != nil {
			resp["execution_error"] = execErr.Error()
		} else if execResult != nil {
			resp["command_id"] = execResult.CommandID
			resp["command_status"] = execResult.Status
		} else {
			resp["audit_only"] = true
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveDecisionRequest — 운영자 결정 처리 요청 바디.
type resolveDecisionRequest struct {
	Resolution string `json:"resolution"`
	OperatorID string `json:"operator_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// handleResolveDecision — 운영자가 pending 결정을 처리 (approved | rejected).
func (s *Server) handleResolveDecision(w http.ResponseWriter, r *http.Request, tankID, decisionID string) {
	var req resolveDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	// API 경로로는 approved | rejected 만 허용. timed_out_* 는 C-3 타이머가 발행.
	switch req.Resolution {
	case "approved", "rejected":
	default:
		writeError(w, http.StatusUnprocessableEntity, "INVALID_RESOLUTION",
			"resolution must be one of: approved, rejected (timed_out_* are emitted by timers in C-3)", "")
		return
	}

	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}

	now := common.NowUTC()
	payload := events.TankDecisionResolvedPayload{
		DecisionID: decisionID,
		TankID:     tankID,
		Resolution: req.Resolution,
		OperatorID: req.OperatorID,
		Reason:     req.Reason,
		ResolvedAt: now.Format(time.RFC3339Nano),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusInternalServerError, "PAYLOAD_INVALID", err.Error(), "")
		return
	}

	seq, err := s.app.AppendEvent(r.Context(),
		"api", "decision_resolve", tankID,
		events.EventTankDecisionResolved, decisionID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EVENT_APPEND_FAILED", err.Error(), "")
		return
	}

	// 해소 후 pending count 갱신 → 알림 조정
	count := s.countPendingDecisions(r, tankID)
	_ = baseline.RaiseDecisionPendingAlert(r.Context(), s.app, s.store, tankID, count)

	// C-3: approved → 안전 게이트 재확인 후 control command 발행.
	// 시간이 흐른 사이 critical alert 가 새로 떴을 수 있음.
	var resolveExecResult *control.CommandResult
	if req.Resolution == "approved" {
		routed := s.findRoutedDecision(r.Context(), tankID, decisionID)
		if routed != nil {
			gate, _ := baseline.EvaluateSafetyGate(r.Context(), s.store, tankID)
			if gate.Blocked {
				s.recordAutonomousActionBlocked(r.Context(), decisionID, tankID,
					routed.DecisionKind, "safety_gate", gate.Detail, nil)
			} else {
				execResult, execErr := s.executeAutonomousAction(
					r.Context(), tankID, decisionID, routed.DecisionKind, routed.DecisionData, "approved")
				if execErr != nil {
					s.recordAutonomousActionBlocked(r.Context(), decisionID, tankID,
						routed.DecisionKind, "submission_failed", execErr.Error(), nil)
					s.autoDowngradeMode(r.Context(), tankID, "control_failure", execErr.Error())
				} else if execResult != nil {
					s.recordAutonomousActionExecuted(r.Context(), decisionID, tankID,
						routed.DecisionKind, execResult.CommandID, execResult.Status, "approved", routed.DecisionData)
					resolveExecResult = execResult
				}
			}
		}
	}

	resp := map[string]any{
		"ok":          true,
		"sequence":    seq,
		"decision_id": decisionID,
		"resolution":  req.Resolution,
		"resolved_at": payload.ResolvedAt,
	}
	if resolveExecResult != nil {
		resp["command_id"] = resolveExecResult.CommandID
		resp["command_status"] = resolveExecResult.Status
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleListPendingDecisions — 이 Cage/Tank의 미해소 pending_approval | pending_notify 결정 목록.
func (s *Server) handleListPendingDecisions(w http.ResponseWriter, r *http.Request, tankID string) {
	items := s.collectPendingDecisions(r, tankID, 50)
	writeJSON(w, http.StatusOK, map[string]any{
		"tank_id": tankID,
		"count":   len(items),
		"items":   items,
	})
}

// collectPendingDecisions — pending 결정 목록 (최대 limit 건). handleListPendingDecisions + state vector 공용.
func (s *Server) collectPendingDecisions(r *http.Request, tankID string, limit int) []map[string]any {
	routedEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankDecisionRouted,
		Limit:     200,
	})
	if err != nil {
		return nil
	}

	resolvedIDs := s.resolvedDecisionIDs(r, tankID)

	var pending []map[string]any
	for _, e := range routedEvents {
		var p events.TankDecisionRoutedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID {
			continue
		}
		if p.Route != string(baseline.RoutePendingApproval) && p.Route != string(baseline.RoutePendingNotify) {
			continue
		}
		if resolvedIDs[p.DecisionID] {
			continue
		}
		pending = append(pending, map[string]any{
			"decision_id":      p.DecisionID,
			"decision_kind":    p.DecisionKind,
			"route":            p.Route,
			"proposing_source": p.ProposingSource,
			"confidence":       p.Confidence,
			"autonomous_mode":  p.AutonomousMode,
			"reasoning":        p.Reasoning,
			"proposed_at":      p.ProposedAt,
		})
		if len(pending) >= limit {
			break
		}
	}

	// ProposedAt 내림차순 (이미 최신 우선이지만 명시적으로 정렬)
	sort.Slice(pending, func(i, j int) bool {
		ai, _ := pending[i]["proposed_at"].(string)
		aj, _ := pending[j]["proposed_at"].(string)
		return ai > aj
	})
	return pending
}

// resolvedDecisionIDs — tank 의 tank.decision.resolved 이벤트에서 decision_id set 반환.
func (s *Server) resolvedDecisionIDs(r *http.Request, tankID string) map[string]bool {
	resolvedEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankDecisionResolved,
		Limit:     500,
	})
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool)
	for _, e := range resolvedEvents {
		var p events.TankDecisionResolvedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID {
			out[p.DecisionID] = true
		}
	}
	return out
}

// countPendingDecisions — pending 건수만 반환 (알림 업데이트용).
func (s *Server) countPendingDecisions(r *http.Request, tankID string) int {
	return len(s.collectPendingDecisions(r, tankID, 200))
}

// lastDecisionRouted — state vector 용: 가장 최근 라우팅 이벤트.
func (s *Server) lastDecisionRouted(r *http.Request, tankID string) (*events.TankDecisionRoutedPayload, bool) {
	es, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankDecisionRouted,
		Limit:     50,
	})
	if err != nil {
		return nil, false
	}
	for _, e := range es {
		var p events.TankDecisionRoutedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID {
			return &p, true
		}
	}
	return nil, false
}

// fmtDecisionTime — proposed_at 을 RFC3339Nano 로 포맷 (비어 있으면 빈 문자열).
func fmtDecisionTime(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// ── C-3 헬퍼 ─────────────────────────────────────────────────────────────────

// executeAutonomousAction — feeding 만 실제 명령 발행. 다른 kind 는 audit_only.
// 반환: (CommandResult, error). audit_only 인 경우 nil result + nil error.
func (s *Server) executeAutonomousAction(
	ctx context.Context, tankID, decisionID, kind string,
	data map[string]any, trigger string,
) (*control.CommandResult, error) {
	plan := baseline.PlanCommand(kind, data)
	if !plan.SupportedKind {
		// feeding 외 kind — audit 기록 후 nil (에러 아님)
		s.recordAutonomousActionBlocked(ctx, decisionID, tankID,
			kind, "control_not_wired", plan.Reason, nil)
		return nil, nil
	}

	req := control.CommandRequest{
		IdempotencyKey: "decision:" + decisionID + ":" + trigger,
		RequestedBy:    map[string]any{"source": "autonomous", "trigger": trigger},
		Target:         map[string]any{"device_id": plan.DeviceID, "tank_id": tankID},
		Command:        plan.CommandBody,
		CorrelationID:  decisionID,
	}
	return s.ctrl.Submit(ctx, req)
}

// recordAutonomousActionExecuted — 실제 명령 발행 완료 audit.
func (s *Server) recordAutonomousActionExecuted(
	ctx context.Context, decisionID, tankID, kind, commandID, commandStatus, trigger string,
	evidence map[string]any,
) {
	p := events.AutonomousActionExecutedPayload{
		DecisionID:    decisionID,
		TankID:        tankID,
		DecisionKind:  kind,
		CommandID:     commandID,
		CommandStatus: commandStatus,
		ExecutedAt:    common.FormatTime(common.NowUTC()),
		Trigger:       trigger,
		Evidence:      evidence,
	}
	if err := p.Validate(); err != nil {
		return // best-effort
	}
	_, _ = s.app.AppendEvent(ctx, "api", "autonomous", tankID,
		events.EventAutonomousActionExecuted, common.NewID("exec"), p)
}

// recordAutonomousActionBlocked — 차단 audit.
func (s *Server) recordAutonomousActionBlocked(
	ctx context.Context, decisionID, tankID, kind, reason, detail string,
	evidence map[string]any,
) {
	p := events.AutonomousActionBlockedPayload{
		DecisionID:   decisionID,
		TankID:       tankID,
		DecisionKind: kind,
		Reason:       reason,
		Detail:       detail,
		BlockedAt:    common.FormatTime(common.NowUTC()),
		Evidence:     evidence,
	}
	if err := p.Validate(); err != nil {
		return // best-effort
	}
	_, _ = s.app.AppendEvent(ctx, "api", "autonomous", tankID,
		events.EventAutonomousActionBlocked, common.NewID("block"), p)
}

// autoDowngradeMode — control 실패 등 안전 트리거로 자율 모드를 observation 으로 내림.
// 이미 off | observation 이면 noop.
func (s *Server) autoDowngradeMode(ctx context.Context, tankID, reason, detail string) {
	row, err := s.store.GetTankAutonomousMode(ctx, tankID)
	if err != nil || row == nil {
		return
	}
	if row.Mode == "off" || row.Mode == "observation" {
		return // 이미 안전 상태
	}
	prev := row.Mode
	row.Mode = "observation"
	row.Reason = "auto-downgraded: " + reason
	row.ChangedAt = common.NowUTC()
	row.ChangedBy = "system"
	if err := s.store.UpsertTankAutonomousMode(ctx, row); err != nil {
		return
	}
	ev := events.AutonomousModeAutoDowngradePayload{
		TankID:       tankID,
		PreviousMode: prev,
		NewMode:      "observation",
		Reason:       "control_failure",
		Detail:       detail,
		DowngradedAt: common.FormatTime(common.NowUTC()),
	}
	if err := ev.Validate(); err != nil {
		return // best-effort
	}
	_, _ = s.app.AppendEvent(ctx, "api", "autonomous", tankID,
		events.EventAutonomousModeAutoDowngrade, common.NewID("downgrade"), ev)
}

// findRoutedDecision — 이 Cage/Tank의 tank.decision.routed 이벤트에서 decision_id 로 검색.
func (s *Server) findRoutedDecision(ctx context.Context, tankID, decisionID string) *events.TankDecisionRoutedPayload {
	es, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankDecisionRouted,
		Limit:     500,
	})
	if err != nil {
		return nil
	}
	for _, e := range es {
		var p events.TankDecisionRoutedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID && p.DecisionID == decisionID {
			return &p
		}
	}
	return nil
}
