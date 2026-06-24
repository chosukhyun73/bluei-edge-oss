package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *sqliteStore) UpsertGroupProfile(ctx context.Context, p *GroupProfile) error {
	metaJSON, err := json.Marshal(p.Metadata)
	if err != nil {
		return err
	}
	now := fmtNow()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO group_profiles(group_id,name,description,color,metadata_json,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?)
         ON CONFLICT(group_id) DO UPDATE SET
           name=excluded.name,
           description=excluded.description,
           color=excluded.color,
           metadata_json=excluded.metadata_json,
           updated_at=excluded.updated_at`,
		p.GroupID,
		p.Name,
		p.Description,
		p.Color,
		string(metaJSON),
		now,
		now,
	)
	return err
}

func (s *sqliteStore) GetGroupProfile(ctx context.Context, groupID string) (*GroupProfile, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT group_id,name,description,color,metadata_json
         FROM group_profiles WHERE group_id=?`, groupID)
	p, err := scanGroupProfile(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *sqliteStore) ListGroupProfiles(ctx context.Context) ([]*GroupProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT group_id,name,description,color,metadata_json
         FROM group_profiles ORDER BY group_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*GroupProfile{}
	for rows.Next() {
		p, err := scanGroupProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteGroupProfile(ctx context.Context, groupID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_profiles WHERE group_id=?`, groupID)
	return err
}

// ListTanksByGroup returns all tank profiles whose group_id matches.
func (s *sqliteStore) ListTanksByGroup(ctx context.Context, groupID string) ([]*TankProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+tankProfileSelectCols+` FROM tank_profiles WHERE group_id=? ORDER BY tank_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*TankProfile{}
	for rows.Next() {
		p, err := scanTankProfileRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type groupScanner interface {
	Scan(dest ...any) error
}

func scanGroupProfile(row groupScanner) (*GroupProfile, error) {
	p := &GroupProfile{}
	var metaJSON string
	if err := row.Scan(&p.GroupID, &p.Name, &p.Description, &p.Color, &metaJSON); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(metaJSON), &p.Metadata)
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	return p, nil
}
