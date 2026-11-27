package storage

import "context"

// Certificate ledger and the schemes a plant applies. docs/54 §23.

func (s *sqliteStore) UpsertPlantCertificate(ctx context.Context, c *PlantCertificate) error {
	now := fmtNow()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO plant_certificates(cert_id,site_id,scheme,cert_no,scope,holder_kind,holder_name,
		   valid_from,valid_until,verified_at,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(cert_id) DO UPDATE SET
		   scheme=excluded.scheme, cert_no=excluded.cert_no, scope=excluded.scope,
		   holder_kind=excluded.holder_kind, holder_name=excluded.holder_name,
		   valid_from=excluded.valid_from, valid_until=excluded.valid_until, verified_at=excluded.verified_at,
		   updated_at=excluded.updated_at
		 WHERE plant_certificates.site_id=excluded.site_id`,
		c.CertID, c.SiteID, c.Scheme, c.CertNo, c.Scope, c.HolderKind, c.HolderName,
		c.ValidFrom, c.ValidUntil, c.VerifiedAt, now, now)
	return upserted(res, err)
}

func (s *sqliteStore) ListPlantCertificates(ctx context.Context, siteID string) ([]*PlantCertificate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cert_id,site_id,scheme,cert_no,scope,holder_kind,holder_name,valid_from,valid_until,verified_at
		   FROM plant_certificates WHERE site_id=? ORDER BY created_at, cert_id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlantCertificate
	for rows.Next() {
		c := &PlantCertificate{}
		if err := rows.Scan(&c.CertID, &c.SiteID, &c.Scheme, &c.CertNo, &c.Scope, &c.HolderKind, &c.HolderName,
			&c.ValidFrom, &c.ValidUntil, &c.VerifiedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SetPlantComplianceProfiles(ctx context.Context, siteID string, profileIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM plant_compliance_profiles WHERE site_id=?`, siteID); err != nil {
		return err
	}
	now := fmtNow()
	for _, id := range profileIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO plant_compliance_profiles(site_id,profile_id,created_at) VALUES(?,?,?)`,
			siteID, id, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ListPlantComplianceProfiles(ctx context.Context, siteID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT profile_id FROM plant_compliance_profiles WHERE site_id=? ORDER BY profile_id`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
