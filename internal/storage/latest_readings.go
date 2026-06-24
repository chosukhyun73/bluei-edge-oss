package storage

import (
	"context"
	"encoding/json"
	"fmt"
)

// LatestSensorReadings returns the newest sensor.reading.recorded payloads for the
// requested scope. When tank_id is supplied, results are de-duplicated by metric so
// tank state can show one current value per environmental/operational metric. For
// global/sensor scopes, results are de-duplicated by sensor_id+metric.
func (s *sqliteStore) LatestSensorReadings(ctx context.Context, f LatestReadingFilter) ([]*LatestSensorReading, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	scanLimit := f.MaxScan
	if scanLimit <= 0 {
		scanLimit = 5000
	}
	if scanLimit < limit {
		scanLimit = limit
	}

	events, err := s.QueryEvents(ctx, EventFilter{
		EventType: "sensor.reading.recorded",
		Limit:     scanLimit,
	})
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	out := make([]*LatestSensorReading, 0, limit)
	for _, e := range events {
		reading, err := latestReadingFromEvent(e)
		if err != nil {
			continue
		}
		if f.SensorID != "" && reading.SensorID != f.SensorID {
			continue
		}
		if f.Metric != "" && reading.Metric != f.Metric {
			continue
		}
		if f.TankID != "" && locationString(reading.Location, "tank_id") != f.TankID {
			continue
		}

		key := reading.SensorID + "|" + reading.Metric
		if f.TankID != "" {
			key = reading.Metric
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, reading)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func latestReadingFromEvent(e *Event) (*LatestSensorReading, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}
	loc, _ := payload["location"].(map[string]any)
	reading := &LatestSensorReading{
		Sequence:   e.Sequence,
		EventID:    e.EventID,
		SensorID:   stringFromAny(payload["sensor_id"]),
		DeviceID:   stringFromAny(payload["device_id"]),
		Metric:     stringFromAny(payload["metric"]),
		Value:      floatPtrFromAny(payload["value"]),
		Unit:       stringFromAny(payload["unit"]),
		Quality:    stringFromAny(payload["quality"]),
		ObservedAt: stringFromAny(payload["observed_at"]),
		Location:   loc,
		Payload:    payload,
	}
	return reading, nil
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func floatPtrFromAny(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case nil:
		return nil
	default:
		return nil
	}
}

func locationString(location map[string]any, key string) string {
	if location == nil {
		return ""
	}
	return stringFromAny(location[key])
}
