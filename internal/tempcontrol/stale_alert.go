package tempcontrol

// 수온 데이터 이상 알림 (2026-08-28 운영자 지시).
//
// 배경: 8-28 10:37~13:11 저쪽 API 인증서가 만료돼 센서 폴링이 155회 전부 실패했다.
// 그동안 수온 데이터가 한 건도 안 들어왔고, 온도제어는 판단 근거가 없어 아무 동작도
// 하지 않았다. 그 결과 10:24 에 켠 냉각기가 2시간 49분 동안 켜진 채 남아 수온이
// 목표 12.9℃ 를 0.95℃ 밑도는 11.95℃ 까지 내려갔다.
//
// 같은 시각 급이는 막혔다 — arbiter 안전 게이트가 sensor_stale 로 사이클 3건을
// 거부했다. 막는 장치가 냉각기 쪽에만 없었다.
//
// **냉각기 상태는 건드리지 않는다** (운영자 결정): "신호가 끊어지면 어차피 대응이
// 불가능하니까" — 자동 OFF 는 여름철 급상승 국면에서 오히려 위험할 수 있다.
// 여기서 하는 일은 운영자가 알아차리게 하는 것뿐이다.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

const staleAlertDedupeKey = "site.water_temperature.stale"

// checkTempStaleness — 수온 관측이 오래됐거나 아예 없으면 알림을 올리고, 회복되면 내린다.
//
// age 는 관측 시각(observed_at) 기준이다. 수집이 멎어도 마지막 값은 테이블에 남아
// 있으므로 값의 존재만으로는 단절을 알 수 없다 — 시각을 봐야 한다.
func (w *Worker) checkTempStaleness(ctx context.Context, observedAt string, haveReading bool) {
	cutoff := w.staleCutoff()
	stale, detail := false, ""

	switch {
	case !haveReading:
		stale, detail = true, "수온 관측값 없음"
	default:
		t, err := time.Parse(time.RFC3339, observedAt)
		if err != nil {
			stale, detail = true, fmt.Sprintf("관측 시각 해석 불가(%s)", observedAt)
			break
		}
		if age := time.Since(t); age > cutoff {
			stale, detail = true, fmt.Sprintf("마지막 관측 %d초 전(임계 %d초)",
				int(age.Seconds()), int(cutoff.Seconds()))
		}
	}

	if !stale {
		// 회복 — 열려 있던 알림을 내린다. 없으면 no-op.
		if err := w.store.ClearAlert(ctx, staleAlertDedupeKey); err != nil {
			w.log.Warn("temp stale alert: clear failed", "error", err)
		}
		return
	}
	w.raiseTempStaleAlert(ctx, detail)
}

func (w *Worker) staleCutoff() time.Duration {
	if w.staleSec > 0 {
		return time.Duration(w.staleSec) * time.Second
	}
	return 300 * time.Second // arbiter 안전 게이트 기본값과 같은 5분
}

// raiseTempStaleAlert — dedupe key 가 하나라 단절이 길어도 알림은 하나로 모인다.
func (w *Worker) raiseTempStaleAlert(ctx context.Context, detail string) {
	at := common.NowUTC()
	alertID := common.NewID("alert")
	msg := fmt.Sprintf("수온 데이터 이상 — %s. 온도제어가 판단을 못 하는 동안 "+
		"냉각기는 마지막 상태 그대로 유지됩니다. 수온을 직접 확인하세요.", detail)
	payload := map[string]any{
		"alert_id":   alertID,
		"alert_type": "site.water_temperature.stale",
		"severity":   "critical",
		"status":     "open",
		"subject":    map[string]any{"kind": "site", "id": "site"},
		"message":    msg,
		"evidence": map[string]any{
			"detail":      detail,
			"detected_at": common.FormatTime(at),
			"note":        "냉각기 상태는 자동 변경하지 않음 (2026-08-28 운영자 결정)",
		},
		"raised_at":  common.FormatTime(at),
		"updated_at": common.FormatTime(at),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        alertID,
		AlertDedupeKey: staleAlertDedupeKey,
		AlertType:      "site.water_temperature.stale",
		Severity:       "critical",
		SubjectKind:    "site",
		SubjectID:      "site",
		Status:         "open",
		RaisedAt:       at,
		UpdatedAt:      at,
		PayloadJSON:    string(body),
	}
	created, err := w.store.UpsertAlert(ctx, row)
	if err != nil {
		w.log.Warn("temp stale alert: upsert failed", "error", err)
		return
	}
	if created {
		// 재알림은 조용히 갱신만 — 5초 tick 마다 ERROR 로그가 쌓이면 다른 신호를 덮는다.
		w.log.Error("수온 데이터 이상 — 온도제어 판단 불가", "detail", detail)
	}
}
