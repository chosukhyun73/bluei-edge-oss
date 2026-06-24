package storage

import (
	"context"
	"database/sql"
	"encoding/json"

	"bluei.kr/edge/internal/sensor"
)

// UpsertSensor inserts or updates a sensor row.
// C-13a — model_id / mount_location / installed_depth_m / measurement_role_json /
// calibration_last_at / calibration_due_at 6 컬럼 추가.
func (s *sqliteStore) UpsertSensor(ctx context.Context, sen *sensor.Sensor) error {
	capsJSON, err := json.Marshal(sen.Capabilities)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(sen.Metadata)
	if err != nil {
		return err
	}
	roleJSON, err := json.Marshal(sen.MeasurementRole)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sensors(sensor_id,sensor_type,site_id,tank_id,wtg_id,position,hardware,capabilities_json,metadata_json,model_id,mount_location,installed_depth_m,measurement_role_json,calibration_last_at,calibration_due_at,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(sensor_id) DO UPDATE SET
           sensor_type=excluded.sensor_type,
           site_id=excluded.site_id,
           tank_id=excluded.tank_id,
           wtg_id=excluded.wtg_id,
           position=excluded.position,
           hardware=excluded.hardware,
           capabilities_json=excluded.capabilities_json,
           metadata_json=excluded.metadata_json,
           model_id=excluded.model_id,
           mount_location=excluded.mount_location,
           installed_depth_m=excluded.installed_depth_m,
           measurement_role_json=excluded.measurement_role_json,
           calibration_last_at=excluded.calibration_last_at,
           calibration_due_at=excluded.calibration_due_at,
           updated_at=excluded.updated_at`,
		sen.SensorID,
		sen.SensorType,
		nullStr(sen.SiteID),
		nullStr(sen.TankID),
		nullStr(sen.WTGID),
		sen.Position,
		sen.Hardware,
		string(capsJSON),
		string(metaJSON),
		nullStr(sen.ModelID),
		nullStr(sen.MountLocation),
		ptrFloat(sen.InstalledDepthM),
		string(roleJSON),
		nullStr(sen.CalibrationLastAt),
		nullStr(sen.CalibrationDueAt),
		fmtNow(),
		fmtNow(),
	)
	return err
}

// DeleteSensor removes a sensor row.
func (s *sqliteStore) DeleteSensor(ctx context.Context, sensorID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sensors WHERE sensor_id=?`, sensorID)
	return err
}

// SensorExists — 존재 여부 (NOT_FOUND 분기용).
func (s *sqliteStore) SensorExists(ctx context.Context, sensorID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sensors WHERE sensor_id=?`, sensorID).Scan(&n)
	return n > 0, err
}

// ListSensors returns sensors, optionally filtered by tank_id, site_id, or wtg_id.
// C-13a — model_id / mount_location / installed_depth_m / measurement_role_json /
// calibration_last_at / calibration_due_at 6 컬럼 포함.
func (s *sqliteStore) ListSensors(ctx context.Context, tankID, siteID, wtgID string) ([]*sensor.Sensor, error) {
	q := `SELECT sensor_id,sensor_type,
	             COALESCE(site_id,''),COALESCE(tank_id,''),COALESCE(wtg_id,''),
	             COALESCE(position,''),COALESCE(hardware,''),
	             capabilities_json,metadata_json,
	             COALESCE(model_id,''),COALESCE(mount_location,''),
	             installed_depth_m,
	             COALESCE(measurement_role_json,'[]'),
	             COALESCE(calibration_last_at,''),COALESCE(calibration_due_at,'')
	        FROM sensors WHERE 1=1`
	args := []any{}
	if tankID != "" {
		q += ` AND tank_id=?`
		args = append(args, tankID)
	}
	if siteID != "" {
		q += ` AND site_id=?`
		args = append(args, siteID)
	}
	if wtgID != "" {
		q += ` AND wtg_id=?`
		args = append(args, wtgID)
	}
	q += ` ORDER BY sensor_id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*sensor.Sensor
	for rows.Next() {
		var sen sensor.Sensor
		var capsJSON, metaJSON, roleJSON string
		var depth sql.NullFloat64
		if err := rows.Scan(
			&sen.SensorID, &sen.SensorType,
			&sen.SiteID, &sen.TankID, &sen.WTGID,
			&sen.Position, &sen.Hardware,
			&capsJSON, &metaJSON,
			&sen.ModelID, &sen.MountLocation,
			&depth,
			&roleJSON,
			&sen.CalibrationLastAt, &sen.CalibrationDueAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(capsJSON), &sen.Capabilities)
		_ = json.Unmarshal([]byte(metaJSON), &sen.Metadata)
		_ = json.Unmarshal([]byte(roleJSON), &sen.MeasurementRole)
		if depth.Valid {
			v := depth.Float64
			sen.InstalledDepthM = &v
		}
		out = append(out, &sen)
	}
	return out, rows.Err()
}
