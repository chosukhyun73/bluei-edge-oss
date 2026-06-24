package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) GetTankSampling(ctx context.Context, tankID string) (*TankSampling, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tank_id, latest_sampling_id, COALESCE(stocking_id,''),
		        sampled_count, avg_weight_g,
		        std_weight_g, min_weight_g, max_weight_g,
		        health_score, COALESCE(health_notes,''),
		        abnormal_count,
		        sampled_at, recorded_by, updated_at
		 FROM current_tank_sampling WHERE tank_id=?`, tankID)
	return scanTankSampling(row)
}

func (s *sqliteStore) UpsertTankSampling(ctx context.Context, ts *TankSampling) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_tank_sampling(
		   tank_id, latest_sampling_id, stocking_id,
		   sampled_count, avg_weight_g,
		   std_weight_g, min_weight_g, max_weight_g,
		   health_score, health_notes, abnormal_count,
		   sampled_at, recorded_by, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(tank_id) DO UPDATE SET
		   latest_sampling_id=excluded.latest_sampling_id,
		   stocking_id=excluded.stocking_id,
		   sampled_count=excluded.sampled_count,
		   avg_weight_g=excluded.avg_weight_g,
		   std_weight_g=excluded.std_weight_g,
		   min_weight_g=excluded.min_weight_g,
		   max_weight_g=excluded.max_weight_g,
		   health_score=excluded.health_score,
		   health_notes=excluded.health_notes,
		   abnormal_count=excluded.abnormal_count,
		   sampled_at=excluded.sampled_at,
		   recorded_by=excluded.recorded_by,
		   updated_at=excluded.updated_at`,
		ts.TankID,
		ts.LatestSamplingID,
		nullStr(ts.StockingID),
		ts.SampledCount,
		ts.AvgWeightG,
		nullPtrFloat64(ts.StdWeightG),
		nullPtrFloat64(ts.MinWeightG),
		nullPtrFloat64(ts.MaxWeightG),
		nullPtrInt(ts.HealthScore),
		ts.HealthNotes,
		nullPtrInt(ts.AbnormalCount),
		fmtTime(ts.SampledAt),
		ts.RecordedBy,
		fmtTime(ts.UpdatedAt),
	)
	return err
}

// scanTankSampling — sql.Row (단건). 행 없으면 (nil, nil).
func scanTankSampling(row *sql.Row) (*TankSampling, error) {
	ts := &TankSampling{}
	var stdW, minW, maxW sql.NullFloat64
	var healthScore, abnormalCount sql.NullInt64
	var sampledAt, updatedAt string
	err := row.Scan(
		&ts.TankID, &ts.LatestSamplingID, &ts.StockingID,
		&ts.SampledCount, &ts.AvgWeightG,
		&stdW, &minW, &maxW,
		&healthScore, &ts.HealthNotes,
		&abnormalCount,
		&sampledAt, &ts.RecordedBy, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if stdW.Valid {
		v := stdW.Float64
		ts.StdWeightG = &v
	}
	if minW.Valid {
		v := minW.Float64
		ts.MinWeightG = &v
	}
	if maxW.Valid {
		v := maxW.Float64
		ts.MaxWeightG = &v
	}
	if healthScore.Valid {
		v := int(healthScore.Int64)
		ts.HealthScore = &v
	}
	if abnormalCount.Valid {
		v := int(abnormalCount.Int64)
		ts.AbnormalCount = &v
	}
	ts.SampledAt, _ = time.Parse(time.RFC3339Nano, sampledAt)
	ts.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return ts, nil
}

// nullPtrFloat64 — *float64 → sql NULL if nil.
func nullPtrFloat64(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

// nullPtrInt — *int → sql NULL if nil.
func nullPtrInt(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}
