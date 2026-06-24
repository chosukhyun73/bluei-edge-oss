package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) UpsertTankWeightSnapshot(ctx context.Context, snap *TankWeightSnapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tank_weight_history(
			tank_id, snapshot_date, estimated_avg_weight_g, anchor_weight_g,
			anchor_source, days_since_anchor, expected_fcr, fcr_source,
			cumulative_feed_g, quality, snapshot_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tank_id, snapshot_date) DO UPDATE SET
			estimated_avg_weight_g = excluded.estimated_avg_weight_g,
			anchor_weight_g        = excluded.anchor_weight_g,
			anchor_source          = excluded.anchor_source,
			days_since_anchor      = excluded.days_since_anchor,
			expected_fcr           = excluded.expected_fcr,
			fcr_source             = excluded.fcr_source,
			cumulative_feed_g      = excluded.cumulative_feed_g,
			quality                = excluded.quality,
			snapshot_at            = excluded.snapshot_at`,
		snap.TankID,
		snap.SnapshotDate,
		snap.EstimatedAvgWeightG,
		snap.AnchorWeightG,
		snap.AnchorSource,
		snap.DaysSinceAnchor,
		snap.ExpectedFCR,
		snap.FCRSource,
		snap.CumulativeFeedG,
		snap.Quality,
		snap.SnapshotAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *sqliteStore) ListTankWeightHistory(ctx context.Context, tankID string, days int) ([]TankWeightSnapshot, error) {
	if days < 1 {
		days = 1
	}
	if days > 365 {
		days = 365
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tank_id, snapshot_date, estimated_avg_weight_g, anchor_weight_g,
		        anchor_source, days_since_anchor, expected_fcr, fcr_source,
		        cumulative_feed_g, quality, snapshot_at
		 FROM tank_weight_history
		 WHERE tank_id = ? AND snapshot_date >= date('now', ?)
		 ORDER BY snapshot_date ASC
		 LIMIT 366`,
		tankID,
		"-"+formatDays(days)+" days",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TankWeightSnapshot
	for rows.Next() {
		var snap TankWeightSnapshot
		var snapshotAt string
		if err := rows.Scan(
			&snap.TankID,
			&snap.SnapshotDate,
			&snap.EstimatedAvgWeightG,
			&snap.AnchorWeightG,
			&snap.AnchorSource,
			&snap.DaysSinceAnchor,
			&snap.ExpectedFCR,
			&snap.FCRSource,
			&snap.CumulativeFeedG,
			&snap.Quality,
			&snapshotAt,
		); err != nil {
			return nil, err
		}
		snap.SnapshotAt, _ = time.Parse(time.RFC3339, snapshotAt)
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []TankWeightSnapshot{}
	}
	return out, nil
}

// formatDays — int → string (SQLite date modifier 용).
func formatDays(n int) string {
	// date('now', '-30 days') 형식
	b := make([]byte, 0, 4)
	if n == 0 {
		return "0"
	}
	remaining := n
	for remaining > 0 {
		b = append([]byte{byte('0' + remaining%10)}, b...)
		remaining /= 10
	}
	return string(b)
}

// scanNullString — sql.NullString helper used in this file.
func scanNullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
