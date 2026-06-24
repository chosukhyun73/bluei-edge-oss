package storage

import (
	"context"
	"encoding/json"

	"bluei.kr/edge/internal/site"
)

// UpsertSiteLand inserts or updates a land site row.
func (s *sqliteStore) UpsertSiteLand(ctx context.Context, sl *site.SiteLand) error {
	metaJSON, err := json.Marshal(sl.Metadata)
	if err != nil {
		return err
	}
	var lat, lon *float64
	if sl.Location.Coordinates != nil {
		lat = &sl.Location.Coordinates.Lat
		lon = &sl.Location.Coordinates.Lon
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sites(site_id,farm_id,site_type,name,timezone,address,lat,lon,metadata_json,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(site_id) DO UPDATE SET
           farm_id=excluded.farm_id,
           site_type=excluded.site_type,
           name=excluded.name,
           timezone=excluded.timezone,
           address=excluded.address,
           lat=excluded.lat,
           lon=excluded.lon,
           metadata_json=excluded.metadata_json,
           updated_at=excluded.updated_at`,
		sl.SiteID,
		sl.FarmID,
		string(site.TypeLand),
		sl.Name,
		sl.Timezone,
		nullStr(sl.Location.Address),
		lat,
		lon,
		string(metaJSON),
		fmtNow(),
		fmtNow(),
	)
	return err
}

// UpsertSiteMarine inserts or updates a marine site row, then bulk-replaces GPS points.
func (s *sqliteStore) UpsertSiteMarine(ctx context.Context, sm *site.SiteMarine) error {
	metaJSON, err := json.Marshal(sm.Metadata)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO sites(site_id,farm_id,site_type,name,timezone,heading_deg,metadata_json,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?)
         ON CONFLICT(site_id) DO UPDATE SET
           farm_id=excluded.farm_id,
           site_type=excluded.site_type,
           name=excluded.name,
           timezone=excluded.timezone,
           heading_deg=excluded.heading_deg,
           metadata_json=excluded.metadata_json,
           updated_at=excluded.updated_at`,
		sm.SiteID,
		sm.FarmID,
		string(site.TypeMarine),
		sm.Name,
		sm.Timezone,
		sm.Location.HeadingDeg,
		string(metaJSON),
		fmtNow(),
		fmtNow(),
	)
	if err != nil {
		return err
	}

	// 기존 GPS 점 모두 삭제 후 재삽입 (bulk-replace)
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM sites_marine_gps WHERE site_id=?`, sm.SiteID); err != nil {
		return err
	}
	for _, gp := range sm.Location.GPSPoints {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO sites_marine_gps(site_id,position,lat,lon,updated_at) VALUES(?,?,?,?,?)`,
			sm.SiteID, gp.Position, gp.Lat, gp.Lon, fmtNow()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteSite removes a site row (sites_marine_gps rows are FK CASCADE in schema).
func (s *sqliteStore) DeleteSite(ctx context.Context, siteID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sites_marine_gps WHERE site_id=?`, siteID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sites WHERE site_id=?`, siteID); err != nil {
		return err
	}
	return tx.Commit()
}

// SiteExists — site_id 존재 여부 (NOT_FOUND 분기용).
func (s *sqliteStore) SiteExists(ctx context.Context, siteID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites WHERE site_id=?`, siteID).Scan(&n)
	return n > 0, err
}

// CountWTGsForSite — site 에 속한 WTG 수 (FK 의존성 reject 용).
func (s *sqliteStore) CountWTGsForSite(ctx context.Context, siteID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM water_treatment_groups WHERE site_id=?`, siteID).Scan(&n)
	return n, err
}

// CountTanksForSite — site 에 속한 tank 수.
func (s *sqliteStore) CountTanksForSite(ctx context.Context, siteID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tank_profiles WHERE site_id=?`, siteID).Scan(&n)
	return n, err
}

// ListSites returns all sites, optionally filtered by farm_id.
// Returns raw map[string]any rows so the caller doesn't need to unify land/marine types.
func (s *sqliteStore) ListSites(ctx context.Context, farmID string) ([]map[string]any, error) {
	q := `SELECT site_id,farm_id,site_type,name,timezone,address,lat,lon,heading_deg,metadata_json FROM sites`
	args := []any{}
	if farmID != "" {
		q += ` WHERE farm_id=?`
		args = append(args, farmID)
	}
	q += ` ORDER BY site_id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var siteID, farmIDCol, siteType, name, tz string
		var address *string
		var lat, lon, headingDeg *float64
		var metaJSON string
		if err := rows.Scan(&siteID, &farmIDCol, &siteType, &name, &tz, &address, &lat, &lon, &headingDeg, &metaJSON); err != nil {
			return nil, err
		}
		var meta map[string]any
		_ = json.Unmarshal([]byte(metaJSON), &meta)
		row := map[string]any{
			"site_id":   siteID,
			"farm_id":   farmIDCol,
			"site_type": siteType,
			"name":      name,
			"timezone":  tz,
		}
		if address != nil {
			row["address"] = *address
		}
		if lat != nil {
			row["lat"] = *lat
		}
		if lon != nil {
			row["lon"] = *lon
		}
		if headingDeg != nil {
			row["heading_deg"] = *headingDeg
		}
		if len(meta) > 0 {
			row["metadata"] = meta
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
