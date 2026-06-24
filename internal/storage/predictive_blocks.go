package storage

import (
	"context"
	"time"
)

func (s *sqliteStore) InsertPredictiveBlock(ctx context.Context, b *PredictiveBlock) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO predictive_blocks(block_id, wtg_id, tank_id, cycle_id, reason,
		  predicted_value, threshold_value, forecast_at, blocked_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		b.BlockID,
		b.WTGID,
		nullStr(b.TankID),
		nullStr(b.CycleID),
		b.Reason,
		b.PredictedValue,
		b.ThresholdValue,
		fmtTime(b.ForecastAt),
		fmtTime(b.BlockedAt),
	)
	return err
}

func (s *sqliteStore) ListPredictiveBlocks(ctx context.Context, limit int) ([]*PredictiveBlock, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT block_id, COALESCE(wtg_id,''), COALESCE(tank_id,''), COALESCE(cycle_id,''),
		        reason, COALESCE(predicted_value,0), COALESCE(threshold_value,0),
		        forecast_at, blocked_at
		 FROM predictive_blocks ORDER BY blocked_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*PredictiveBlock
	for rows.Next() {
		b := &PredictiveBlock{}
		var forecastAt, blockedAt string
		if err := rows.Scan(
			&b.BlockID, &b.WTGID, &b.TankID, &b.CycleID,
			&b.Reason, &b.PredictedValue, &b.ThresholdValue,
			&forecastAt, &blockedAt,
		); err != nil {
			return nil, err
		}
		b.ForecastAt, _ = time.Parse(time.RFC3339Nano, forecastAt)
		b.BlockedAt, _ = time.Parse(time.RFC3339Nano, blockedAt)
		out = append(out, b)
	}
	return out, rows.Err()
}
