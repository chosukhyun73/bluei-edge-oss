package storage

import (
	"context"
	"time"
)

func (s *sqliteStore) InsertArbiterDecision(ctx context.Context, d *ArbiterDecision) error {
	accepted := 0
	if d.Accepted {
		accepted = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO arbiter_decisions(
		   decision_id, tank_id, source, priority,
		   accepted, rejection_reason, existing_cycle_id, resulting_cycle_id,
		   intent_id, submitted_at, decided_at, preempted_cycle_id
		 ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.DecisionID,
		d.TankID,
		d.Source,
		d.Priority,
		accepted,
		nullStr(d.RejectionReason),
		nullStr(d.ExistingCycleID),
		nullStr(d.ResultingCycleID),
		nullStr(d.IntentID),
		fmtTime(d.SubmittedAt),
		fmtTime(d.DecidedAt),
		nullStr(d.PreemptedCycleID),
	)
	return err
}

// GetArbiterDecisionByCycleID returns the arbiter_decisions row whose
// resulting_cycle_id matches cycleID, or nil if none exists.
//
// 사용처: feed_cycles view serializer (C-3l). cycle 응답에 decision_id 를 echo
// 하여 dashboard 의 운영자 dispute 입력이 decision_id 를 손쉽게 첨부할 수 있도록 함.
func (s *sqliteStore) GetArbiterDecisionByCycleID(ctx context.Context, cycleID string) (*ArbiterDecision, error) {
	if cycleID == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT decision_id, tank_id, source, priority,
		        accepted, COALESCE(rejection_reason,''), COALESCE(existing_cycle_id,''),
		        COALESCE(resulting_cycle_id,''), COALESCE(intent_id,''),
		        submitted_at, decided_at, COALESCE(preempted_cycle_id,'')
		 FROM arbiter_decisions
		 WHERE resulting_cycle_id=?
		 ORDER BY decided_at DESC LIMIT 1`, cycleID)
	d := &ArbiterDecision{}
	var accepted int
	var submittedAt, decidedAt string
	err := row.Scan(
		&d.DecisionID, &d.TankID, &d.Source, &d.Priority,
		&accepted, &d.RejectionReason, &d.ExistingCycleID,
		&d.ResultingCycleID, &d.IntentID,
		&submittedAt, &decidedAt, &d.PreemptedCycleID,
	)
	if err != nil {
		// sql.ErrNoRows → nil, nil (caller-friendly)
		return nil, nil
	}
	d.Accepted = accepted == 1
	d.SubmittedAt, _ = time.Parse(time.RFC3339Nano, submittedAt)
	d.DecidedAt, _ = time.Parse(time.RFC3339Nano, decidedAt)
	return d, nil
}

func (s *sqliteStore) ListArbiterDecisions(ctx context.Context, tankID string, limit int) ([]*ArbiterDecision, error) {
	if limit <= 0 {
		limit = 50
	}

	var (
		sqlRows interface {
			Next() bool
			Scan(dest ...any) error
			Close() error
			Err() error
		}
		err error
	)

	if tankID != "" {
		sqlRows, err = s.db.QueryContext(ctx,
			`SELECT decision_id, tank_id, source, priority,
			        accepted, COALESCE(rejection_reason,''), COALESCE(existing_cycle_id,''),
			        COALESCE(resulting_cycle_id,''), COALESCE(intent_id,''),
			        submitted_at, decided_at, COALESCE(preempted_cycle_id,'')
			 FROM arbiter_decisions
			 WHERE tank_id=?
			 ORDER BY decided_at DESC LIMIT ?`, tankID, limit)
	} else {
		sqlRows, err = s.db.QueryContext(ctx,
			`SELECT decision_id, tank_id, source, priority,
			        accepted, COALESCE(rejection_reason,''), COALESCE(existing_cycle_id,''),
			        COALESCE(resulting_cycle_id,''), COALESCE(intent_id,''),
			        submitted_at, decided_at, COALESCE(preempted_cycle_id,'')
			 FROM arbiter_decisions
			 ORDER BY decided_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var out []*ArbiterDecision
	for sqlRows.Next() {
		d := &ArbiterDecision{}
		var accepted int
		var submittedAt, decidedAt string
		if err := sqlRows.Scan(
			&d.DecisionID, &d.TankID, &d.Source, &d.Priority,
			&accepted, &d.RejectionReason, &d.ExistingCycleID,
			&d.ResultingCycleID, &d.IntentID,
			&submittedAt, &decidedAt, &d.PreemptedCycleID,
		); err != nil {
			return nil, err
		}
		d.Accepted = accepted == 1
		d.SubmittedAt, _ = time.Parse(time.RFC3339Nano, submittedAt)
		d.DecidedAt, _ = time.Parse(time.RFC3339Nano, decidedAt)
		out = append(out, d)
	}
	return out, sqlRows.Err()
}
