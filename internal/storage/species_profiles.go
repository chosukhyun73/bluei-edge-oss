package storage

import (
	"context"
	"encoding/json"

	"bluei.kr/edge/internal/species"
)

// UpsertSpeciesProfile inserts or updates a species profile row.
func (s *sqliteStore) UpsertSpeciesProfile(ctx context.Context, key string, p *species.Profile) error {
	stagesJSON, err := json.Marshal(p.LifecycleStages)
	if err != nil {
		return err
	}
	wasteJSON, err := json.Marshal(p.WasteModel)
	if err != nil {
		return err
	}
	src := p.Source
	if src == "" {
		src = "default"
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO species_profiles(species,display_name,lifecycle_stages_json,waste_model_json,source,created_at,updated_at,fao_asfis_code,scientific_name)
         VALUES(?,?,?,?,?,?,?,?,?)
         ON CONFLICT(species) DO UPDATE SET
           display_name=excluded.display_name,
           lifecycle_stages_json=excluded.lifecycle_stages_json,
           waste_model_json=excluded.waste_model_json,
           source=excluded.source,
           updated_at=excluded.updated_at,
           fao_asfis_code=excluded.fao_asfis_code,
           scientific_name=excluded.scientific_name`,
		key,
		p.DisplayName,
		string(stagesJSON),
		string(wasteJSON),
		src,
		fmtNow(),
		fmtNow(),
		nullStr(p.FAOASFISCode),
		nullStr(p.ScientificName),
	)
	return err
}

// DeleteSpeciesProfile removes a species_profiles row by species key.
func (s *sqliteStore) DeleteSpeciesProfile(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM species_profiles WHERE species=?`, key)
	return err
}

// SpeciesProfileExists — 존재 여부.
func (s *sqliteStore) SpeciesProfileExists(ctx context.Context, key string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM species_profiles WHERE species=?`, key).Scan(&n)
	return n > 0, err
}

// CountTanksForSpecies — 어종을 사용 중인 tank 수.
func (s *sqliteStore) CountTanksForSpecies(ctx context.Context, key string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tank_profiles WHERE species=?`, key).Scan(&n)
	return n, err
}

// ListSpeciesProfiles returns all species profiles as raw maps (species key included).
func (s *sqliteStore) ListSpeciesProfiles(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT species,display_name,lifecycle_stages_json,waste_model_json,source,
		        COALESCE(fao_asfis_code,''),COALESCE(scientific_name,'')
		 FROM species_profiles ORDER BY species`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var sp, displayName, src, stagesJSON, wasteJSON, faoCode, sciName string
		if err := rows.Scan(&sp, &displayName, &stagesJSON, &wasteJSON, &src, &faoCode, &sciName); err != nil {
			return nil, err
		}
		var stages map[string]any
		var waste map[string]any
		_ = json.Unmarshal([]byte(stagesJSON), &stages)
		_ = json.Unmarshal([]byte(wasteJSON), &waste)
		out = append(out, map[string]any{
			"species":          sp,
			"display_name":     displayName,
			"lifecycle_stages": stages,
			"waste_model":      waste,
			"source":           src,
			"fao_asfis_code":   faoCode,
			"scientific_name":  sciName,
		})
	}
	return out, rows.Err()
}
