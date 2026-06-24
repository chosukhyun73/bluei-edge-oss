package storage

import (
	"context"
	"time"
)

// EnvironmentalSnapshot is a row in environmental_snapshots.
type EnvironmentalSnapshot struct {
	SnapshotID       string    `json:"snapshot_id"`
	SiteID           string    `json:"site_id"`
	WindSpeedMS      *float64  `json:"wind_speed_ms,omitempty"`
	WaveHeightM      *float64  `json:"wave_height_m,omitempty"`
	TidePhase        string    `json:"tide_phase,omitempty"`
	TideMinutesToLow *int      `json:"tide_minutes_to_low,omitempty"`
	TemperatureC     *float64  `json:"temperature_c,omitempty"`
	RecordedAt       time.Time `json:"recorded_at"`
	Source           string    `json:"source"`
}

func (s *sqliteStore) InsertEnvironmentalSnapshot(ctx context.Context, snap *EnvironmentalSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO environmental_snapshots
		 (snapshot_id,site_id,wind_speed_ms,wave_height_m,tide_phase,tide_minutes_to_low,temperature_c,recorded_at,source)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		snap.SnapshotID, snap.SiteID,
		snap.WindSpeedMS, snap.WaveHeightM,
		nullStr(snap.TidePhase), snap.TideMinutesToLow,
		snap.TemperatureC, fmtTime(snap.RecordedAt), snap.Source,
	)
	return err
}

func (s *sqliteStore) ListRecentEnvironmentalSnapshots(ctx context.Context, siteID string, limit int) ([]*EnvironmentalSnapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT snapshot_id,site_id,wind_speed_ms,wave_height_m,
		        COALESCE(tide_phase,''),tide_minutes_to_low,temperature_c,recorded_at,source
		 FROM environmental_snapshots
		 WHERE site_id=?
		 ORDER BY recorded_at DESC LIMIT ?`, siteID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*EnvironmentalSnapshot
	for rows.Next() {
		snap := &EnvironmentalSnapshot{}
		var recordedAt string
		if err := rows.Scan(
			&snap.SnapshotID, &snap.SiteID,
			&snap.WindSpeedMS, &snap.WaveHeightM,
			&snap.TidePhase, &snap.TideMinutesToLow,
			&snap.TemperatureC, &recordedAt, &snap.Source,
		); err != nil {
			return nil, err
		}
		snap.RecordedAt, _ = time.Parse(time.RFC3339Nano, recordedAt)
		out = append(out, snap)
	}
	return out, rows.Err()
}
