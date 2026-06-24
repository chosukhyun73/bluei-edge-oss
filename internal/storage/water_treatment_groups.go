package storage

import (
	"context"
	"encoding/json"

	"bluei.kr/edge/internal/wtg"
)

// UpsertWTG inserts or updates a water treatment group row.
func (s *sqliteStore) UpsertWTG(ctx context.Context, g *wtg.Group) error {
	equipJSON, err := json.Marshal(g.SharedEquipment)
	if err != nil {
		return err
	}
	policyJSON, err := json.Marshal(g.FeedingPolicy)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO water_treatment_groups(wtg_id,site_id,name,shared_equipment_json,intake_sensor_id,outlet_sensor_id,volume_m3,nh3_processing_kg_per_h,flow_rate_m3_per_h,feeding_policy_json,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(wtg_id) DO UPDATE SET
           site_id=excluded.site_id,
           name=excluded.name,
           shared_equipment_json=excluded.shared_equipment_json,
           intake_sensor_id=excluded.intake_sensor_id,
           outlet_sensor_id=excluded.outlet_sensor_id,
           volume_m3=excluded.volume_m3,
           nh3_processing_kg_per_h=excluded.nh3_processing_kg_per_h,
           flow_rate_m3_per_h=excluded.flow_rate_m3_per_h,
           feeding_policy_json=excluded.feeding_policy_json,
           updated_at=excluded.updated_at`,
		g.WTGID,
		g.SiteID,
		g.Name,
		string(equipJSON),
		nullStr(g.IntakeSensor),
		nullStr(g.OutletSensor),
		nullFloat(g.Capacity.VolumeM3),
		nullFloat(g.Capacity.NH3ProcessingKgPerH),
		nullFloat(g.Capacity.FlowRateM3PerH),
		string(policyJSON),
		fmtNow(),
		fmtNow(),
	)
	return err
}

// DeleteWTG removes a water_treatment_groups row.
func (s *sqliteStore) DeleteWTG(ctx context.Context, wtgID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM water_treatment_groups WHERE wtg_id=?`, wtgID)
	return err
}

// WTGExists — 존재 여부.
func (s *sqliteStore) WTGExists(ctx context.Context, wtgID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM water_treatment_groups WHERE wtg_id=?`, wtgID).Scan(&n)
	return n > 0, err
}

// CountTanksForWTG — wtg 에 속한 tank 수.
func (s *sqliteStore) CountTanksForWTG(ctx context.Context, wtgID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tank_profiles WHERE wtg_id=?`, wtgID).Scan(&n)
	return n, err
}

// ListWTGs returns all water treatment groups, optionally filtered by site_id.
func (s *sqliteStore) ListWTGs(ctx context.Context, siteID string) ([]*wtg.Group, error) {
	q := `SELECT wtg_id,site_id,name,shared_equipment_json,
	             COALESCE(intake_sensor_id,''),COALESCE(outlet_sensor_id,''),
	             COALESCE(volume_m3,0),COALESCE(nh3_processing_kg_per_h,0),COALESCE(flow_rate_m3_per_h,0),
	             feeding_policy_json
	        FROM water_treatment_groups`
	args := []any{}
	if siteID != "" {
		q += ` WHERE site_id=?`
		args = append(args, siteID)
	}
	q += ` ORDER BY wtg_id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*wtg.Group
	for rows.Next() {
		var g wtg.Group
		var equipJSON, policyJSON string
		if err := rows.Scan(
			&g.WTGID, &g.SiteID, &g.Name,
			&equipJSON,
			&g.IntakeSensor, &g.OutletSensor,
			&g.Capacity.VolumeM3, &g.Capacity.NH3ProcessingKgPerH, &g.Capacity.FlowRateM3PerH,
			&policyJSON,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(equipJSON), &g.SharedEquipment)
		_ = json.Unmarshal([]byte(policyJSON), &g.FeedingPolicy)
		out = append(out, &g)
	}
	return out, rows.Err()
}
