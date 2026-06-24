package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// SpawnBatch — 산란 배치(알 lot). 어미 계군에서 이어지며 족보를 스냅샷 보존한다.
// 부화 결과는 같은 레코드에 갱신(hatch_*). status: incubating|hatched|discarded|sold.
type SpawnBatch struct {
	BatchID           string         `json:"batch_id"`
	GroupID           string         `json:"group_id"`
	TankID            string         `json:"tank_id,omitempty"`
	Species           string         `json:"species"`
	LotCode           string         `json:"lot_code,omitempty"`
	FemaleCohortID    string         `json:"female_cohort_id,omitempty"`
	MaleCohortID      string         `json:"male_cohort_id,omitempty"`
	OriginType        string         `json:"origin_type,omitempty"`
	OriginRegion      string         `json:"origin_region,omitempty"`
	Supplier          string         `json:"supplier,omitempty"`
	Generation        string         `json:"generation,omitempty"`
	SpawnDate         string         `json:"spawn_date,omitempty"`
	EggCount          int            `json:"egg_count"`
	EggVolumeML       float64        `json:"egg_volume_ml"`
	FertilizationRate float64        `json:"fertilization_rate"`
	HatchDate         string         `json:"hatch_date,omitempty"`
	HatchedCount      int            `json:"hatched_count"`
	HatchRate         float64        `json:"hatch_rate"`
	Status            string         `json:"status"`
	Buyer             string         `json:"buyer,omitempty"`
	Notes             string         `json:"notes,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         string         `json:"created_at,omitempty"`
	UpdatedAt         string         `json:"updated_at,omitempty"`
}

const spawnCols = `batch_id,group_id,tank_id,species,lot_code,female_cohort_id,male_cohort_id,` +
	`origin_type,origin_region,supplier,generation,spawn_date,egg_count,egg_volume_ml,` +
	`fertilization_rate,hatch_date,hatched_count,hatch_rate,status,buyer,notes,` +
	`metadata_json,created_at,updated_at`

func (s *sqliteStore) UpsertSpawnBatch(ctx context.Context, b *SpawnBatch) error {
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
		b.Status = "incubating"
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO spawn_batches(`+spawnCols+`)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(batch_id) DO UPDATE SET
           group_id=excluded.group_id, tank_id=excluded.tank_id, species=excluded.species,
           lot_code=excluded.lot_code, female_cohort_id=excluded.female_cohort_id,
           male_cohort_id=excluded.male_cohort_id, origin_type=excluded.origin_type,
           origin_region=excluded.origin_region, supplier=excluded.supplier,
           generation=excluded.generation, spawn_date=excluded.spawn_date,
           egg_count=excluded.egg_count, egg_volume_ml=excluded.egg_volume_ml,
           fertilization_rate=excluded.fertilization_rate, hatch_date=excluded.hatch_date,
           hatched_count=excluded.hatched_count, hatch_rate=excluded.hatch_rate,
           status=excluded.status, buyer=excluded.buyer, notes=excluded.notes,
           metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		b.BatchID, b.GroupID, b.TankID, b.Species, b.LotCode, b.FemaleCohortID, b.MaleCohortID,
		b.OriginType, b.OriginRegion, b.Supplier, b.Generation, b.SpawnDate, b.EggCount, b.EggVolumeML,
		b.FertilizationRate, b.HatchDate, b.HatchedCount, b.HatchRate, b.Status, b.Buyer, b.Notes,
		string(metaJSON), b.CreatedAt, b.UpdatedAt,
	)
	return err
}

func (s *sqliteStore) GetSpawnBatch(ctx context.Context, batchID string) (*SpawnBatch, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+spawnCols+` FROM spawn_batches WHERE batch_id=?`, batchID)
	b, err := scanSpawnBatch(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func (s *sqliteStore) GetSpawnBatchByLotCode(ctx context.Context, lotCode string) (*SpawnBatch, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+spawnCols+` FROM spawn_batches WHERE lot_code=? LIMIT 1`, lotCode)
	b, err := scanSpawnBatch(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func (s *sqliteStore) ListSpawnBatchesByGroup(ctx context.Context, groupID string) ([]*SpawnBatch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+spawnCols+` FROM spawn_batches WHERE group_id=? ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*SpawnBatch{}
	for rows.Next() {
		b, err := scanSpawnBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteSpawnBatch(ctx context.Context, batchID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM spawn_batches WHERE batch_id=?`, batchID)
	return err
}

func scanSpawnBatch(row groupScanner) (*SpawnBatch, error) {
	b := &SpawnBatch{}
	var tankID, lotCode, fCohort, mCohort, oType, oRegion, supplier, gen sql.NullString
	var spawnDate, hatchDate, buyer, notes, metaJSON sql.NullString
	var eggCount, hatchedCount sql.NullInt64
	var eggVol, fertRate, hatchRate sql.NullFloat64
	if err := row.Scan(
		&b.BatchID, &b.GroupID, &tankID, &b.Species, &lotCode, &fCohort, &mCohort,
		&oType, &oRegion, &supplier, &gen, &spawnDate, &eggCount, &eggVol,
		&fertRate, &hatchDate, &hatchedCount, &hatchRate, &b.Status, &buyer, &notes,
		&metaJSON, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, err
	}
	b.TankID = tankID.String
	b.LotCode = lotCode.String
	b.FemaleCohortID = fCohort.String
	b.MaleCohortID = mCohort.String
	b.OriginType = oType.String
	b.OriginRegion = oRegion.String
	b.Supplier = supplier.String
	b.Generation = gen.String
	b.SpawnDate = spawnDate.String
	b.HatchDate = hatchDate.String
	b.Buyer = buyer.String
	b.Notes = notes.String
	b.EggCount = int(eggCount.Int64)
	b.HatchedCount = int(hatchedCount.Int64)
	b.EggVolumeML = eggVol.Float64
	b.FertilizationRate = fertRate.Float64
	b.HatchRate = hatchRate.Float64
	_ = json.Unmarshal([]byte(metaJSON.String), &b.Metadata)
	if b.Metadata == nil {
		b.Metadata = map[string]any{}
	}
	return b, nil
}
