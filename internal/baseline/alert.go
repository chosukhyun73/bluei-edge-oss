package baseline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// alertTypeAnomaly — Cage/Tank baseline 자체에서 올라온 알림 타입.
// rules engine 의 임계값 알림과 출처가 다름을 운영자가 식별할 수 있게 분리.
const alertTypeAnomaly = "tank.baseline.anomaly"

// dedupeKey — 한 Cage/Tank당 동시에 1건의 baseline 알림만 열려 있도록.
// verdict 가 normal 로 바뀌면 자동 close.
func dedupeKey(tankID string) string {
	return "tank.baseline." + tankID
}

// MaybeRaiseOrCloseAlert — score 결과의 verdict 에 따라 알림 상태를 조정한다.
//
//   - verdict=anomaly  → alert raise (severity=critical)
//   - verdict=warning  → alert raise (severity=warning)
//   - verdict=normal   → 열린 alert 가 있으면 close
//
// 같은 verdict 가 반복되면 update 만 (대시보드 noise 회피).
// rules engine 의 안전 알림과 별개 — 이 알림은 비-actuating, 운영자 인지용.
func MaybeRaiseOrCloseAlert(ctx context.Context, app *runtime.App, store storage.Store, p events.TankBaselineScoredPayload) error {
	key := dedupeKey(p.TankID)
	existing, err := store.GetOpenAlert(ctx, key)
	if err != nil {
		return fmt.Errorf("get open alert: %w", err)
	}

	switch p.Verdict {
	case "anomaly", "warning":
		return raiseAnomalyAlert(ctx, app, store, p, key, existing)
	case "normal":
		if existing == nil {
			return nil
		}
		return closeAnomalyAlert(ctx, app, store, p, existing)
	default:
		return nil
	}
}

func raiseAnomalyAlert(ctx context.Context, app *runtime.App, store storage.Store,
	p events.TankBaselineScoredPayload, key string, existing *storage.OpenAlert) error {
	severity := events.SeverityWarning
	if p.Verdict == "anomaly" {
		severity = events.SeverityCritical
	}
	now := common.NowUTC()
	msg := buildAnomalyMessage(p)
	evidence := map[string]any{
		"tank_id":       p.TankID,
		"verdict":       p.Verdict,
		"anomaly_score": p.AnomalyScore,
		"p95_threshold": p.P95Threshold,
		"p99_threshold": p.P99Threshold,
		"top_features":  topFeatureDiffs(p.FeatureDiff, 3),
		"model_dir":     p.ModelDir,
		"job_id":        p.JobID,
		"evaluated_at":  p.EvaluatedAt,
	}

	if existing == nil {
		alertID := common.NewAlertID()
		payload := events.AlertPayload{
			AlertID:   alertID,
			AlertType: alertTypeAnomaly,
			Severity:  severity,
			Status:    events.AlertStatusOpen,
			Subject:   events.AlertSubject{Kind: "tank", ID: p.TankID},
			Message:   msg,
			Evidence:  evidence,
			RaisedAt:  common.FormatTime(now),
			UpdatedAt: common.FormatTime(now),
		}
		body, _ := json.Marshal(payload)
		row := &storage.OpenAlert{
			AlertID:        alertID,
			AlertDedupeKey: key,
			AlertType:      alertTypeAnomaly,
			Severity:       severity,
			SubjectKind:    "tank",
			SubjectID:      p.TankID,
			Status:         events.AlertStatusOpen,
			RaisedAt:       now,
			UpdatedAt:      now,
			PayloadJSON:    string(body),
		}
		if _, err := store.UpsertAlert(ctx, row); err != nil {
			return fmt.Errorf("upsert alert: %w", err)
		}
		_, err := app.AppendEvent(ctx, "baseline", "", p.TankID,
			events.EventAlertRaised, alertID, payload)
		return err
	}

	// 이미 열려있음 → severity/메시지 갱신
	existing.Severity = severity
	existing.Status = events.AlertStatusOpen
	existing.UpdatedAt = now
	payload := events.AlertPayload{
		AlertID:   existing.AlertID,
		AlertType: existing.AlertType,
		Severity:  severity,
		Status:    events.AlertStatusOpen,
		Subject:   events.AlertSubject{Kind: existing.SubjectKind, ID: existing.SubjectID},
		Message:   msg,
		Evidence:  evidence,
		RaisedAt:  common.FormatTime(existing.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	body, _ := json.Marshal(payload)
	existing.PayloadJSON = string(body)
	if _, err := store.UpsertAlert(ctx, existing); err != nil {
		return fmt.Errorf("update alert: %w", err)
	}
	_, err := app.AppendEvent(ctx, "baseline", "", p.TankID,
		events.EventAlertUpdated, existing.AlertID, payload)
	return err
}

func closeAnomalyAlert(ctx context.Context, app *runtime.App, store storage.Store,
	p events.TankBaselineScoredPayload, existing *storage.OpenAlert) error {
	now := common.NowUTC()
	payload := events.AlertPayload{
		AlertID:   existing.AlertID,
		AlertType: existing.AlertType,
		Severity:  existing.Severity,
		Status:    events.AlertStatusResolved,
		Subject:   events.AlertSubject{Kind: existing.SubjectKind, ID: existing.SubjectID},
		Message:   fmt.Sprintf("이 Cage/Tank가 평소 범위로 복귀했습니다 (anomaly score %.4f)", p.AnomalyScore),
		Evidence: map[string]any{
			"resolution_reason": "verdict_returned_normal",
			"anomaly_score":     p.AnomalyScore,
			"p95_threshold":     p.P95Threshold,
			"evaluated_at":      p.EvaluatedAt,
		},
		RaisedAt:  common.FormatTime(existing.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	// open_alerts 테이블에서 제거 (rules engine 의 clearAlert 와 동일 패턴).
	// alert.updated event 는 audit 용으로 적재됨.
	if err := store.ClearAlert(ctx, existing.AlertDedupeKey); err != nil {
		return fmt.Errorf("clear alert: %w", err)
	}
	_, err := app.AppendEvent(ctx, "baseline", "", p.TankID,
		events.EventAlertUpdated, existing.AlertID, payload)
	return err
}

func buildAnomalyMessage(p events.TankBaselineScoredPayload) string {
	prefix := "주의"
	if p.Verdict == "anomaly" {
		prefix = "이상"
	}
	top := topFeatureDiffs(p.FeatureDiff, 3)
	if len(top) == 0 {
		return fmt.Sprintf("[%s] Cage/Tank %s 가 평소와 다릅니다 (점수 %.4f, 임계 p95=%.4f)",
			prefix, p.TankID, p.AnomalyScore, p.P95Threshold)
	}
	names := make([]string, 0, len(top))
	for _, t := range top {
		names = append(names, t.Name)
	}
	return fmt.Sprintf("[%s] Cage/Tank %s 가 평소와 다릅니다. 주요 이탈: %s (점수 %.4f, 임계 p95=%.4f)",
		prefix, p.TankID, joinComma(names), p.AnomalyScore, p.P95Threshold)
}

type featureDiff struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// topFeatureDiffs — 가장 큰 deviation 3개 반환. 알림 메시지/evidence 에 사용.
func topFeatureDiffs(m map[string]float64, n int) []featureDiff {
	if len(m) == 0 {
		return nil
	}
	out := make([]featureDiff, 0, len(m))
	for k, v := range m {
		out = append(out, featureDiff{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// alertTypeTransition — Phase 3.5 성장 단계 전환 알림 타입.
// baseline anomaly 알림과 dedupeKey 분리.
const alertTypeTransition = "tank.transition"

// raiseTransitionAlert — Phase 3.5 전환 감지 알림.
// Severity 는 warning (자율 운영 중단 X). 기존 anomaly 알림과 dedupeKey 분리.
// TODO Phase 3.5+: auto-close after retraining + recovery.
func raiseTransitionAlert(ctx context.Context, app *runtime.App, store storage.Store, p events.TankTransitionDetectedPayload) error {
	key := "tank.transition." + p.TankID
	existing, err := store.GetOpenAlert(ctx, key)
	if err != nil {
		return fmt.Errorf("get open alert: %w", err)
	}

	now := common.NowUTC()
	msg := buildTransitionMessage(p)
	evidence := map[string]any{
		"tank_id":     p.TankID,
		"reason":      p.Reason,
		"detected_at": p.DetectedAt,
	}
	for k, v := range p.Evidence {
		evidence[k] = v
	}

	if existing == nil {
		alertID := common.NewAlertID()
		payload := events.AlertPayload{
			AlertID:   alertID,
			AlertType: alertTypeTransition,
			Severity:  events.SeverityWarning,
			Status:    events.AlertStatusOpen,
			Subject:   events.AlertSubject{Kind: "tank", ID: p.TankID},
			Message:   msg,
			Evidence:  evidence,
			RaisedAt:  common.FormatTime(now),
			UpdatedAt: common.FormatTime(now),
		}
		body, _ := json.Marshal(payload)
		row := &storage.OpenAlert{
			AlertID:        alertID,
			AlertDedupeKey: key,
			AlertType:      alertTypeTransition,
			Severity:       events.SeverityWarning,
			SubjectKind:    "tank",
			SubjectID:      p.TankID,
			Status:         events.AlertStatusOpen,
			RaisedAt:       now,
			UpdatedAt:      now,
			PayloadJSON:    string(body),
		}
		if _, err := store.UpsertAlert(ctx, row); err != nil {
			return fmt.Errorf("upsert transition alert: %w", err)
		}
		_, err := app.AppendEvent(ctx, "baseline", "", p.TankID,
			events.EventAlertRaised, alertID, payload)
		return err
	}

	// 이미 열려있음 → 메시지/UpdatedAt 갱신
	existing.UpdatedAt = now
	payload := events.AlertPayload{
		AlertID:   existing.AlertID,
		AlertType: existing.AlertType,
		Severity:  events.SeverityWarning,
		Status:    events.AlertStatusOpen,
		Subject:   events.AlertSubject{Kind: existing.SubjectKind, ID: existing.SubjectID},
		Message:   msg,
		Evidence:  evidence,
		RaisedAt:  common.FormatTime(existing.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	body, _ := json.Marshal(payload)
	existing.PayloadJSON = string(body)
	if _, err := store.UpsertAlert(ctx, existing); err != nil {
		return fmt.Errorf("update transition alert: %w", err)
	}
	_, err = app.AppendEvent(ctx, "baseline", "", p.TankID,
		events.EventAlertUpdated, existing.AlertID, payload)
	return err
}

func buildTransitionMessage(p events.TankTransitionDetectedPayload) string {
	switch p.Reason {
	case "weight_threshold_passed":
		w, _ := p.Evidence["current_weight"].(float64)
		t, _ := p.Evidence["threshold_passed"].(float64)
		return fmt.Sprintf("Cage/Tank %s 가 성장 단계 전환 가능 — 체중 %.0fg 가 임계 %.0fg 통과. baseline 재학습 권장.",
			p.TankID, w, t)
	case "anomaly_drift_detected":
		rr, _ := p.Evidence["recent_normal_rate"].(float64)
		or_, _ := p.Evidence["older_normal_rate"].(float64)
		rm, _ := p.Evidence["recent_mean_score"].(float64)
		om, _ := p.Evidence["older_mean_score"].(float64)
		return fmt.Sprintf("Cage/Tank %s 의 평소 패턴이 최근 변했습니다. 정상 비율 %.0f%% → %.0f%%, 점수 평균 %.4f → %.4f 증가. baseline 재학습 권장.",
			p.TankID, or_*100, rr*100, om, rm)
	default:
		return fmt.Sprintf("Cage/Tank %s 성장 단계 전환 감지 (%s). baseline 재학습 권장.", p.TankID, p.Reason)
	}
}

// ── Decision Pending Alert (Phase 4 C-2) ──

const alertTypeDecisionPending = "tank.decision.pending"

// RaiseDecisionPendingAlert — pending_approval 결정이 있을 때 운영자 대시보드 알림.
// pendingCount > 0 이면 raise/update, 0 이면 clear.
// dedupeKey: "tank.decision.{tankID}" — Cage/Tank당 1건.
func RaiseDecisionPendingAlert(ctx context.Context, app *runtime.App, store storage.Store, tankID string, pendingCount int) error {
	key := "tank.decision." + tankID
	existing, err := store.GetOpenAlert(ctx, key)
	if err != nil {
		return fmt.Errorf("get decision alert: %w", err)
	}

	if pendingCount == 0 {
		if existing == nil {
			return nil
		}
		return clearDecisionAlert(ctx, app, store, tankID, existing)
	}

	msg := fmt.Sprintf("Cage/Tank %s 에 운영자 결정 대기 %d건", tankID, pendingCount)
	now := common.NowUTC()
	evidence := map[string]any{
		"tank_id":       tankID,
		"pending_count": pendingCount,
	}

	if existing == nil {
		alertID := common.NewAlertID()
		payload := events.AlertPayload{
			AlertID:   alertID,
			AlertType: alertTypeDecisionPending,
			Severity:  events.SeverityWarning,
			Status:    events.AlertStatusOpen,
			Subject:   events.AlertSubject{Kind: "tank", ID: tankID},
			Message:   msg,
			Evidence:  evidence,
			RaisedAt:  common.FormatTime(now),
			UpdatedAt: common.FormatTime(now),
		}
		body, _ := json.Marshal(payload)
		row := &storage.OpenAlert{
			AlertID:        alertID,
			AlertDedupeKey: key,
			AlertType:      alertTypeDecisionPending,
			Severity:       events.SeverityWarning,
			SubjectKind:    "tank",
			SubjectID:      tankID,
			Status:         events.AlertStatusOpen,
			RaisedAt:       now,
			UpdatedAt:      now,
			PayloadJSON:    string(body),
		}
		if _, err := store.UpsertAlert(ctx, row); err != nil {
			return fmt.Errorf("upsert decision alert: %w", err)
		}
		_, err := app.AppendEvent(ctx, "baseline", "", tankID,
			events.EventAlertRaised, alertID, payload)
		return err
	}

	// 이미 열려 있음 → 메시지/카운트 갱신
	existing.UpdatedAt = now
	payload := events.AlertPayload{
		AlertID:   existing.AlertID,
		AlertType: alertTypeDecisionPending,
		Severity:  events.SeverityWarning,
		Status:    events.AlertStatusOpen,
		Subject:   events.AlertSubject{Kind: "tank", ID: tankID},
		Message:   msg,
		Evidence:  evidence,
		RaisedAt:  common.FormatTime(existing.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	body, _ := json.Marshal(payload)
	existing.PayloadJSON = string(body)
	if _, err := store.UpsertAlert(ctx, existing); err != nil {
		return fmt.Errorf("update decision alert: %w", err)
	}
	_, err = app.AppendEvent(ctx, "baseline", "", tankID,
		events.EventAlertUpdated, existing.AlertID, payload)
	return err
}

func clearDecisionAlert(ctx context.Context, app *runtime.App, store storage.Store, tankID string, existing *storage.OpenAlert) error {
	now := common.NowUTC()
	payload := events.AlertPayload{
		AlertID:   existing.AlertID,
		AlertType: alertTypeDecisionPending,
		Severity:  existing.Severity,
		Status:    events.AlertStatusResolved,
		Subject:   events.AlertSubject{Kind: "tank", ID: tankID},
		Message:   fmt.Sprintf("Cage/Tank %s 의 대기 결정이 모두 처리되었습니다.", tankID),
		Evidence:  map[string]any{"resolution_reason": "all_pending_resolved"},
		RaisedAt:  common.FormatTime(existing.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	if err := store.ClearAlert(ctx, existing.AlertDedupeKey); err != nil {
		return fmt.Errorf("clear decision alert: %w", err)
	}
	_, err := app.AppendEvent(ctx, "baseline", "", tankID,
		events.EventAlertUpdated, existing.AlertID, payload)
	return err
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
