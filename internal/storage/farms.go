package storage

import (
	"context"
	"encoding/json"

	"bluei.kr/edge/internal/farm"
)

// UpsertFarm inserts or updates a farm row.
func (s *sqliteStore) UpsertFarm(ctx context.Context, f *farm.Farm) error {
	certsJSON, err := json.Marshal(f.Certifications)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(f.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO farms(farm_id,license_no,operator,certifications_json,metadata_json,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?)
         ON CONFLICT(farm_id) DO UPDATE SET
           license_no=excluded.license_no,
           operator=excluded.operator,
           certifications_json=excluded.certifications_json,
           metadata_json=excluded.metadata_json,
           updated_at=excluded.updated_at`,
		f.FarmID,
		f.LicenseNo,
		f.Operator,
		string(certsJSON),
		string(metaJSON),
		fmtNow(),
		fmtNow(),
	)
	return err
}

// DeleteFarm removes a farm row by farm_id.
// FK 의존성 (sites) 은 호출자가 사전에 검증 — 여기는 단순 row 삭제.
func (s *sqliteStore) DeleteFarm(ctx context.Context, farmID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM farms WHERE farm_id=?`, farmID)
	return err
}

// GetFarm returns a single farm row, or nil if not found.
func (s *sqliteStore) GetFarm(ctx context.Context, farmID string) (*farm.Farm, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT farm_id,license_no,operator,certifications_json,metadata_json FROM farms WHERE farm_id=?`, farmID)
	var f farm.Farm
	var certsJSON, metaJSON string
	if err := row.Scan(&f.FarmID, &f.LicenseNo, &f.Operator, &certsJSON, &metaJSON); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(certsJSON), &f.Certifications)
	_ = json.Unmarshal([]byte(metaJSON), &f.Metadata)
	return &f, nil
}

// CountSitesForFarm — 자식 사이트 수 (FK 의존성 reject 용).
func (s *sqliteStore) CountSitesForFarm(ctx context.Context, farmID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites WHERE farm_id=?`, farmID).Scan(&n)
	return n, err
}

// ListFarms returns all farms ordered by farm_id.
func (s *sqliteStore) ListFarms(ctx context.Context) ([]*farm.Farm, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT farm_id,license_no,operator,certifications_json,metadata_json FROM farms ORDER BY farm_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*farm.Farm
	for rows.Next() {
		var f farm.Farm
		var certsJSON, metaJSON string
		if err := rows.Scan(&f.FarmID, &f.LicenseNo, &f.Operator, &certsJSON, &metaJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(certsJSON), &f.Certifications)
		_ = json.Unmarshal([]byte(metaJSON), &f.Metadata)
		out = append(out, &f)
	}
	return out, rows.Err()
}
