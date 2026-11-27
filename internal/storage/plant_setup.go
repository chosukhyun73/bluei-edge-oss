package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrPlantIDTaken — 같은 id 가 다른 사업장에 이미 있다.
var ErrPlantIDTaken = errors.New("id is already used by another site")

// upserted turns "no row written" into ErrPlantIDTaken: the upserts below only
// update a row that belongs to the same site.
func upserted(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPlantIDTaken
	}
	return nil
}

// Plant setup: profile, zones, equipment, process sensors, local users. docs/54.

func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (s *sqliteStore) UpsertPlantProfile(ctx context.Context, p *PlantProfile) error {
	now := fmtNow()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO plant_profiles(site_id,business_no,license_name,license_no,license_authority,haccp_cert_no,
		   human_welfare_policy,retention_years,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(site_id) DO UPDATE SET
		   business_no=excluded.business_no, license_name=excluded.license_name, license_no=excluded.license_no,
		   license_authority=excluded.license_authority, haccp_cert_no=excluded.haccp_cert_no,
		   human_welfare_policy=excluded.human_welfare_policy, retention_years=excluded.retention_years,
		   updated_at=excluded.updated_at`,
		p.SiteID, p.BusinessNo, p.LicenseName, p.LicenseNo, p.LicenseAuthority, p.HACCPCertNo,
		p.HumanWelfarePolicy, p.RetentionYears, now, now)
	return err
}

func (s *sqliteStore) GetPlantProfile(ctx context.Context, siteID string) (*PlantProfile, error) {
	p := &PlantProfile{}
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT site_id,business_no,license_name,license_no,license_authority,haccp_cert_no,
		        human_welfare_policy,retention_years,created_at,updated_at
		   FROM plant_profiles WHERE site_id=?`, siteID).Scan(
		&p.SiteID, &p.BusinessNo, &p.LicenseName, &p.LicenseNo, &p.LicenseAuthority, &p.HACCPCertNo,
		&p.HumanWelfarePolicy, &p.RetentionYears, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = parseRFC3339(createdAt), parseRFC3339(updatedAt)
	return p, nil
}

func (s *sqliteStore) UpsertPlantZone(ctx context.Context, z *PlantZone) error {
	now := fmtNow()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO plant_zones(zone_id,site_id,name,hygiene_class,temp_class,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(zone_id) DO UPDATE SET
		   name=excluded.name, hygiene_class=excluded.hygiene_class, temp_class=excluded.temp_class,
		   updated_at=excluded.updated_at
		 WHERE plant_zones.site_id=excluded.site_id`,
		z.ZoneID, z.SiteID, z.Name, z.HygieneClass, z.TempClass, now, now)
	return upserted(res, err)
}

func (s *sqliteStore) ListPlantZones(ctx context.Context, siteID string) ([]*PlantZone, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT zone_id,site_id,name,hygiene_class,temp_class,created_at,updated_at
		   FROM plant_zones WHERE site_id=? ORDER BY created_at, zone_id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlantZone
	for rows.Next() {
		z := &PlantZone{}
		var createdAt, updatedAt string
		if err := rows.Scan(&z.ZoneID, &z.SiteID, &z.Name, &z.HygieneClass, &z.TempClass, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		z.CreatedAt, z.UpdatedAt = parseRFC3339(createdAt), parseRFC3339(updatedAt)
		out = append(out, z)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertPlantEquipment(ctx context.Context, e *PlantEquipment) error {
	now := fmtNow()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO plant_equipment(equipment_id,site_id,zone_id,kind,name,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(equipment_id) DO UPDATE SET
		   zone_id=excluded.zone_id, kind=excluded.kind, name=excluded.name, updated_at=excluded.updated_at
		 WHERE plant_equipment.site_id=excluded.site_id`,
		e.EquipmentID, e.SiteID, e.ZoneID, e.Kind, e.Name, now, now)
	return upserted(res, err)
}

func (s *sqliteStore) ListPlantEquipment(ctx context.Context, siteID string) ([]*PlantEquipment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT equipment_id,site_id,zone_id,kind,name,created_at,updated_at
		   FROM plant_equipment WHERE site_id=? ORDER BY created_at, equipment_id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlantEquipment
	for rows.Next() {
		e := &PlantEquipment{}
		var createdAt, updatedAt string
		if err := rows.Scan(&e.EquipmentID, &e.SiteID, &e.ZoneID, &e.Kind, &e.Name, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt, e.UpdatedAt = parseRFC3339(createdAt), parseRFC3339(updatedAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertPlantSensor(ctx context.Context, sn *PlantSensor) error {
	now := fmtNow()
	mock := 0
	if sn.IsMock {
		mock = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO plant_sensors(sensor_id,site_id,zone_id,equipment_id,metric,unit,is_mock,calibration_due_at,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(sensor_id) DO UPDATE SET
		   zone_id=excluded.zone_id, equipment_id=excluded.equipment_id, metric=excluded.metric, unit=excluded.unit,
		   is_mock=excluded.is_mock, calibration_due_at=excluded.calibration_due_at, updated_at=excluded.updated_at
		 WHERE plant_sensors.site_id=excluded.site_id`,
		sn.SensorID, sn.SiteID, sn.ZoneID, sn.EquipmentID, sn.Metric, sn.Unit, mock,
		fmtTimePtr(sn.CalibrationDueAt), now, now)
	return upserted(res, err)
}

func (s *sqliteStore) ListPlantSensors(ctx context.Context, siteID string) ([]*PlantSensor, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sensor_id,site_id,zone_id,equipment_id,metric,unit,is_mock,calibration_due_at,created_at,updated_at
		   FROM plant_sensors WHERE site_id=? ORDER BY created_at, sensor_id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlantSensor
	for rows.Next() {
		sn := &PlantSensor{}
		var mock int
		var due *string
		var createdAt, updatedAt string
		if err := rows.Scan(&sn.SensorID, &sn.SiteID, &sn.ZoneID, &sn.EquipmentID, &sn.Metric, &sn.Unit,
			&mock, &due, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		sn.IsMock = mock != 0
		if due != nil {
			t := parseRFC3339(*due)
			sn.CalibrationDueAt = &t
		}
		sn.CreatedAt, sn.UpdatedAt = parseRFC3339(createdAt), parseRFC3339(updatedAt)
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *sqliteStore) InsertLocalUser(ctx context.Context, u *LocalUser) error {
	now := fmtNow()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO local_users(user_id,site_id,name,role,pin_salt,pin_hash,active,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,1,?,?)`,
		u.UserID, u.SiteID, u.Name, u.Role, u.PinSalt, u.PinHash, now, now)
	return err
}

const localUserCols = `user_id,site_id,name,role,pin_salt,pin_hash,active,created_at,updated_at`

func scanLocalUser(sc rowScanner) (*LocalUser, error) {
	u := &LocalUser{}
	var active int
	var createdAt, updatedAt string
	if err := sc.Scan(&u.UserID, &u.SiteID, &u.Name, &u.Role, &u.PinSalt, &u.PinHash, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	u.Active = active != 0
	u.CreatedAt, u.UpdatedAt = parseRFC3339(createdAt), parseRFC3339(updatedAt)
	return u, nil
}

func (s *sqliteStore) GetLocalUser(ctx context.Context, userID string) (*LocalUser, error) {
	u, err := scanLocalUser(s.db.QueryRowContext(ctx, `SELECT `+localUserCols+` FROM local_users WHERE user_id=?`, userID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (s *sqliteStore) ListLocalUsers(ctx context.Context, siteID string) ([]*LocalUser, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+localUserCols+` FROM local_users WHERE site_id=? ORDER BY created_at, user_id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*LocalUser
	for rows.Next() {
		u, err := scanLocalUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
