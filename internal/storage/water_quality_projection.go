package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) UpsertWaterQualityBucket(ctx context.Context, b *WaterQualityBucketProjection) error {
	updatedAt := b.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO water_quality_buckets(
		  tank_id,bucket_start,bucket_sec,temperature_c_avg,ph_avg,do_mg_l_avg,
		  quality,sample_count,suspect_count,source_reading_ids_json,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(tank_id,bucket_start) DO UPDATE SET
		  bucket_sec=excluded.bucket_sec,
		  temperature_c_avg=excluded.temperature_c_avg,
		  ph_avg=excluded.ph_avg,
		  do_mg_l_avg=excluded.do_mg_l_avg,
		  quality=excluded.quality,
		  sample_count=excluded.sample_count,
		  suspect_count=excluded.suspect_count,
		  source_reading_ids_json=excluded.source_reading_ids_json,
		  updated_at=excluded.updated_at`,
		b.TankID, fmtTime(b.BucketStart), b.BucketSec, b.TemperatureCAvg, b.PHAvg, b.DOMgLAvg,
		b.Quality, b.SampleCount, b.SuspectCount, b.SourceReadingIDs, fmtTime(updatedAt))
	return err
}

func (s *sqliteStore) ListWaterQualityBuckets(ctx context.Context, tankID string, since, until *time.Time, limit int) ([]*WaterQualityBucketProjection, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT tank_id,bucket_start,bucket_sec,temperature_c_avg,ph_avg,do_mg_l_avg,
	          quality,sample_count,suspect_count,source_reading_ids_json,updated_at
	          FROM water_quality_buckets WHERE tank_id=?`
	args := []any{tankID}
	if since != nil {
		query += ` AND bucket_start>=?`
		args = append(args, fmtTime(*since))
	}
	if until != nil {
		query += ` AND bucket_start<=?`
		args = append(args, fmtTime(*until))
	}
	query += ` ORDER BY bucket_start ASC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WaterQualityBucketProjection
	for rows.Next() {
		b := &WaterQualityBucketProjection{}
		var bucketStart, updatedAt string
		if err := rows.Scan(&b.TankID, &bucketStart, &b.BucketSec, &b.TemperatureCAvg, &b.PHAvg, &b.DOMgLAvg,
			&b.Quality, &b.SampleCount, &b.SuspectCount, &b.SourceReadingIDs, &updatedAt); err != nil {
			return nil, err
		}
		b.BucketStart, _ = time.Parse(time.RFC3339Nano, bucketStart)
		b.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertFeedingImpactAnalysis(ctx context.Context, a *FeedingImpactAnalysisProjection) error {
	updatedAt := a.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feeding_impact_analyses(
		  feeding_id,analysis_id,tank_id,feed_amount_g,fed_at,do_baseline_mg_l,do_min_post_mg_l,
		  do_drop_mg_l,do_recovery_min,ph_delta,temp_delta_c,quality,reason_codes_json,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(feeding_id) DO UPDATE SET
		  analysis_id=excluded.analysis_id,
		  tank_id=excluded.tank_id,
		  feed_amount_g=excluded.feed_amount_g,
		  fed_at=excluded.fed_at,
		  do_baseline_mg_l=excluded.do_baseline_mg_l,
		  do_min_post_mg_l=excluded.do_min_post_mg_l,
		  do_drop_mg_l=excluded.do_drop_mg_l,
		  do_recovery_min=excluded.do_recovery_min,
		  ph_delta=excluded.ph_delta,
		  temp_delta_c=excluded.temp_delta_c,
		  quality=excluded.quality,
		  reason_codes_json=excluded.reason_codes_json,
		  updated_at=excluded.updated_at`,
		a.FeedingID, a.AnalysisID, a.TankID, a.FeedAmountG, fmtTime(a.FedAt), a.DOBaselineMgL, a.DOMinPostMgL,
		a.DODropMgL, a.DORecoveryMin, a.PHDelta, a.TempDeltaC, a.Quality, a.ReasonCodesJSON, fmtTime(updatedAt))
	return err
}

func (s *sqliteStore) GetFeedingImpactAnalysis(ctx context.Context, feedingID string) (*FeedingImpactAnalysisProjection, error) {
	a := &FeedingImpactAnalysisProjection{}
	var fedAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT feeding_id,analysis_id,tank_id,feed_amount_g,fed_at,do_baseline_mg_l,do_min_post_mg_l,
		        do_drop_mg_l,do_recovery_min,ph_delta,temp_delta_c,quality,reason_codes_json,updated_at
		 FROM feeding_impact_analyses WHERE feeding_id=?`, feedingID).
		Scan(&a.FeedingID, &a.AnalysisID, &a.TankID, &a.FeedAmountG, &fedAt, &a.DOBaselineMgL, &a.DOMinPostMgL,
			&a.DODropMgL, &a.DORecoveryMin, &a.PHDelta, &a.TempDeltaC, &a.Quality, &a.ReasonCodesJSON, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.FedAt, _ = time.Parse(time.RFC3339Nano, fedAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return a, nil
}
