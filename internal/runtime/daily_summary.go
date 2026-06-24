package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SensorDailySummaryEventType — 센서 raw 가 pruned 되기 전 남기는 일별 요약 이벤트.
// retention 룰 대상이 아니므로 영구 보존되고, 소량이라 sync 에도 그대로 합류한다.
const SensorDailySummaryEventType = "sensor.reading.daily_summary"

// SummarizeSensorReadingsDaily aggregates raw sensor.reading.recorded events
// recorded before `before` into one daily min/max/avg summary per (day, sensor,
// metric), emitting a SensorDailySummaryEventType event for each. Idempotent:
// summaries that already exist (deterministic event_id) are skipped, so it is
// safe to re-run before the raw rows are pruned. Returns the number emitted.
func (a *App) SummarizeSensorReadingsDaily(ctx context.Context, before time.Time, loc *time.Location) (int, error) {
	if loc == nil {
		loc = time.UTC
	}
	_, offset := before.In(loc).Zone()

	aggs, err := a.Store.AggregateSensorReadingsDailyBefore(ctx, before, offset)
	if err != nil {
		return 0, err
	}
	if len(aggs) == 0 {
		return 0, nil
	}

	ids := make([]string, len(aggs))
	for i, ag := range aggs {
		ids[i] = dailySummaryEventID(ag.Date, ag.SensorID, ag.Metric)
	}
	existing, err := a.Store.FilterExistingEventIDs(ctx, ids)
	if err != nil {
		return 0, err
	}

	// sensorID → tankID, so the synced summary carries tank_id (the backend
	// resolves platform_tank_id from it via gx10_tank_bindings; summaries with no
	// tank_id are unresolvable). See GX10_SYNC_CONTRACT_V1 §4.1/§6.
	sensorTank := map[string]string{}
	if sensors, serr := a.Store.ListSensors(ctx, "", "", ""); serr == nil {
		for _, sen := range sensors {
			if sen != nil && sen.TankID != "" {
				sensorTank[sen.SensorID] = sen.TankID
			}
		}
	} else {
		slog.Warn("daily summary: list sensors for tank mapping failed", "error", serr)
	}

	emitted := 0
	for i, ag := range aggs {
		id := ids[i]
		if existing[id] {
			continue
		}
		payload := map[string]any{
			"date":      ag.Date,
			"sensor_id": ag.SensorID,
			"device_id": ag.DeviceID,
			"metric":    ag.Metric,
			"unit":      ag.Unit,
			"count":     ag.Count,
			"min":       ag.Min,
			"max":       ag.Max,
			"avg":       ag.Avg,
			"timezone":  loc.String(),
		}
		if tankID := sensorTank[ag.SensorID]; tankID != "" {
			payload["tank_id"] = tankID
		}
		if _, err := a.AppendEventWithID(ctx, id, "retention", "", ag.DeviceID,
			SensorDailySummaryEventType, "", payload); err != nil {
			return emitted, fmt.Errorf("emit daily summary %s: %w", id, err)
		}
		emitted++
	}
	return emitted, nil
}

// dailySummaryEventID — 결정적 ID 로 멱등성 보장 (날짜+센서+메트릭당 1건).
func dailySummaryEventID(date, sensorID, metric string) string {
	return fmt.Sprintf("dsum-%s-%s-%s", date, sensorID, metric)
}
