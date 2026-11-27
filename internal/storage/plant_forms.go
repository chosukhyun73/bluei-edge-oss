package storage

import (
	"context"
	"database/sql"
)

// Plant record forms. docs/56 §18.

const plantFormCols = `site_id,form_id,revision,source,base_pack,base_id,content,note,created_by,created_at`

func scanPlantForm(sc rowScanner) (*PlantForm, error) {
	f := &PlantForm{}
	err := sc.Scan(&f.SiteID, &f.FormID, &f.Revision, &f.Source, &f.BasePack, &f.BaseID, &f.Content, &f.Note, &f.CreatedBy, &f.CreatedAt)
	return f, err
}

func (s *sqliteStore) InsertPlantForm(ctx context.Context, f *PlantForm) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO plant_forms(`+plantFormCols+`)
		 VALUES(?,?,(SELECT COALESCE(MAX(revision),0)+1 FROM plant_forms WHERE site_id=? AND form_id=?),?,?,?,?,?,?,?)
		 RETURNING revision`,
		f.SiteID, f.FormID, f.SiteID, f.FormID, f.Source, f.BasePack, f.BaseID, f.Content, f.Note, f.CreatedBy, f.CreatedAt,
	).Scan(&f.Revision)
}

func (s *sqliteStore) GetPlantForm(ctx context.Context, siteID, formID string) (*PlantForm, error) {
	f, err := scanPlantForm(s.db.QueryRowContext(ctx,
		`SELECT `+plantFormCols+` FROM plant_forms WHERE site_id=? AND form_id=? ORDER BY revision DESC LIMIT 1`, siteID, formID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return f, err
}

func (s *sqliteStore) ListPlantForms(ctx context.Context, siteID string) ([]*PlantForm, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+plantFormCols+` FROM plant_forms f WHERE site_id=?
		   AND revision=(SELECT MAX(revision) FROM plant_forms WHERE site_id=f.site_id AND form_id=f.form_id)
		 ORDER BY (SELECT MIN(rowid) FROM plant_forms WHERE site_id=f.site_id AND form_id=f.form_id)`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlantForm
	for rows.Next() {
		f, err := scanPlantForm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
