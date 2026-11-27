package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AggregateSensorReadingsDailyBefore groups raw sensor.reading.recorded events by
// local calendar day + sensor_id + metric, returning min/max/avg/count, for every
// day that has a reading with recorded_at < before (the rows retention will prune).
// The day is payload.observed_at (fallback recorded_at) shifted by tzOffsetSeconds.
func (s *sqliteStore) AggregateSensorReadingsDailyBefore(ctx context.Context, before time.Time, tzOffsetSeconds int) ([]SensorDailyAgg, error) {
	// SQLite date() with a '+N seconds' modifier shifts UTC timestamps into local time.
	// 날짜는 측정 시각(payload.observed_at) 기준, 없으면 recorded_at (2026-11-18).
	// 서버에서 받아 온 과거 기록은 recorded_at 이 받은 날이라, recorded_at 으로 묶으면
	// 1년 뒤 몇 년치가 "받은 날" 하루로 뭉친다.
	// 대상 = 이번에 지워질 raw(recorded_at < before)가 하나라도 있는 측정일. 그날 값은
	// recorded_at 과 무관하게 전부 넣는다 — 한 측정일이 이틀에 걸쳐 들어와도 요약이 온전하다
	// (요약 ID 가 날짜+센서+메트릭이라 처음 만든 요약이 그대로 남기 때문).
	shift := fmt.Sprintf("%+d seconds", tzOffsetSeconds)
	rows, err := s.db.QueryContext(ctx, `
		WITH r AS (
		  SELECT date(COALESCE(json_extract(payload_json,'$.observed_at'), recorded_at), ?) AS d,
		         recorded_at, payload_json
		  FROM events
		  WHERE event_type='sensor.reading.recorded'
		    AND json_extract(payload_json,'$.value') IS NOT NULL
		)
		SELECT d,
		       json_extract(payload_json,'$.sensor_id')           AS sensor_id,
		       COALESCE(json_extract(payload_json,'$.device_id'),'') AS device_id,
		       json_extract(payload_json,'$.metric')              AS metric,
		       COALESCE(json_extract(payload_json,'$.unit'),'')   AS unit,
		       COUNT(*)                                            AS n,
		       MIN(CAST(json_extract(payload_json,'$.value') AS REAL)) AS mn,
		       MAX(CAST(json_extract(payload_json,'$.value') AS REAL)) AS mx,
		       AVG(CAST(json_extract(payload_json,'$.value') AS REAL)) AS av
		FROM r
		WHERE d IN (SELECT d FROM r WHERE recorded_at < ?)
		GROUP BY d, sensor_id, metric
		ORDER BY d, sensor_id, metric`,
		shift, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("aggregate sensor daily: %w", err)
	}
	defer rows.Close()

	var out []SensorDailyAgg
	for rows.Next() {
		var a SensorDailyAgg
		if err := rows.Scan(&a.Date, &a.SensorID, &a.DeviceID, &a.Metric, &a.Unit,
			&a.Count, &a.Min, &a.Max, &a.Avg); err != nil {
			return nil, fmt.Errorf("scan agg: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FilterExistingEventIDs returns the subset of ids that already exist in events.
func (s *sqliteStore) FilterExistingEventIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	existing := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return existing, nil
	}
	// Chunk to stay under SQLite's variable limit.
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		ph := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx,
			`SELECT event_id FROM events WHERE event_id IN (`+ph+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("filter existing ids: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			existing[id] = true
		}
		rows.Close()
	}
	return existing, nil
}
