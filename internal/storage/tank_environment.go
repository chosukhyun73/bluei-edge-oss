package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *sqliteStore) UpsertTankEnvironmentReading(ctx context.Context, reading *CurrentTankEnvironmentReading, payloadJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_tank_environment(tank_id,metric,value,unit,quality,sensor_id,device_id,last_event_id,observed_at,updated_at,payload_json)
         VALUES(?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(tank_id, metric) DO UPDATE SET
           value=excluded.value,
           unit=excluded.unit,
           quality=excluded.quality,
           sensor_id=excluded.sensor_id,
           device_id=excluded.device_id,
           last_event_id=excluded.last_event_id,
           observed_at=excluded.observed_at,
           updated_at=excluded.updated_at,
           payload_json=excluded.payload_json`,
		reading.TankID,
		reading.Metric,
		reading.Value,
		reading.Unit,
		reading.Quality,
		reading.SensorID,
		reading.DeviceID,
		reading.LastEventID,
		reading.ObservedAt,
		fmtNow(),
		payloadJSON,
	)
	return err
}

func (s *sqliteStore) ListTankEnvironment(ctx context.Context, tankID string) ([]*CurrentTankEnvironmentReading, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tank_id,metric,value,unit,quality,sensor_id,device_id,last_event_id,observed_at,updated_at,payload_json
         FROM current_tank_environment
         WHERE tank_id=?
         ORDER BY metric`,
		tankID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*CurrentTankEnvironmentReading, 0)
	for rows.Next() {
		reading := &CurrentTankEnvironmentReading{}
		var value sql.NullFloat64
		var payloadJSON string
		if err := rows.Scan(&reading.TankID, &reading.Metric, &value, &reading.Unit, &reading.Quality, &reading.SensorID, &reading.DeviceID, &reading.LastEventID, &reading.ObservedAt, &reading.UpdatedAt, &payloadJSON); err != nil {
			return nil, err
		}
		if value.Valid {
			reading.Value = &value.Float64
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err == nil {
			reading.Payload = payload
		} else {
			reading.Payload = map[string]any{}
		}
		out = append(out, reading)
	}
	return out, rows.Err()
}
