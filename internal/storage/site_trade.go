package storage

import (
	"context"
	"database/sql"
	"time"
)

// ── site_stockings ───────────────────────────────────────────────────────────

func (s *sqliteStore) UpsertSiteStocking(ctx context.Context, st *SiteStocking) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO site_stockings(
		   site_stocking_id,site_id,supplier_id,supplier_name,species,growth_stage,source_hatchery,
		   batch_lot_no,total_count,total_avg_weight_g,total_biomass_kg,allocations_json,
		   stocked_at,operator_id,notes,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(site_stocking_id) DO UPDATE SET
		   allocations_json=excluded.allocations_json,
		   total_count=excluded.total_count,
		   notes=excluded.notes,
		   updated_at=excluded.updated_at`,
		st.SiteStockingID, st.SiteID, nullStr(st.SupplierID), nullStr(st.SupplierName),
		st.Species, st.GrowthStage, nullStr(st.SourceHatchery), nullStr(st.BatchLotNo),
		st.TotalCount, st.TotalAvgWeightG, st.TotalBiomassKg, st.AllocationsJSON,
		fmtTime(st.StockedAt), st.OperatorID, nullStr(st.Notes),
		fmtTime(st.CreatedAt), fmtTime(st.UpdatedAt),
	)
	return err
}

const siteStockingCols = `site_stocking_id,site_id,COALESCE(supplier_id,''),COALESCE(supplier_name,''),
		        species,growth_stage,COALESCE(source_hatchery,''),COALESCE(batch_lot_no,''),
		        total_count,COALESCE(total_avg_weight_g,0),COALESCE(total_biomass_kg,0),allocations_json,
		        stocked_at,operator_id,COALESCE(notes,''),created_at,updated_at`

func (s *sqliteStore) GetSiteStocking(ctx context.Context, id string) (*SiteStocking, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+siteStockingCols+` FROM site_stockings WHERE site_stocking_id=?`, id)
	st, err := scanSiteStocking(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return st, err
}

func (s *sqliteStore) ListSiteStockings(ctx context.Context, siteID string) ([]*SiteStocking, error) {
	query := `SELECT ` + siteStockingCols + ` FROM site_stockings`
	args := []any{}
	if siteID != "" {
		query += ` WHERE site_id=?`
		args = append(args, siteID)
	}
	query += ` ORDER BY stocked_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SiteStocking
	for rows.Next() {
		st, scanErr := scanSiteStocking(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func scanSiteStocking(sc rowScanner) (*SiteStocking, error) {
	st := &SiteStocking{}
	var stockedAt, createdAt, updatedAt string
	if err := sc.Scan(
		&st.SiteStockingID, &st.SiteID, &st.SupplierID, &st.SupplierName,
		&st.Species, &st.GrowthStage, &st.SourceHatchery, &st.BatchLotNo,
		&st.TotalCount, &st.TotalAvgWeightG, &st.TotalBiomassKg, &st.AllocationsJSON,
		&stockedAt, &st.OperatorID, &st.Notes, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	st.StockedAt, _ = time.Parse(time.RFC3339Nano, stockedAt)
	st.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	st.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return st, nil
}

// ── site_harvests ────────────────────────────────────────────────────────────

func (s *sqliteStore) UpsertSiteHarvest(ctx context.Context, h *SiteHarvest) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO site_harvests(
		   site_harvest_id,site_id,buyer_id,buyer_name,total_count,total_biomass_kg,
		   lines_json,vehicle_info,harvested_at,operator_id,notes,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(site_harvest_id) DO UPDATE SET
		   lines_json=excluded.lines_json,
		   total_count=excluded.total_count,
		   notes=excluded.notes,
		   updated_at=excluded.updated_at`,
		h.SiteHarvestID, h.SiteID, nullStr(h.BuyerID), nullStr(h.BuyerName),
		h.TotalCount, h.TotalBiomassKg, h.LinesJSON, nullStr(h.VehicleInfo),
		fmtTime(h.HarvestedAt), h.OperatorID, nullStr(h.Notes),
		fmtTime(h.CreatedAt), fmtTime(h.UpdatedAt),
	)
	return err
}

const siteHarvestCols = `site_harvest_id,site_id,COALESCE(buyer_id,''),COALESCE(buyer_name,''),
		        total_count,COALESCE(total_biomass_kg,0),lines_json,COALESCE(vehicle_info,''),
		        harvested_at,operator_id,COALESCE(notes,''),created_at,updated_at`

func (s *sqliteStore) GetSiteHarvest(ctx context.Context, id string) (*SiteHarvest, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+siteHarvestCols+` FROM site_harvests WHERE site_harvest_id=?`, id)
	h, err := scanSiteHarvest(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return h, err
}

func (s *sqliteStore) ListSiteHarvests(ctx context.Context, siteID string) ([]*SiteHarvest, error) {
	query := `SELECT ` + siteHarvestCols + ` FROM site_harvests`
	args := []any{}
	if siteID != "" {
		query += ` WHERE site_id=?`
		args = append(args, siteID)
	}
	query += ` ORDER BY harvested_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SiteHarvest
	for rows.Next() {
		h, scanErr := scanSiteHarvest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func scanSiteHarvest(sc rowScanner) (*SiteHarvest, error) {
	h := &SiteHarvest{}
	var harvestedAt, createdAt, updatedAt string
	if err := sc.Scan(
		&h.SiteHarvestID, &h.SiteID, &h.BuyerID, &h.BuyerName,
		&h.TotalCount, &h.TotalBiomassKg, &h.LinesJSON, &h.VehicleInfo,
		&harvestedAt, &h.OperatorID, &h.Notes, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	h.HarvestedAt, _ = time.Parse(time.RFC3339Nano, harvestedAt)
	h.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	h.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return h, nil
}
