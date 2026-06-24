package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// LarvalBatch — 자어 배치. 부화 후 자어조 사육. 부모 알 lot(SourceLotCode)에서 족보를
// 이어받아 GDST 연속성 유지(cohort→spawn→larval). 생존율/발달단계 추적.
type LarvalBatch struct {
	BatchID       string         `json:"batch_id"`
	GroupID       string         `json:"group_id"`
	TankID        string         `json:"tank_id,omitempty"`
	Species       string         `json:"species"`
	SourceLotCode string         `json:"source_lot_code,omitempty"`
	OriginType    string         `json:"origin_type,omitempty"`
	OriginRegion  string         `json:"origin_region,omitempty"`
	Supplier      string         `json:"supplier,omitempty"`
	Generation    string         `json:"generation,omitempty"`
	StartDate     string         `json:"start_date,omitempty"`
	InitialCount  int            `json:"initial_count"`
	CurrentCount  int            `json:"current_count"`
	SurvivalRate  float64        `json:"survival_rate"`
	DevStage      string         `json:"dev_stage,omitempty"`
	DensityPerL   float64        `json:"density_per_l"`
	Status        string         `json:"status"`
	Notes         string         `json:"notes,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     string         `json:"created_at,omitempty"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
}

const larvalCols = `batch_id,group_id,tank_id,species,source_lot_code,origin_type,origin_region,` +
	`supplier,generation,start_date,initial_count,current_count,survival_rate,dev_stage,` +
	`density_per_l,status,notes,metadata_json,created_at,updated_at`

func (s *sqliteStore) UpsertLarvalBatch(ctx context.Context, b *LarvalBatch) error {
	metaJSON, err := json.Marshal(b.Metadata)
	if err != nil {
		return err
	}
	now := fmtNow()
	if b.CreatedAt == "" {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	if b.Status == "" {
		b.Status = "rearing"
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO larval_batches(`+larvalCols+`)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(batch_id) DO UPDATE SET
           group_id=excluded.group_id, tank_id=excluded.tank_id, species=excluded.species,
           source_lot_code=excluded.source_lot_code, origin_type=excluded.origin_type,
           origin_region=excluded.origin_region, supplier=excluded.supplier,
           generation=excluded.generation, start_date=excluded.start_date,
           initial_count=excluded.initial_count, current_count=excluded.current_count,
           survival_rate=excluded.survival_rate, dev_stage=excluded.dev_stage,
           density_per_l=excluded.density_per_l, status=excluded.status, notes=excluded.notes,
           metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		b.BatchID, b.GroupID, b.TankID, b.Species, b.SourceLotCode, b.OriginType, b.OriginRegion,
		b.Supplier, b.Generation, b.StartDate, b.InitialCount, b.CurrentCount, b.SurvivalRate,
		b.DevStage, b.DensityPerL, b.Status, b.Notes, string(metaJSON), b.CreatedAt, b.UpdatedAt,
	)
	return err
}

func (s *sqliteStore) GetLarvalBatch(ctx context.Context, batchID string) (*LarvalBatch, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+larvalCols+` FROM larval_batches WHERE batch_id=?`, batchID)
	b, err := scanLarvalBatch(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func (s *sqliteStore) ListLarvalBatchesByGroup(ctx context.Context, groupID string) ([]*LarvalBatch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+larvalCols+` FROM larval_batches WHERE group_id=? ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LarvalBatch{}
	for rows.Next() {
		b, err := scanLarvalBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteLarvalBatch(ctx context.Context, batchID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM larval_batches WHERE batch_id=?`, batchID)
	return err
}

func scanLarvalBatch(row groupScanner) (*LarvalBatch, error) {
	b := &LarvalBatch{}
	var tankID, srcLot, oType, oRegion, supplier, gen, startDate, devStage, notes, metaJSON sql.NullString
	var initCount, curCount sql.NullInt64
	var survival, density sql.NullFloat64
	if err := row.Scan(
		&b.BatchID, &b.GroupID, &tankID, &b.Species, &srcLot, &oType, &oRegion,
		&supplier, &gen, &startDate, &initCount, &curCount, &survival, &devStage,
		&density, &b.Status, &notes, &metaJSON, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, err
	}
	b.TankID = tankID.String
	b.SourceLotCode = srcLot.String
	b.OriginType = oType.String
	b.OriginRegion = oRegion.String
	b.Supplier = supplier.String
	b.Generation = gen.String
	b.StartDate = startDate.String
	b.DevStage = devStage.String
	b.Notes = notes.String
	b.InitialCount = int(initCount.Int64)
	b.CurrentCount = int(curCount.Int64)
	b.SurvivalRate = survival.Float64
	b.DensityPerL = density.Float64
	_ = json.Unmarshal([]byte(metaJSON.String), &b.Metadata)
	if b.Metadata == nil {
		b.Metadata = map[string]any{}
	}
	return b, nil
}
