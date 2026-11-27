package feed_cycle

// safety_block 연속 에스컬레이션 (2026-07-16 운영자 지시).
//
// 배경: 외부 장비 원격 API 순단(JSON 대신 HTML 응답)으로 사이클이 fail-safe 중단된
// 실사례(07-16 11:30 tank_01, 2/5펄스). 1회 순단은 그 사이클의 결손으로 그냥
// 넘기지만(결손 이월은 2026-08-23 폐지), 같은 수조 연속 2회면 원격 인프라 지속 장애로 보고
//   ① critical 알림  ② 해당 수조 급이 일시정지(operating_mode=manual)
// 로 헛발사·반복 결식을 막는다. 재개는 운영자가 대시보드 자동/수동 토글로
// (확립된 정지 메커니즘 — AIScheduler 가 manual 수조의 스케줄을 제거/미생성).

import (
	"context"
	"encoding/json"
	"fmt"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

// safetyBlockEscalateStreak — 연속 safety_block 임계. 1회는 순단 허용(이월로 흡수).
const safetyBlockEscalateStreak = 2

// safetyBlockStreak — 최근 완료 사이클 목록(최신순)에서 선두 연속 safety_block 수.
func safetyBlockStreak(rows []*storage.FeedCycle) int {
	streak := 0
	for _, r := range rows {
		if r.CompletedAt == nil {
			continue // 진행 중 행은 건너뜀 (판정 대상 아님)
		}
		if r.TerminationReason != string(ReasonSafetyBlock) {
			break
		}
		streak++
	}
	return streak
}

// escalateConsecutiveSafetyBlock — finalizeCycle 훅(goroutine). 연속 임계 도달 시
// 급이 일시정지 + critical 알림. 실패는 로그만 (사이클 경로에 영향 없음).
func (w *Worker) escalateConsecutiveSafetyBlock(ctx context.Context, tankID, cycleID string) {
	rows, err := w.store.ListRecentFeedCycles(ctx, tankID, safetyBlockEscalateStreak+1)
	if err != nil {
		w.log.Warn("safety escalation: list cycles failed", "tank_id", tankID, "error", err)
		return
	}
	streak := safetyBlockStreak(rows)
	if streak < safetyBlockEscalateStreak {
		return
	}

	paused, err := w.pauseTankFeeding(ctx, tankID)
	if err != nil {
		w.log.Error("safety escalation: pause failed — 알림만 발행",
			"tank_id", tankID, "error", err)
	}
	if !paused && err == nil {
		return // 이미 manual — 재알림 없이 종료 (idempotent)
	}

	at := common.NowUTC()
	alertID := common.NewID("alert")
	msg := fmt.Sprintf("급이 안전차단 %d회 연속 (%s) — 원격 급이기 API 장애 의심. "+
		"급이를 일시정지했습니다. 장비/외부 장비 확인 후 대시보드에서 자동 모드로 재개하세요.",
		streak, tankID)
	payload := map[string]any{
		"alert_id":   alertID,
		"alert_type": "tank.feeder.safety_block_streak",
		"severity":   "critical",
		"status":     "open",
		"subject":    map[string]any{"kind": "tank", "id": tankID},
		"message":    msg,
		"evidence": map[string]any{
			"tank_id":       tankID,
			"cycle_id":      cycleID,
			"streak":        streak,
			"paused":        paused,
			"detected_at":   common.FormatTime(at),
			"resume_how_to": "대시보드 수조 자동/수동 토글 → 자동",
		},
		"raised_at":  common.FormatTime(at),
		"updated_at": common.FormatTime(at),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        alertID,
		AlertDedupeKey: "tank.feeder.safety_block_streak:" + tankID,
		AlertType:      "tank.feeder.safety_block_streak",
		Severity:       "critical",
		SubjectKind:    "tank",
		SubjectID:      tankID,
		Status:         "open",
		RaisedAt:       at,
		UpdatedAt:      at,
		PayloadJSON:    string(body),
	}
	if _, err := w.store.UpsertAlert(ctx, row); err != nil {
		w.log.Warn("safety escalation: upsert alert failed", "tank_id", tankID, "error", err)
	}
	if w.app != nil {
		_, _ = w.app.AppendEvent(ctx, "feed_cycle", "", tankID, "alert.raised", alertID, payload)
	}
	w.log.Error("급이 safety_block 연속 — 일시정지 + critical 알림",
		"tank_id", tankID, "streak", streak, "paused", paused)
}

// pauseTankFeeding — feeding_policy_override.operating_mode=manual (확립된 정지 경로).
// 반환: (이번에 정지시켰는지, 에러). 이미 manual 이면 (false, nil).
func (w *Worker) pauseTankFeeding(ctx context.Context, tankID string) (bool, error) {
	profile, err := w.store.GetTankProfile(ctx, tankID)
	if err != nil {
		return false, err
	}
	if profile == nil {
		return false, fmt.Errorf("tank profile not found: %s", tankID)
	}
	if profile.Metadata == nil {
		profile.Metadata = map[string]any{}
	}
	override, _ := profile.Metadata["feeding_policy_override"].(map[string]any)
	if override == nil {
		override = map[string]any{}
	}
	if mode, _ := override["operating_mode"].(string); mode == "manual" {
		return false, nil
	}
	override["operating_mode"] = "manual"
	profile.Metadata["feeding_policy_override"] = override
	if err := w.store.UpsertTankProfile(ctx, profile); err != nil {
		return false, err
	}
	return true, nil
}
