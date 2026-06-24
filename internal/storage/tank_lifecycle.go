package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) GetTankLifecycle(ctx context.Context, tankID string) (*TankLifecycle, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tank_id,active_stocking_id,species,growth_stage,
		        initial_count,initial_avg_weight_g,
		        target_harvest_weight_g,COALESCE(target_harvest_date,''),
		        COALESCE(source_hatchery,''),stocked_at,status,updated_at,
		        COALESCE(lot_no,''),COALESCE(parent_lot_no,'')
		 FROM current_tank_lifecycle WHERE tank_id=?`, tankID)
	return scanTankLifecycle(row)
}

func (s *sqliteStore) UpsertTankLifecycle(ctx context.Context, lc *TankLifecycle) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_tank_lifecycle(
		   tank_id,active_stocking_id,species,growth_stage,
		   initial_count,initial_avg_weight_g,
		   target_harvest_weight_g,target_harvest_date,
		   source_hatchery,stocked_at,status,updated_at,
		   lot_no,parent_lot_no)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(tank_id) DO UPDATE SET
		   active_stocking_id=excluded.active_stocking_id,
		   species=excluded.species,
		   growth_stage=excluded.growth_stage,
		   initial_count=excluded.initial_count,
		   initial_avg_weight_g=excluded.initial_avg_weight_g,
		   target_harvest_weight_g=excluded.target_harvest_weight_g,
		   target_harvest_date=excluded.target_harvest_date,
		   source_hatchery=excluded.source_hatchery,
		   stocked_at=excluded.stocked_at,
		   status=excluded.status,
		   updated_at=excluded.updated_at,
		   lot_no=excluded.lot_no,
		   parent_lot_no=excluded.parent_lot_no`,
		lc.TankID,
		lc.ActiveStockingID,
		lc.Species,
		lc.GrowthStage,
		lc.InitialCount,
		lc.InitialAvgWeightG,
		lc.TargetHarvestWeightG,
		nullStr(lc.TargetHarvestDate),
		nullStr(lc.SourceHatchery),
		fmtTime(lc.StockedAt),
		lc.Status,
		fmtTime(lc.UpdatedAt),
		nullStr(lc.LotNo),
		nullStr(lc.ParentLotNo),
	)
	return err
}

// scanTankLifecycle — sql.Row (단건). 행 없으면 (nil, nil).
func scanTankLifecycle(row *sql.Row) (*TankLifecycle, error) {
	lc := &TankLifecycle{}
	var targetWeight sql.NullFloat64
	var stockedAt, updatedAt string
	err := row.Scan(
		&lc.TankID, &lc.ActiveStockingID, &lc.Species, &lc.GrowthStage,
		&lc.InitialCount, &lc.InitialAvgWeightG,
		&targetWeight, &lc.TargetHarvestDate,
		&lc.SourceHatchery, &stockedAt, &lc.Status, &updatedAt,
		&lc.LotNo, &lc.ParentLotNo,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if targetWeight.Valid {
		v := targetWeight.Float64
		lc.TargetHarvestWeightG = &v
	}
	lc.StockedAt, _ = time.Parse(time.RFC3339Nano, stockedAt)
	lc.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return lc, nil
}
