package storage

import (
	"context"
	"time"
)

// OperatorDispute is a row in operator_disputes.
type OperatorDispute struct {
	DisputeID   string    `json:"dispute_id"`
	DecisionID  string    `json:"decision_id"`
	TankID      string    `json:"tank_id"`
	DisputeType string    `json:"dispute_type"`
	Comment     string    `json:"comment"`
	DisputedAt  time.Time `json:"disputed_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *sqliteStore) InsertOperatorDispute(ctx context.Context, d *OperatorDispute) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO operator_disputes(dispute_id,decision_id,tank_id,dispute_type,comment,disputed_at,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		d.DisputeID, d.DecisionID, d.TankID, d.DisputeType,
		d.Comment, fmtTime(d.DisputedAt), fmtTime(d.CreatedAt),
	)
	return err
}

func (s *sqliteStore) ListOperatorDisputes(ctx context.Context, limit int) ([]*OperatorDispute, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT dispute_id,decision_id,tank_id,dispute_type,
		        COALESCE(comment,''),disputed_at,created_at
		 FROM operator_disputes ORDER BY disputed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*OperatorDispute
	for rows.Next() {
		d := &OperatorDispute{}
		var disputedAt, createdAt string
		if err := rows.Scan(&d.DisputeID, &d.DecisionID, &d.TankID, &d.DisputeType,
			&d.Comment, &disputedAt, &createdAt); err != nil {
			return nil, err
		}
		d.DisputedAt, _ = time.Parse(time.RFC3339Nano, disputedAt)
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, d)
	}
	return out, rows.Err()
}
