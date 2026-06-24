package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// LiveFeedCulture — 먹이배양 배치(로티퍼/알테미아 등). 자어 급이용. 연어류는 불필요.
type LiveFeedCulture struct {
	CultureID       string         `json:"culture_id"`
	GroupID         string         `json:"group_id"`
	TankID          string         `json:"tank_id,omitempty"`
	FeedType        string         `json:"feed_type"` // rotifer|artemia|microalgae|copepod|other
	Strain          string         `json:"strain,omitempty"`
	StartDate       string         `json:"start_date,omitempty"`
	VolumeL         float64        `json:"volume_l"`
	DensityPerML    float64        `json:"density_per_ml"`
	LastHarvestDate string         `json:"last_harvest_date,omitempty"`
	HarvestAmount   string         `json:"harvest_amount,omitempty"`
	Status          string         `json:"status"`
	Notes           string         `json:"notes,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
}

const liveFeedCols = `culture_id,group_id,tank_id,feed_type,strain,start_date,volume_l,` +
	`density_per_ml,last_harvest_date,harvest_amount,status,notes,metadata_json,created_at,updated_at`

func (s *sqliteStore) UpsertLiveFeedCulture(ctx context.Context, c *LiveFeedCulture) error {
	metaJSON, err := json.Marshal(c.Metadata)
	if err != nil {
		return err
	}
	now := fmtNow()
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = "culturing"
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO live_feed_cultures(`+liveFeedCols+`)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(culture_id) DO UPDATE SET
           group_id=excluded.group_id, tank_id=excluded.tank_id, feed_type=excluded.feed_type,
           strain=excluded.strain, start_date=excluded.start_date, volume_l=excluded.volume_l,
           density_per_ml=excluded.density_per_ml, last_harvest_date=excluded.last_harvest_date,
           harvest_amount=excluded.harvest_amount, status=excluded.status, notes=excluded.notes,
           metadata_json=excluded.metadata_json, updated_at=excluded.updated_at`,
		c.CultureID, c.GroupID, c.TankID, c.FeedType, c.Strain, c.StartDate, c.VolumeL,
		c.DensityPerML, c.LastHarvestDate, c.HarvestAmount, c.Status, c.Notes,
		string(metaJSON), c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (s *sqliteStore) GetLiveFeedCulture(ctx context.Context, cultureID string) (*LiveFeedCulture, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+liveFeedCols+` FROM live_feed_cultures WHERE culture_id=?`, cultureID)
	c, err := scanLiveFeedCulture(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *sqliteStore) ListLiveFeedByGroup(ctx context.Context, groupID string) ([]*LiveFeedCulture, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+liveFeedCols+` FROM live_feed_cultures WHERE group_id=? ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LiveFeedCulture{}
	for rows.Next() {
		c, err := scanLiveFeedCulture(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteLiveFeedCulture(ctx context.Context, cultureID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM live_feed_cultures WHERE culture_id=?`, cultureID)
	return err
}

func scanLiveFeedCulture(row groupScanner) (*LiveFeedCulture, error) {
	c := &LiveFeedCulture{}
	var tankID, strain, startDate, lastHarvest, harvestAmt, notes, metaJSON sql.NullString
	var volume, density sql.NullFloat64
	if err := row.Scan(
		&c.CultureID, &c.GroupID, &tankID, &c.FeedType, &strain, &startDate, &volume,
		&density, &lastHarvest, &harvestAmt, &c.Status, &notes, &metaJSON, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	c.TankID = tankID.String
	c.Strain = strain.String
	c.StartDate = startDate.String
	c.LastHarvestDate = lastHarvest.String
	c.HarvestAmount = harvestAmt.String
	c.Notes = notes.String
	c.VolumeL = volume.Float64
	c.DensityPerML = density.Float64
	_ = json.Unmarshal([]byte(metaJSON.String), &c.Metadata)
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	return c, nil
}
