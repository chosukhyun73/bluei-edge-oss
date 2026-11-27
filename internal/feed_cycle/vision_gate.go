package feed_cycle

// 비전 게이트 (2026-08-25 운영자 지시).
//
// 카메라가 끊기면 급이를 시작하지 않고, 진행 중이면 그 순간부터 멈춘다.
// 근거는 운영자의 말 그대로다 — "카메라가 없다면 먹는지 안 먹는지도 모른 채 사료를
// 주는 것이고, 카메라가 끊어진다는 것은 눈이 사라지는 것".
//
// 판정 카메라 = 그 수조의 **상단** 카메라. 배출·중단 판정이 상단 곡선만 쓰기
// 때문이다(shadow_stop.go 의 segmentChartValue 가 수중 카메라를 건너뛴다).
// 수중 카메라는 LRCN 관찰·바닥 스틸용이라 끊겨도 "먹는지 안 먹는지"는 보인다.
//
// 판단 자체는 api 패키지(프레임허브를 쥐고 있는 쪽)가 하고, 여기서는 주입받는다.

import (
	"context"
	"encoding/json"
	"fmt"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

// VisionGate — 이 수조의 판정 카메라가 지금 프레임을 주고 있는가.
// ok=false 면 급이 차단. detail 은 사유(카메라 id · 경과 시간).
// 판정 불가(조회 실패 등)일 때는 ok=true 로 돌려준다 — 알 수 없다는 이유로
// 어체를 굶기지 않는다. 막는 것은 "끊긴 것이 확인된" 경우뿐이다.
type VisionGate func(tankID string) (ok bool, detail string)

// SetVisionGate wires the gate. nil 이면 게이트 미적용(종전 동작).
func (w *Worker) SetVisionGate(g VisionGate) { w.visionGate = g }

// visionBlocked — true 면 급이를 막아야 한다.
func (w *Worker) visionBlocked(tankID string) (bool, string) {
	if w.visionGate == nil {
		return false, ""
	}
	ok, detail := w.visionGate(tankID)
	if ok {
		return false, ""
	}
	if detail == "" {
		detail = "판정 카메라 프레임 없음"
	}
	return true, detail
}

// raiseVisionBlackoutAlert — 차단 사실을 운영자에게 남긴다.
//
// 조용히 막으면 "급이가 왜 안 나갔지"가 되므로 알림이 차단과 한 세트다.
// dedupe key 가 수조 단위라 카메라가 오래 끊겨도 알림은 수조당 하나로 모인다.
// 급이기 장애(safety_block)와 달리 수조를 manual 로 내리지 않는다 — 카메라가
// 돌아오면 다음 스케줄 사이클이 저절로 나가야 하기 때문이다.
func (w *Worker) raiseVisionBlackoutAlert(ctx context.Context, tankID, cycleID, detail string, pulsesDone int) {
	at := common.NowUTC()
	alertID := common.NewID("alert")
	msg := fmt.Sprintf("%s 판정 카메라 단절 — 급이를 중단했습니다 (%s). "+
		"카메라가 돌아오면 다음 스케줄부터 자동으로 재개됩니다.", tankID, detail)
	if cycleID == "" {
		msg = fmt.Sprintf("%s 판정 카메라 단절 — 급이를 시작하지 않았습니다 (%s). "+
			"카메라가 돌아오면 다음 스케줄부터 자동으로 재개됩니다.", tankID, detail)
	}
	payload := map[string]any{
		"alert_id":   alertID,
		"alert_type": "tank.vision.blackout",
		"severity":   "warning",
		"status":     "open",
		"subject":    map[string]any{"kind": "tank", "id": tankID},
		"message":    msg,
		"evidence": map[string]any{
			"tank_id":         tankID,
			"cycle_id":        cycleID,
			"detail":          detail,
			"pulses_executed": pulsesDone,
			"detected_at":     common.FormatTime(at),
		},
		"raised_at":  common.FormatTime(at),
		"updated_at": common.FormatTime(at),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        alertID,
		AlertDedupeKey: "tank.vision.blackout:" + tankID,
		AlertType:      "tank.vision.blackout",
		Severity:       "warning",
		SubjectKind:    "tank",
		SubjectID:      tankID,
		Status:         "open",
		RaisedAt:       at,
		UpdatedAt:      at,
		PayloadJSON:    string(body),
	}
	if _, err := w.store.UpsertAlert(ctx, row); err != nil {
		w.log.Warn("vision blackout: upsert alert failed", "tank_id", tankID, "error", err)
	}
	if w.app != nil {
		_, _ = w.app.AppendEvent(ctx, "feed_cycle", "", tankID, "alert.raised", alertID, payload)
	}
	w.log.Warn("급이 차단 — 판정 카메라 단절",
		"tank_id", tankID, "cycle_id", cycleID, "detail", detail, "pulses_executed", pulsesDone)
}
