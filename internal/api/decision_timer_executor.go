package api

import (
	"context"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
)

// decisionTimerExecutor — Server 의 자율 실행 경로를 baseline.TimerExecutor 인터페이스로 노출.
type decisionTimerExecutor struct{ s *Server }

// DecisionTimerExecutor — public factory for main.go wiring.
func (s *Server) DecisionTimerExecutor() baseline.TimerExecutor {
	return &decisionTimerExecutor{s: s}
}

// ExecuteTimedOut — pending_notify 의 grace 경과 처리.
// safety gate → mode → policy 재확인 후 실행 또는 스킵.
func (e *decisionTimerExecutor) ExecuteTimedOut(ctx context.Context, decisionID, tankID, kind string, data map[string]any) error {
	// 1. Safety gate 재확인 — 시간 흐른 사이 critical alert 가 새로 떴을 수 있음
	gate, _ := baseline.EvaluateSafetyGate(ctx, e.s.store, tankID)
	if gate.Blocked {
		e.s.recordTimedOutSkipped(ctx, decisionID, tankID, "safety_gate", gate.Detail)
		return nil
	}

	// 2. Autonomous mode 재확인 — observation/off 로 다운그레이드됐을 수 있음
	mode, _ := e.s.store.GetTankAutonomousMode(ctx, tankID)
	currentMode := "off"
	if mode != nil {
		currentMode = mode.Mode
	}
	if currentMode == "off" || currentMode == "observation" {
		e.s.recordTimedOutSkipped(ctx, decisionID, tankID, "mode_downgraded", "current="+currentMode)
		return nil
	}

	// 3. 정책 enabled 재확인 — 운영자가 사이에 끄지 않았는지
	enabled, _ := e.s.effectiveDecisionPolicy(ctx, tankID)
	if !enabled {
		e.s.recordTimedOutSkipped(ctx, decisionID, tankID, "policy_disabled", "auto_execute_enabled=false")
		return nil
	}

	// 4. C-3 의 executeAutonomousAction 재사용 (feeding 만 wired)
	res, err := e.s.executeAutonomousAction(ctx, tankID, decisionID, kind, data, "timed_out_executed")
	if err != nil {
		// submission failure → audit + auto-downgrade (C-3 동일 패턴)
		e.s.recordAutonomousActionBlocked(ctx, decisionID, tankID,
			kind, "submission_failed", err.Error(), nil)
		e.s.autoDowngradeMode(ctx, tankID, "control_failure", err.Error())
		return nil
	}
	if res != nil {
		e.s.recordAutonomousActionExecuted(ctx, decisionID, tankID,
			kind, res.CommandID, res.Status, "timed_out_executed", data)
	}
	// audit_only (feeding 외) 또는 executed → 모두 resolve event 적재
	e.s.recordTimedOutResolved(ctx, decisionID, tankID, "timed_out_executed", "")
	return nil
}

// recordTimedOutSkipped — timed_out_skipped resolution 으로 결정 종료 처리.
func (s *Server) recordTimedOutSkipped(ctx context.Context, decisionID, tankID, reason, detail string) {
	now := common.FormatTime(common.NowUTC())
	p := events.TankDecisionResolvedPayload{
		DecisionID: decisionID,
		TankID:     tankID,
		Resolution: "timed_out_skipped",
		OperatorID: "decision_timer",
		Reason:     reason + ": " + detail,
		ResolvedAt: now,
	}
	if err := p.Validate(); err != nil {
		return // best-effort
	}
	_, _ = s.app.AppendEvent(ctx, "decision_timer", "auto", tankID,
		events.EventTankDecisionResolved, decisionID, p)
}

// recordTimedOutResolved — timed_out_executed resolution 으로 결정 종료 처리.
func (s *Server) recordTimedOutResolved(ctx context.Context, decisionID, tankID, resolution, detail string) {
	resolvedAt := time.Now().UTC().Format(time.RFC3339Nano)
	p := events.TankDecisionResolvedPayload{
		DecisionID: decisionID,
		TankID:     tankID,
		Resolution: resolution,
		OperatorID: "decision_timer",
		Reason:     detail,
		ResolvedAt: resolvedAt,
	}
	if err := p.Validate(); err != nil {
		return // best-effort
	}
	_, _ = s.app.AppendEvent(ctx, "decision_timer", "auto", tankID,
		events.EventTankDecisionResolved, decisionID, p)
}
