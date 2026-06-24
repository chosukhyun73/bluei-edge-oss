package baseline

import (
	"context"
	"fmt"
	"strings"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// SafetyGateResult — 안전 게이트 평가 결과.
type SafetyGateResult struct {
	Blocked bool
	Reason  string // ""(통과) | "open_critical_alert"
	Detail  string
	Notes   []string
}

// EvaluateSafetyGate — Cage/Tank에 열린 critical alert 가 있는지 확인.
// 차단 조건: subject_kind=tank AND subject_id=tankID AND severity=critical AND status=open
// 보수적 정책: ANY open critical alert 가 있으면 자율 결정 차단.
// 운영자가 alert 를 해소해야만 자율 운영 재개 가능.
func EvaluateSafetyGate(ctx context.Context, store storage.Store, tankID string) (SafetyGateResult, error) {
	alerts, err := store.ListOpenAlerts(ctx)
	if err != nil {
		return SafetyGateResult{}, fmt.Errorf("safety gate: list open alerts: %w", err)
	}

	var blocked []string
	for _, a := range alerts {
		if a.SubjectKind == "tank" &&
			a.SubjectID == tankID &&
			a.Severity == events.SeverityCritical &&
			a.Status == events.AlertStatusOpen {
			blocked = append(blocked, a.AlertType)
		}
	}

	if len(blocked) == 0 {
		return SafetyGateResult{Blocked: false}, nil
	}

	detail := fmt.Sprintf("Cage/Tank %s 에 미해소 critical 알림 %d건: %s",
		tankID, len(blocked), strings.Join(blocked, ", "))
	return SafetyGateResult{
		Blocked: true,
		Reason:  "open_critical_alert",
		Detail:  detail,
		Notes:   blocked,
	}, nil
}
