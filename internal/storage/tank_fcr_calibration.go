package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) GetTankFCRCalibration(ctx context.Context, tankID string) (*TankFCRCalibration, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tank_id, stocking_id, sampling_id,
		        default_fcr, observed_fcr, calibrated_fcr,
		        deviation_pct, cumulative_feed_g, delta_biomass_g,
		        calibrated_at
		 FROM current_tank_fcr_calibration WHERE tank_id=?`, tankID)
	return scanTankFCRCalibration(row)
}

func (s *sqliteStore) UpsertTankFCRCalibration(ctx context.Context, c *TankFCRCalibration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_tank_fcr_calibration(
		   tank_id, stocking_id, sampling_id,
		   default_fcr, observed_fcr, calibrated_fcr,
		   deviation_pct, cumulative_feed_g, delta_biomass_g,
		   calibrated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(tank_id) DO UPDATE SET
		   stocking_id=excluded.stocking_id,
		   sampling_id=excluded.sampling_id,
		   default_fcr=excluded.default_fcr,
		   observed_fcr=excluded.observed_fcr,
		   calibrated_fcr=excluded.calibrated_fcr,
		   deviation_pct=excluded.deviation_pct,
		   cumulative_feed_g=excluded.cumulative_feed_g,
		   delta_biomass_g=excluded.delta_biomass_g,
		   calibrated_at=excluded.calibrated_at`,
		c.TankID,
		c.StockingID,
		c.SamplingID,
		c.DefaultFCR,
		c.ObservedFCR,
		c.CalibratedFCR,
		c.DeviationPct,
		c.CumulativeFeedG,
		c.DeltaBiomassG,
		fmtTime(c.CalibratedAt),
	)
	return err
}

// scanTankFCRCalibration — sql.Row (단건). 행 없으면 (nil, nil).
func scanTankFCRCalibration(row *sql.Row) (*TankFCRCalibration, error) {
	c := &TankFCRCalibration{}
	var calibratedAt string
	err := row.Scan(
		&c.TankID, &c.StockingID, &c.SamplingID,
		&c.DefaultFCR, &c.ObservedFCR, &c.CalibratedFCR,
		&c.DeviationPct, &c.CumulativeFeedG, &c.DeltaBiomassG,
		&calibratedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.CalibratedAt, _ = time.Parse(time.RFC3339Nano, calibratedAt)
	return c, nil
}
