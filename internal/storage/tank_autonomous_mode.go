package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) GetTankAutonomousMode(ctx context.Context, tankID string) (*TankAutonomousMode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tank_id,mode,COALESCE(reason,''),changed_at,changed_by
         FROM current_tank_autonomous_mode WHERE tank_id=?`, tankID)
	return scanAutonomousMode(row)
}

func (s *sqliteStore) UpsertTankAutonomousMode(ctx context.Context, mode *TankAutonomousMode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_tank_autonomous_mode(tank_id,mode,reason,changed_at,changed_by)
         VALUES(?,?,?,?,?)
         ON CONFLICT(tank_id) DO UPDATE SET
           mode=excluded.mode,
           reason=excluded.reason,
           changed_at=excluded.changed_at,
           changed_by=excluded.changed_by`,
		mode.TankID,
		mode.Mode,
		nullStr(mode.Reason),
		fmtTime(mode.ChangedAt),
		mode.ChangedBy,
	)
	return err
}

func (s *sqliteStore) ListTankAutonomousModes(ctx context.Context) ([]*TankAutonomousMode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tank_id,mode,COALESCE(reason,''),changed_at,changed_by
         FROM current_tank_autonomous_mode ORDER BY tank_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TankAutonomousMode
	for rows.Next() {
		m, err := scanAutonomousModeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// scanAutonomousMode — sql.Row (단건). 행 없으면 (nil, nil) — GetTankProfile 과 동일 패턴.
func scanAutonomousMode(row *sql.Row) (*TankAutonomousMode, error) {
	m := &TankAutonomousMode{}
	var changedAt string
	err := row.Scan(&m.TankID, &m.Mode, &m.Reason, &changedAt, &m.ChangedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.ChangedAt, _ = time.Parse(time.RFC3339Nano, changedAt)
	return m, nil
}

func scanAutonomousModeRows(rows scanner) (*TankAutonomousMode, error) {
	m := &TankAutonomousMode{}
	var changedAt string
	if err := rows.Scan(&m.TankID, &m.Mode, &m.Reason, &changedAt, &m.ChangedBy); err != nil {
		return nil, err
	}
	m.ChangedAt, _ = time.Parse(time.RFC3339Nano, changedAt)
	return m, nil
}
