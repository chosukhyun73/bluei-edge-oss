package storage

import (
	"context"
	"encoding/json"

	"bluei.kr/edge/internal/actuator"
)

// UpsertActuator inserts or updates an actuator row.
func (s *sqliteStore) UpsertActuator(ctx context.Context, a *actuator.Actuator) error {
	posJSON, err := json.Marshal(a.PositionInTank)
	if err != nil {
		return err
	}
	capsJSON, err := json.Marshal(a.Capabilities)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(a.Metadata)
	if err != nil {
		return err
	}
	// C-13b 신규 컬럼 — safety_roles_json / alarm_thresholds_json 직렬화.
	safetyRolesJSON, err := json.Marshal(a.SafetyRoles)
	if err != nil {
		return err
	}
	alarmThresholdsJSON, err := json.Marshal(a.AlarmThresholds)
	if err != nil {
		return err
	}
	operatingMode := a.OperatingMode
	if operatingMode == "" {
		operatingMode = "auto"
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO actuators(device_id,device_type,site_id,tank_id,wtg_id,controller_id,model,rated_power_w,position_json,capabilities_json,metadata_json,created_at,updated_at,model_id,mount_location,safety_role_json,operating_mode,alarm_thresholds_json,last_maintenance_at,next_maintenance_due_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(device_id) DO UPDATE SET
           device_type=excluded.device_type,
           site_id=excluded.site_id,
           tank_id=excluded.tank_id,
           wtg_id=excluded.wtg_id,
           controller_id=excluded.controller_id,
           model=excluded.model,
           rated_power_w=excluded.rated_power_w,
           position_json=excluded.position_json,
           capabilities_json=excluded.capabilities_json,
           metadata_json=excluded.metadata_json,
           model_id=excluded.model_id,
           mount_location=excluded.mount_location,
           safety_role_json=excluded.safety_role_json,
           operating_mode=excluded.operating_mode,
           alarm_thresholds_json=excluded.alarm_thresholds_json,
           last_maintenance_at=excluded.last_maintenance_at,
           next_maintenance_due_at=excluded.next_maintenance_due_at,
           updated_at=excluded.updated_at`,
		a.DeviceID,
		a.DeviceType,
		nullStr(a.SiteID),
		nullStr(a.TankID),
		nullStr(a.WTGID),
		nullStr(a.ControllerID),
		a.Model,
		nullFloat(a.RatedPowerW),
		string(posJSON),
		string(capsJSON),
		string(metaJSON),
		fmtNow(),
		fmtNow(),
		nullStr(a.ModelID),
		nullStr(a.MountLocation),
		string(safetyRolesJSON),
		operatingMode,
		string(alarmThresholdsJSON),
		nullStr(a.LastMaintenanceAt),
		nullStr(a.NextMaintenanceDueAt),
	)
	return err
}

// DeleteActuator removes an actuator row.
func (s *sqliteStore) DeleteActuator(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM actuators WHERE device_id=?`, deviceID)
	return err
}

// ActuatorExists — 존재 여부.
func (s *sqliteStore) ActuatorExists(ctx context.Context, deviceID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actuators WHERE device_id=?`, deviceID).Scan(&n)
	return n > 0, err
}

// ListActuators returns actuators, optionally filtered by tank_id, site_id, or wtg_id.
// Non-empty filter values are ANDed together.
func (s *sqliteStore) ListActuators(ctx context.Context, tankID, siteID, wtgID string) ([]*actuator.Actuator, error) {
	q := `SELECT device_id,device_type,
	             COALESCE(site_id,''),COALESCE(tank_id,''),COALESCE(wtg_id,''),COALESCE(controller_id,''),
	             COALESCE(model,''),COALESCE(rated_power_w,0),
	             position_json,capabilities_json,metadata_json,
	             COALESCE(model_id,''),COALESCE(mount_location,''),
	             COALESCE(safety_role_json,'[]'),COALESCE(operating_mode,'auto'),
	             COALESCE(alarm_thresholds_json,'{}'),
	             COALESCE(last_maintenance_at,''),COALESCE(next_maintenance_due_at,'')
	        FROM actuators WHERE 1=1`
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
	q += ` ORDER BY device_id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*actuator.Actuator
	for rows.Next() {
		var a actuator.Actuator
		var posJSON, capsJSON, metaJSON string
		var safetyRolesJSON, alarmThresholdsJSON string
		if err := rows.Scan(
			&a.DeviceID, &a.DeviceType,
			&a.SiteID, &a.TankID, &a.WTGID, &a.ControllerID,
			&a.Model, &a.RatedPowerW,
			&posJSON, &capsJSON, &metaJSON,
			&a.ModelID, &a.MountLocation,
			&safetyRolesJSON, &a.OperatingMode,
			&alarmThresholdsJSON,
			&a.LastMaintenanceAt, &a.NextMaintenanceDueAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(capsJSON), &a.Capabilities)
		_ = json.Unmarshal([]byte(metaJSON), &a.Metadata)
		_ = json.Unmarshal([]byte(safetyRolesJSON), &a.SafetyRoles)
		_ = json.Unmarshal([]byte(alarmThresholdsJSON), &a.AlarmThresholds)
		var pos actuator.PositionInTank
		if err := json.Unmarshal([]byte(posJSON), &pos); err == nil {
			a.PositionInTank = &pos
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
