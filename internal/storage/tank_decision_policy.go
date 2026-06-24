package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) GetTankDecisionPolicy(ctx context.Context, tankID string) (*TankDecisionPolicy, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tank_id,auto_execute_enabled,grace_minutes,updated_at,updated_by
         FROM current_tank_decision_policy WHERE tank_id=?`, tankID)
	return scanDecisionPolicy(row)
}

func (s *sqliteStore) UpsertTankDecisionPolicy(ctx context.Context, p *TankDecisionPolicy) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_tank_decision_policy(tank_id,auto_execute_enabled,grace_minutes,updated_at,updated_by)
         VALUES(?,?,?,?,?)
         ON CONFLICT(tank_id) DO UPDATE SET
           auto_execute_enabled=excluded.auto_execute_enabled,
           grace_minutes=excluded.grace_minutes,
           updated_at=excluded.updated_at,
           updated_by=excluded.updated_by`,
		p.TankID,
		boolToInt(p.AutoExecuteEnabled),
		p.GraceMinutes,
		fmtTime(p.UpdatedAt),
		p.UpdatedBy,
	)
	return err
}

// scanDecisionPolicy — sql.Row 단건. 행 없으면 (nil, nil).
func scanDecisionPolicy(row *sql.Row) (*TankDecisionPolicy, error) {
	p := &TankDecisionPolicy{}
	var enabled int
	var updatedAt string
	err := row.Scan(&p.TankID, &enabled, &p.GraceMinutes, &updatedAt, &p.UpdatedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.AutoExecuteEnabled = enabled != 0
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return p, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
