package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) InsertOperatorIntent(ctx context.Context, intent *OperatorIntent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO operator_intents(
		   intent_id, operator_id, tank_id, related_cycle_id, related_decision_id,
		   intent_type, reason, context_json, recorded_at
		 ) VALUES(?,?,?,?,?,?,?,?,?)`,
		intent.IntentID,
		intent.OperatorID,
		nullStr(intent.TankID),
		nullStr(intent.RelatedCycleID),
		nullStr(intent.RelatedDecisionID),
		intent.IntentType,
		intent.Reason,
		coalesce(intent.ContextJSON, "{}"),
		fmtTime(intent.RecordedAt),
	)
	return err
}

func (s *sqliteStore) ListOperatorIntents(ctx context.Context, tankID string, limit int) ([]*OperatorIntent, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		rows interface {
			Next() bool
			Scan(dest ...any) error
			Close() error
			Err() error
		}
		err error
	)

	if tankID != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT intent_id, COALESCE(operator_id,''), COALESCE(tank_id,''),
			        COALESCE(related_cycle_id,''), COALESCE(related_decision_id,''),
			        intent_type, reason, COALESCE(context_json,'{}'), recorded_at
			 FROM operator_intents
			 WHERE tank_id=?
			 ORDER BY recorded_at DESC LIMIT ?`, tankID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT intent_id, COALESCE(operator_id,''), COALESCE(tank_id,''),
			        COALESCE(related_cycle_id,''), COALESCE(related_decision_id,''),
			        intent_type, reason, COALESCE(context_json,'{}'), recorded_at
			 FROM operator_intents
			 ORDER BY recorded_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*OperatorIntent
	for rows.Next() {
		i := &OperatorIntent{}
		var recordedAt string
		if err := rows.Scan(
			&i.IntentID, &i.OperatorID, &i.TankID,
			&i.RelatedCycleID, &i.RelatedDecisionID,
			&i.IntentType, &i.Reason, &i.ContextJSON, &recordedAt,
		); err != nil {
			return nil, err
		}
		i.RecordedAt, _ = time.Parse(time.RFC3339Nano, recordedAt)
		out = append(out, i)
	}
	return out, rows.Err()
}

// GetOperatorIntent returns the operator intent row for the given ID, or nil if not found.
func (s *sqliteStore) GetOperatorIntent(ctx context.Context, intentID string) (*OperatorIntent, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT intent_id, COALESCE(operator_id,''), COALESCE(tank_id,''),
		        COALESCE(related_cycle_id,''), COALESCE(related_decision_id,''),
		        intent_type, reason, COALESCE(context_json,'{}'), recorded_at
		 FROM operator_intents WHERE intent_id=?`, intentID)
	i := &OperatorIntent{}
	var recordedAt string
	err := row.Scan(
		&i.IntentID, &i.OperatorID, &i.TankID,
		&i.RelatedCycleID, &i.RelatedDecisionID,
		&i.IntentType, &i.Reason, &i.ContextJSON, &recordedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	i.RecordedAt, _ = time.Parse(time.RFC3339Nano, recordedAt)
	return i, nil
}

// UpdateOperatorIntentContext — context_json 만 갱신 (Phase F.1 LLM analysis 저장용).
// Store interface 의 일부가 아니라 sqliteStore 의 부가 메서드로 둔다.
// api.operator_intents 가 type assertion 으로 호출.
func (s *sqliteStore) UpdateOperatorIntentContext(ctx context.Context, intentID, contextJSON string) error {
	if contextJSON == "" {
		contextJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE operator_intents SET context_json=? WHERE intent_id=?`,
		contextJSON, intentID)
	return err
}

// coalesce returns s if non-empty, else fallback.
func coalesce(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
