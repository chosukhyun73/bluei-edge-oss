package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *sqliteStore) UpsertPartner(ctx context.Context, p *Partner) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO partners(
		   partner_id,partner_type,name,business_no,license_no,contact,address,gln,notes,site_id,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(partner_id) DO UPDATE SET
		   partner_type=excluded.partner_type,
		   name=excluded.name,
		   business_no=excluded.business_no,
		   license_no=excluded.license_no,
		   contact=excluded.contact,
		   address=excluded.address,
		   gln=excluded.gln,
		   notes=excluded.notes,
		   site_id=excluded.site_id,
		   updated_at=excluded.updated_at`,
		p.PartnerID,
		p.PartnerType,
		p.Name,
		nullStr(p.BusinessNo),
		nullStr(p.LicenseNo),
		nullStr(p.Contact),
		nullStr(p.Address),
		nullStr(p.GLN),
		nullStr(p.Notes),
		nullStr(p.SiteID),
		fmtTime(p.CreatedAt),
		fmtTime(p.UpdatedAt),
	)
	return err
}

const partnerCols = `partner_id,partner_type,name,COALESCE(business_no,''),COALESCE(license_no,''),
		        COALESCE(contact,''),COALESCE(address,''),COALESCE(gln,''),COALESCE(notes,''),COALESCE(site_id,''),created_at,updated_at`

func (s *sqliteStore) GetPartner(ctx context.Context, partnerID string) (*Partner, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+partnerCols+` FROM partners WHERE partner_id=?`, partnerID)
	p, err := scanPartnerRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *sqliteStore) ListPartners(ctx context.Context, partnerType, siteID string) ([]*Partner, error) {
	query := `SELECT ` + partnerCols + ` FROM partners`
	var conds []string
	args := []any{}
	if partnerType != "" {
		conds = append(conds, "partner_type=?")
		args = append(args, partnerType)
	}
	if siteID != "" {
		conds = append(conds, "(site_id=? OR site_id IS NULL OR site_id='')")
		args = append(args, siteID)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Partner
	for rows.Next() {
		p, scanErr := scanPartnerRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPartnerRow(sc rowScanner) (*Partner, error) {
	p := &Partner{}
	var createdAt, updatedAt string
	if err := sc.Scan(
		&p.PartnerID, &p.PartnerType, &p.Name, &p.BusinessNo, &p.LicenseNo,
		&p.Contact, &p.Address, &p.GLN, &p.Notes, &p.SiteID, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return p, nil
}
