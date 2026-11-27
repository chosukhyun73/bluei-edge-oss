package storage

import (
	"context"
	"database/sql"
	"time"
)

// HACCP plans and versions (가공공장 공정도·CCP). docs/54.

func (s *sqliteStore) GetSiteType(ctx context.Context, siteID string) (string, error) {
	var t string
	err := s.db.QueryRowContext(ctx, `SELECT site_type FROM sites WHERE site_id=?`, siteID).Scan(&t)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return t, err
}

// CreateHACCPPlan inserts a plan together with its first draft version.
func (s *sqliteStore) CreateHACCPPlan(ctx context.Context, p *HACCPPlan, first *HACCPPlanVersion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO haccp_plans(plan_id,site_id,product_group,name,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		p.PlanID, p.SiteID, p.ProductGroup, p.Name, fmtTime(p.CreatedAt), fmtTime(p.UpdatedAt),
	); err != nil {
		return err
	}
	if err := insertHACCPPlanVersion(ctx, tx, first); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) GetHACCPPlan(ctx context.Context, planID string) (*HACCPPlan, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT plan_id,site_id,product_group,name,created_at,updated_at FROM haccp_plans WHERE plan_id=?`, planID)
	p, err := scanHACCPPlan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (s *sqliteStore) ListHACCPPlans(ctx context.Context, siteID string) ([]*HACCPPlan, error) {
	q := `SELECT plan_id,site_id,product_group,name,created_at,updated_at FROM haccp_plans`
	args := []any{}
	if siteID != "" {
		q += ` WHERE site_id=?`
		args = append(args, siteID)
	}
	q += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*HACCPPlan
	for rows.Next() {
		p, err := scanHACCPPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanHACCPPlan(sc rowScanner) (*HACCPPlan, error) {
	p := &HACCPPlan{}
	var createdAt, updatedAt string
	if err := sc.Scan(&p.PlanID, &p.SiteID, &p.ProductGroup, &p.Name, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return p, nil
}

const haccpVersionCols = `version_id,plan_id,version_no,status,template_ref,content_json,content_hash,change_reason,
	ccp_changed,onsite_confirmed_by,onsite_confirmed_at,published_by,published_at,created_at,updated_at`

func (s *sqliteStore) ListHACCPPlanVersions(ctx context.Context, planID string) ([]*HACCPPlanVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+haccpVersionCols+` FROM haccp_plan_versions WHERE plan_id=? ORDER BY version_no`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*HACCPPlanVersion
	for rows.Next() {
		v, err := scanHACCPPlanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetHACCPPlanVersion(ctx context.Context, versionID string) (*HACCPPlanVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+haccpVersionCols+` FROM haccp_plan_versions WHERE version_id=?`, versionID)
	v, err := scanHACCPPlanVersion(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func scanHACCPPlanVersion(sc rowScanner) (*HACCPPlanVersion, error) {
	v := &HACCPPlanVersion{}
	var ccpChanged int
	var confirmedAt, publishedAt *string
	var createdAt, updatedAt string
	if err := sc.Scan(&v.VersionID, &v.PlanID, &v.VersionNo, &v.Status, &v.TemplateRef, &v.ContentJSON,
		&v.ContentHash, &v.ChangeReason, &ccpChanged, &v.OnsiteConfirmedBy, &confirmedAt,
		&v.PublishedBy, &publishedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	v.CCPChanged = ccpChanged != 0
	if confirmedAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *confirmedAt); err == nil {
			v.OnsiteConfirmedAt = &t
		}
	}
	if publishedAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *publishedAt); err == nil {
			v.PublishedAt = &t
		}
	}
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	v.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return v, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertHACCPPlanVersion(ctx context.Context, x execer, v *HACCPPlanVersion) error {
	_, err := x.ExecContext(ctx,
		`INSERT INTO haccp_plan_versions(version_id,plan_id,version_no,status,template_ref,content_json,
		   content_hash,change_reason,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		v.VersionID, v.PlanID, v.VersionNo, v.Status, v.TemplateRef, v.ContentJSON,
		v.ContentHash, v.ChangeReason, fmtTime(v.CreatedAt), fmtTime(v.UpdatedAt))
	return err
}

func (s *sqliteStore) InsertHACCPPlanVersion(ctx context.Context, v *HACCPPlanVersion) error {
	return insertHACCPPlanVersion(ctx, s.db, v)
}

func (s *sqliteStore) UpdateHACCPDraftContent(ctx context.Context, versionID, contentJSON string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE haccp_plan_versions SET content_json=?, updated_at=? WHERE version_id=? AND status='draft'`,
		contentJSON, fmtNow(), versionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *sqliteStore) PublishHACCPPlanVersion(ctx context.Context, v *HACCPPlanVersion) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// 직전 게시본은 폐기 상태로 — 내용은 그대로 남는다.
	if _, err := tx.ExecContext(ctx,
		`UPDATE haccp_plan_versions SET status='retired', updated_at=? WHERE plan_id=? AND status='published'`,
		fmtNow(), v.PlanID); err != nil {
		return false, err
	}
	ccpChanged := 0
	if v.CCPChanged {
		ccpChanged = 1
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE haccp_plan_versions
		    SET status='published', content_hash=?, ccp_changed=?, onsite_confirmed_by=?, onsite_confirmed_at=?,
		        published_by=?, published_at=?, updated_at=?
		  WHERE version_id=? AND status='draft'`,
		v.ContentHash, ccpChanged, v.OnsiteConfirmedBy, fmtTimePtr(v.OnsiteConfirmedAt),
		v.PublishedBy, fmtTimePtr(v.PublishedAt), fmtNow(), v.VersionID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, nil
	}
	return true, tx.Commit()
}
