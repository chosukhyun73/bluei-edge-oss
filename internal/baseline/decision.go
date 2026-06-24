package baseline

import (
	"context"
	"fmt"

	"bluei.kr/edge/internal/storage"
)

// Route — 결정 라우팅 결과.
type Route string

const (
	RouteAutoExecuted    Route = "auto_executed"
	RoutePendingNotify   Route = "pending_notify"
	RoutePendingApproval Route = "pending_approval"
	RouteAdvisoryOnly    Route = "advisory_only"
	RouteRejected        Route = "rejected"
)

// RouteDecisionInputs — 라우팅 입력. 호출자가 미리 채워서 넘긴다.
type RouteDecisionInputs struct {
	TankID         string
	Confidence     float64 // Tank Confidence Composite (0..1)
	AutonomousMode string  // off | observation | partial | full
}

// DecideRoute — 라우팅 표(docs/Phase4 C-2) 적용. 순수 함수 (외부 IO 없음).
// Confidence 밴드: <0.3 cold / 0.3..0.6 observation / 0.6..0.85 adapted / ≥0.85 autonomous
func DecideRoute(in RouteDecisionInputs) (Route, string) {
	c := in.Confidence
	m := in.AutonomousMode

	// cold 구간 — 모든 모드에서 거부
	if c < 0.3 {
		return RouteRejected, fmt.Sprintf(
			"신뢰도 %.2f (cold) — 자율/제안 모두 거부, 운영자 직접 결정 필요", c)
	}

	// observation 모드 — AI 결정은 표시만, 실행 X (docs/29 §3.4)
	if m == "observation" {
		return RouteAdvisoryOnly, fmt.Sprintf(
			"autonomous_mode=observation — AI 결정은 표시만, 실행 X (신뢰도 %.2f)", c)
	}

	// off 모드 — 모든 autonomy 차단, 운영자 승인 필수
	if m == "off" {
		return RoutePendingApproval, fmt.Sprintf(
			"autonomous_mode=off — 자율 차단, 운영자 승인 필수 (신뢰도 %.2f)", c)
	}

	// partial | full 모드 — 밴드별 라우팅
	switch {
	case c < 0.6:
		// 0.3 ≤ c < 0.6
		return RoutePendingApproval, fmt.Sprintf(
			"신뢰도 %.2f (observation 밴드) + mode=%s → 운영자 승인 필요", c, m)
	case c < 0.85:
		// 0.6 ≤ c < 0.85
		return RoutePendingNotify, fmt.Sprintf(
			"신뢰도 %.2f (adapted 밴드) + mode=%s → 운영자 푸시 + 자동 실행 대기 (C-3)", c, m)
	default:
		// c ≥ 0.85
		if m == "full" {
			return RouteAutoExecuted, fmt.Sprintf(
				"신뢰도 %.2f (high) + autonomous_mode=full → 자율 실행 가능 (사후 알림)", c)
		}
		// partial + ≥0.85
		return RoutePendingNotify, fmt.Sprintf(
			"신뢰도 %.2f (high) + mode=partial → 운영자 푸시 + 자동 실행 대기 (C-3)", c)
	}
}

// LoadRoutingInputs — store + manifest 에서 입력 자동 수집.
// confidence = ComputeTankConfidence().Composite
// mode = GetTankAutonomousMode → nil 이면 "off"
func LoadRoutingInputs(ctx context.Context, store storage.Store, tankID string) (RouteDecisionInputs, error) {
	in := RouteDecisionInputs{TankID: tankID}

	// 모드 조회
	modeRow, err := store.GetTankAutonomousMode(ctx, tankID)
	if err != nil {
		return in, fmt.Errorf("get autonomous mode: %w", err)
	}
	if modeRow == nil {
		in.AutonomousMode = "off"
	} else {
		in.AutonomousMode = modeRow.Mode
	}

	// confidence 산출
	components, err := ComputeTankConfidence(ctx, store, tankID)
	if err != nil {
		return in, fmt.Errorf("compute confidence: %w", err)
	}
	in.Confidence = components.Composite

	return in, nil
}
