package tempcontrol

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 냉각 역부족 감지 (2026-10-01).
//
// 10-01 새벽 실측: 00:47~03:26 냉각기가 2시간 39분 연속 가동했는데 수온은 9.4 → 12.1℃
// 로 오히려 2.7℃ 올랐다. 외부(취수) 열부하가 냉각 능력을 넘어선 것이다. 그동안 아무
// 경보가 없었고 전력만 소모했다. 운영자는 아침에야 수온 이상을 알았다.
//
// 냉각기가 "돌고 있다"와 "효과가 있다"는 다르다 — 후자를 보는 감시가 없었다.
const (
	shortfallWindow   = 60 * time.Minute // 평가 창
	shortfallMinOnPct = 0.75             // 창의 75% 이상 가동 중이었어야 판단한다
	shortfallRiseC    = 0.5              // 그런데도 이만큼 올랐으면 역부족
	shortfallKey      = "chiller.cooling_shortfall"
)

// checkCoolingShortfall — 냉각기가 계속 도는데 수온이 오르면 알림. 해소되면 자동 종료.
func (w *Worker) checkCoolingShortfall(ctx context.Context, tempNow float64, chillerOn bool) {
	onPct, riseC, ok := w.coolingWindowStats(ctx, tempNow)
	if !ok {
		return
	}
	shortfall := chillerOn && onPct >= shortfallMinOnPct && riseC >= shortfallRiseC
	if !shortfall {
		// 역부족이 해소되면 열린 알림을 닫는다(없으면 무해).
		_ = w.store.ClearAlert(ctx, shortfallKey)
		return
	}

	at := common.NowUTC()
	payload := map[string]any{
		"alert_id":   common.NewID("alert"),
		"alert_type": "chiller.cooling_shortfall",
		"severity":   "warning",
		"status":     "open",
		"message": "냉각기가 가동 중인데 수온이 계속 오릅니다 — 외부 열부하가 냉각 능력을 넘었습니다. " +
			"취수 수온 확인이 필요합니다.",
		"subject": map[string]any{"kind": "device", "id": chillerDeviceID},
		"evidence": map[string]any{
			"window_min":     int(shortfallWindow.Minutes()),
			"chiller_on_pct": math.Round(onPct*100) / 100,
			"temp_rise_c":    math.Round(riseC*100) / 100,
			"temp_now_c":     math.Round(tempNow*100) / 100,
			"detected_at":    common.FormatTime(at),
		},
		"raised_at":  common.FormatTime(at),
		"updated_at": common.FormatTime(at),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        common.NewID("alert"),
		AlertDedupeKey: shortfallKey,
		AlertType:      "chiller.cooling_shortfall",
		Severity:       "warning",
		SubjectKind:    "device",
		SubjectID:      chillerDeviceID,
		Status:         "open",
		RaisedAt:       at,
		UpdatedAt:      at,
		PayloadJSON:    string(body),
	}
	if _, err := w.store.UpsertAlert(ctx, row); err != nil {
		w.log.Warn("upsert cooling-shortfall alert failed", "error", err)
		return
	}
	w.log.Warn("냉각 역부족 — 가동 중에도 수온 상승",
		"window_min", int(shortfallWindow.Minutes()),
		"on_pct", math.Round(onPct*100)/100, "rise_c", math.Round(riseC*100)/100)
}

// coolingWindowStats — 최근 창에서 (냉각기 가동 비율, 수온 상승폭, 유효 여부).
func (w *Worker) coolingWindowStats(ctx context.Context, tempNow float64) (onPct, riseC float64, ok bool) {
	since := time.Now().UTC().Add(-shortfallWindow)
	evs, err := w.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		Since:     &since,
		Limit:     1000,
	})
	if err != nil || len(evs) == 0 {
		return 0, 0, false
	}
	var onN, totalN int
	var oldestTemp float64
	var haveTemp bool
	// QueryEvents 는 최신순이므로 뒤로 갈수록 과거 — 마지막으로 만난 수온이 창의 시작값.
	for _, e := range evs {
		var p struct {
			Metric string   `json:"metric"`
			Value  *float64 `json:"value"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.Value == nil {
			continue
		}
		switch p.Metric {
		case "chiller_status":
			totalN++
			if *p.Value > 0 {
				onN++
			}
		case events.MetricWaterTemperature:
			oldestTemp, haveTemp = *p.Value, true
		}
	}
	if totalN == 0 || !haveTemp {
		return 0, 0, false
	}
	return float64(onN) / float64(totalN), tempNow - oldestTemp, true
}
