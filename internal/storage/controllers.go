package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"bluei.kr/edge/internal/controller"
)

// UpsertController inserts or updates a controller row.
func (s *sqliteStore) UpsertController(ctx context.Context, c *controller.Controller) error {
	commJSON, err := json.Marshal(c.Commissioning)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO controllers(controller_id,tank_id,site_id,actuator_id,mac_address,ip_address,firmware_version,status,registered_at,last_seen_at,commissioning_json,metadata_json,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(controller_id) DO UPDATE SET
           tank_id=excluded.tank_id,
           site_id=excluded.site_id,
           actuator_id=excluded.actuator_id,
           mac_address=excluded.mac_address,
           ip_address=excluded.ip_address,
           firmware_version=excluded.firmware_version,
           status=excluded.status,
           registered_at=excluded.registered_at,
           last_seen_at=excluded.last_seen_at,
           commissioning_json=excluded.commissioning_json,
           metadata_json=excluded.metadata_json,
           updated_at=excluded.updated_at`,
		c.ControllerID,
		nullStr(c.TankID),
		nullStr(c.SiteID),
		nullStr(c.ActuatorID),
		c.MACAddress,
		nullStr(c.IPAddress),
		c.FirmwareVersion,
		string(c.Status),
		c.RegisteredAt,
		nullStr(c.LastSeenAt),
		string(commJSON),
		string(metaJSON),
		fmtNow(),
	)
	return err
}

// scanController scans one controllers row into a Controller struct.
func scanController(row interface {
	Scan(...any) error
}) (*controller.Controller, error) {
	var c controller.Controller
	var tankID, siteID, actuatorID, ipAddress, lastSeenAt sql.NullString
	var commJSON, metaJSON string
	err := row.Scan(
		&c.ControllerID, &tankID, &siteID, &actuatorID,
		&c.MACAddress, &ipAddress, &c.FirmwareVersion,
		&c.Status, &c.RegisteredAt, &lastSeenAt,
		&commJSON, &metaJSON,
	)
	if err != nil {
		return nil, err
	}
	c.TankID = tankID.String
	c.SiteID = siteID.String
	c.ActuatorID = actuatorID.String
	c.IPAddress = ipAddress.String
	c.LastSeenAt = lastSeenAt.String
	_ = json.Unmarshal([]byte(commJSON), &c.Commissioning)
	_ = json.Unmarshal([]byte(metaJSON), &c.Metadata)
	return &c, nil
}

const controllerSelectCols = `controller_id,tank_id,site_id,actuator_id,mac_address,ip_address,firmware_version,status,registered_at,last_seen_at,commissioning_json,metadata_json`

// GetControllerByMAC returns the controller with the given MAC address, or nil if not found.
func (s *sqliteStore) GetControllerByMAC(ctx context.Context, mac string) (*controller.Controller, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+controllerSelectCols+` FROM controllers WHERE mac_address=?`, mac)
	c, err := scanController(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// GetController returns the controller with the given controller_id, or nil if not found.
func (s *sqliteStore) GetController(ctx context.Context, controllerID string) (*controller.Controller, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+controllerSelectCols+` FROM controllers WHERE controller_id=?`, controllerID)
	c, err := scanController(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// DeleteController removes a controller row by controller_id.
func (s *sqliteStore) DeleteController(ctx context.Context, controllerID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM controllers WHERE controller_id=?`, controllerID)
	return err
}

// CountActuatorsForController — 컨트롤러에 연결된 액추에이터 수 (FK 의존성).
func (s *sqliteStore) CountActuatorsForController(ctx context.Context, controllerID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actuators WHERE controller_id=?`, controllerID).Scan(&n)
	return n, err
}

// ListControllers returns all controllers, optionally filtered by status (case-sensitive lowercase).
func (s *sqliteStore) ListControllers(ctx context.Context, status string) ([]*controller.Controller, error) {
	q := `SELECT ` + controllerSelectCols + ` FROM controllers`
	args := []any{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY registered_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*controller.Controller
	for rows.Next() {
		c, err := scanController(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
