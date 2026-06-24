package storage

import (
	"context"
	"database/sql"
	"encoding/json"

	"bluei.kr/edge/internal/config"
)

// TankProfile is the persisted local tank profile used by local-first operations.
type TankProfile = config.TankProfile
type MetricRange = config.MetricRange

func (s *sqliteStore) UpsertTankProfile(ctx context.Context, profile *TankProfile) error {
	rangesJSON, err := json.Marshal(profile.TargetRanges)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(profile.Metadata)
	if err != nil {
		return err
	}
	mutableLifecycleInt := 0
	if profile.MutableLifecycle {
		mutableLifecycleInt = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tank_profiles(tank_id,platform_tank_id,display_name,species,system_type,volume_m3,biomass_kg,fish_count,avg_weight_g,target_ranges_json,metadata_json,group_id,site_id,wtg_id,lot_no,lifecycle_stage,mutable_lifecycle,form_factor,diameter_m,length_m,width_m,depth_m,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(tank_id) DO UPDATE SET
           platform_tank_id=excluded.platform_tank_id,
           display_name=excluded.display_name,
           species=excluded.species,
           system_type=excluded.system_type,
           volume_m3=excluded.volume_m3,
           biomass_kg=excluded.biomass_kg,
           fish_count=excluded.fish_count,
           avg_weight_g=excluded.avg_weight_g,
           target_ranges_json=excluded.target_ranges_json,
           metadata_json=excluded.metadata_json,
           group_id=excluded.group_id,
           site_id=excluded.site_id,
           wtg_id=excluded.wtg_id,
           lot_no=excluded.lot_no,
           lifecycle_stage=excluded.lifecycle_stage,
           mutable_lifecycle=excluded.mutable_lifecycle,
           form_factor=excluded.form_factor,
           diameter_m=excluded.diameter_m,
           length_m=excluded.length_m,
           width_m=excluded.width_m,
           depth_m=excluded.depth_m,
           updated_at=excluded.updated_at`,
		profile.TankID,
		nullStr(profile.PlatformTankID),
		profile.DisplayName,
		profile.Species,
		profile.SystemType,
		nullFloat(profile.VolumeM3),
		nullFloat(profile.BiomassKg),
		nullInt(profile.FishCount),
		nullFloat(profile.AvgWeightG),
		string(rangesJSON),
		string(metadataJSON),
		nullStr(profile.GroupID),
		nullStr(profile.SiteID),
		nullStr(profile.WTGID),
		nullStr(profile.LotNo),
		nullStr(profile.LifecycleStage),
		mutableLifecycleInt,
		nullStr(profile.FormFactor),
		nullFloat(profile.DiameterM),
		nullFloat(profile.LengthM),
		nullFloat(profile.WidthM),
		nullFloat(profile.DepthM),
		fmtNow(),
	)
	return err
}

const tankProfileSelectCols = `tank_id,COALESCE(platform_tank_id,''),display_name,species,system_type,volume_m3,biomass_kg,fish_count,avg_weight_g,target_ranges_json,metadata_json,COALESCE(group_id,''),COALESCE(site_id,''),COALESCE(wtg_id,''),COALESCE(lot_no,''),COALESCE(lifecycle_stage,''),COALESCE(mutable_lifecycle,0),COALESCE(form_factor,''),diameter_m,length_m,width_m,depth_m`

func (s *sqliteStore) GetTankProfile(ctx context.Context, tankID string) (*TankProfile, error) {
	return s.scanTankProfile(s.db.QueryRowContext(ctx,
		`SELECT `+tankProfileSelectCols+` FROM tank_profiles WHERE tank_id=?`, tankID))
}

// DeleteTankProfile removes a tank_profiles row.
// 호출자가 사전에 활성 lifecycle / feed cycle 등 의존성 검증을 수행.
func (s *sqliteStore) DeleteTankProfile(ctx context.Context, tankID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tank_profiles WHERE tank_id=?`, tankID)
	return err
}

// CountActiveFeedCyclesForTank — 진행 중 (completed_at IS NULL) feed_cycles 가 있으면 삭제 거부.
func (s *sqliteStore) CountActiveFeedCyclesForTank(ctx context.Context, tankID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM feed_cycles WHERE tank_id=? AND completed_at IS NULL`, tankID).Scan(&n)
	return n, err
}

func (s *sqliteStore) ListTankProfiles(ctx context.Context) ([]*TankProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+tankProfileSelectCols+` FROM tank_profiles ORDER BY tank_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*TankProfile{}
	for rows.Next() {
		profile, err := scanTankProfileRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (s *sqliteStore) scanTankProfile(row *sql.Row) (*TankProfile, error) {
	profile, err := scanTankProfileScanner(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return profile, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTankProfileRows(rows *sql.Rows) (*TankProfile, error) { return scanTankProfileScanner(rows) }

func scanTankProfileScanner(row scanner) (*TankProfile, error) {
	p := &TankProfile{}
	var volume, biomass, avgWeight sql.NullFloat64
	var fishCount sql.NullInt64
	var rangesJSON, metadataJSON string
	var mutableLifecycleInt int
	var diameter, length, width, depth sql.NullFloat64
	if err := row.Scan(&p.TankID, &p.PlatformTankID, &p.DisplayName, &p.Species, &p.SystemType, &volume, &biomass, &fishCount, &avgWeight, &rangesJSON, &metadataJSON, &p.GroupID, &p.SiteID, &p.WTGID, &p.LotNo, &p.LifecycleStage, &mutableLifecycleInt, &p.FormFactor, &diameter, &length, &width, &depth); err != nil {
		return nil, err
	}
	p.MutableLifecycle = mutableLifecycleInt != 0
	if volume.Valid {
		p.VolumeM3 = volume.Float64
	}
	if biomass.Valid {
		p.BiomassKg = biomass.Float64
	}
	if fishCount.Valid {
		p.FishCount = int(fishCount.Int64)
	}
	if avgWeight.Valid {
		p.AvgWeightG = avgWeight.Float64
	}
	if diameter.Valid {
		p.DiameterM = diameter.Float64
	}
	if length.Valid {
		p.LengthM = length.Float64
	}
	if width.Valid {
		p.WidthM = width.Float64
	}
	if depth.Valid {
		p.DepthM = depth.Float64
	}
	_ = json.Unmarshal([]byte(rangesJSON), &p.TargetRanges)
	_ = json.Unmarshal([]byte(metadataJSON), &p.Metadata)
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	return p, nil
}

func nullFloat(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
