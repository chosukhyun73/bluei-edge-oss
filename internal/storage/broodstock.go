package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// BroodstockCohort — 어미 계군(같은 출신의 어미 한 묶음). 족보(출신성분)를 보관한다.
type BroodstockCohort struct {
	CohortID       string         `json:"cohort_id"`
	GroupID        string         `json:"group_id"`
	TankID         string         `json:"tank_id,omitempty"`
	Species        string         `json:"species"`
	OriginType     string         `json:"origin_type"` // wild | domestic
	OriginRegion   string         `json:"origin_region,omitempty"`
	Supplier       string         `json:"supplier,omitempty"`
	Generation     string         `json:"generation,omitempty"`
	ParentCohortID string         `json:"parent_cohort_id,omitempty"`
	AcquiredDate   string         `json:"acquired_date,omitempty"`
	MaleCount      int            `json:"male_count"`
	FemaleCount    int            `json:"female_count"`
	Maturity       string         `json:"maturity,omitempty"`
	Notes          string         `json:"notes,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"created_at,omitempty"`
	UpdatedAt      string         `json:"updated_at,omitempty"`
}

const broodstockCols = `cohort_id,group_id,tank_id,species,origin_type,origin_region,supplier,` +
	`generation,parent_cohort_id,acquired_date,male_count,female_count,maturity,notes,` +
	`metadata_json,created_at,updated_at`

func (s *sqliteStore) UpsertBroodstockCohort(ctx context.Context, c *BroodstockCohort) error {
	metaJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return err
	}
	now := fmtNow()
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO broodstock_cohorts(`+broodstockCols+`)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(cohort_id) DO UPDATE SET
           group_id=excluded.group_id, tank_id=excluded.tank_id, species=excluded.species,
           origin_type=excluded.origin_type, origin_region=excluded.origin_region,
           supplier=excluded.supplier, generation=excluded.generation,
           parent_cohort_id=excluded.parent_cohort_id, acquired_date=excluded.acquired_date,
           male_count=excluded.male_count, female_count=excluded.female_count,
           maturity=excluded.maturity, notes=excluded.notes,
           metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		c.CohortID, c.GroupID, c.TankID, c.Species, c.OriginType, c.OriginRegion, c.Supplier,
		c.Generation, c.ParentCohortID, c.AcquiredDate, c.MaleCount, c.FemaleCount, c.Maturity,
		c.Notes, string(metaJSON), c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (s *sqliteStore) GetBroodstockCohort(ctx context.Context, cohortID string) (*BroodstockCohort, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+broodstockCols+` FROM broodstock_cohorts WHERE cohort_id=?`, cohortID)
	c, err := scanBroodstockCohort(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *sqliteStore) ListBroodstockByGroup(ctx context.Context, groupID string) ([]*BroodstockCohort, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+broodstockCols+` FROM broodstock_cohorts WHERE group_id=? ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*BroodstockCohort{}
	for rows.Next() {
		c, err := scanBroodstockCohort(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteBroodstockCohort(ctx context.Context, cohortID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM broodstock_cohorts WHERE cohort_id=?`, cohortID)
	return err
}

func scanBroodstockCohort(row groupScanner) (*BroodstockCohort, error) {
	c := &BroodstockCohort{}
	var tankID, region, supplier, gen, parent, acquired, maturity, notes, metaJSON sql.NullString
	if err := row.Scan(
		&c.CohortID, &c.GroupID, &tankID, &c.Species, &c.OriginType, &region, &supplier,
		&gen, &parent, &acquired, &c.MaleCount, &c.FemaleCount, &maturity, &notes,
		&metaJSON, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	c.TankID = tankID.String
	c.OriginRegion = region.String
	c.Supplier = supplier.String
	c.Generation = gen.String
	c.ParentCohortID = parent.String
	c.AcquiredDate = acquired.String
	c.Maturity = maturity.String
	c.Notes = notes.String
	_ = json.Unmarshal([]byte(metaJSON.String), &c.Metadata)
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	return c, nil
}
