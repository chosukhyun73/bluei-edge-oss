package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AggregateSensorReadingsDailyBefore groups raw sensor.reading.recorded events
// (recorded_at < before) by local calendar day + sensor_id + metric, returning
// min/max/avg/count. The local day is recorded_at shifted by tzOffsetSeconds.
// Only complete days appear because `before` is a day boundary chosen by the caller.
func (s *sqliteStore) AggregateSensorReadingsDailyBefore(ctx context.Context, before time.Time, tzOffsetSeconds int) ([]SensorDailyAgg, error) {
	// SQLite date() with a '+N seconds' modifier shifts UTC recorded_at into local time.
	shift := fmt.Sprintf("%+d seconds", tzOffsetSeconds)
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(recorded_at, ?)                                AS d,
		       json_extract(payload_json,'$.sensor_id')           AS sensor_id,
		       COALESCE(json_extract(payload_json,'$.device_id'),'') AS device_id,
		       json_extract(payload_json,'$.metric')              AS metric,
		       COALESCE(json_extract(payload_json,'$.unit'),'')   AS unit,
		       COUNT(*)                                            AS n,
		       MIN(CAST(json_extract(payload_json,'$.value') AS REAL)) AS mn,
		       MAX(CAST(json_extract(payload_json,'$.value') AS REAL)) AS mx,
		       AVG(CAST(json_extract(payload_json,'$.value') AS REAL)) AS av
		FROM events
		WHERE event_type='sensor.reading.recorded'
		  AND recorded_at < ?
		  AND json_extract(payload_json,'$.value') IS NOT NULL
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
