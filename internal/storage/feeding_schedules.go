package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// FeedingSchedulePattern mirrors the JSON stored in pattern_json.
type FeedingSchedulePattern struct {
	PulseDurationMs int      `json:"pulse_duration_ms"`
	GapMs           int      `json:"gap_ms"`
	TotalPulses     int      `json:"total_pulses"`
	TargetAmountG   *float64 `json:"target_amount_g,omitempty"`
}

// FeedingSchedule is a row from the feeding_schedules table.
type FeedingSchedule struct {
	ScheduleID string
	TankIDs    []string // decoded from tank_ids_json
	Cron       string
	Times      []string // decoded from times_json
	Pattern    FeedingSchedulePattern
	Priority   string // "manual_override" | "ai_advisory"
	SafetyGate bool
	Enabled    bool
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// FireTimes returns today's HH:MM fire times from Times or Cron.
// Cron parsing is delegated to the schedule package; here we just return the
// stored times list (cron is stored as-is; the schedule worker calls FireTimes
// on its own schedule.Schedule wrapper). This method returns the stored times.
func (s *FeedingSchedule) FireTimes() []string {
	return s.Times
}

func (s *sqliteStore) UpsertSchedule(ctx context.Context, sched *FeedingSchedule) error {
	tankIDs, err := json.Marshal(sched.TankIDs)
	if err != nil {
		return err
	}
	times, err := json.Marshal(sched.Times)
	if err != nil {
		return err
	}
	pattern, err := json.Marshal(sched.Pattern)
	if err != nil {
		return err
	}
	safetyGate := 0
	if sched.SafetyGate {
		safetyGate = 1
	}
	enabled := 0
	if sched.Enabled {
		enabled = 1
	}
	now := fmtNow()
	if sched.CreatedAt.IsZero() {
		sched.CreatedAt = time.Now().UTC()
	}
	createdAt := fmtTime(sched.CreatedAt)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO feeding_schedules(
		   schedule_id, tank_ids_json, cron, times_json, pattern_json,
		   priority, safety_gate, enabled, created_by, created_at, updated_at
		 ) VALUES(?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(schedule_id) DO UPDATE SET
		   tank_ids_json=excluded.tank_ids_json,
		   cron=excluded.cron,
		   times_json=excluded.times_json,
		   pattern_json=excluded.pattern_json,
		   priority=excluded.priority,
		   safety_gate=excluded.safety_gate,
		   enabled=excluded.enabled,
		   created_by=excluded.created_by,
		   updated_at=excluded.updated_at`,
		sched.ScheduleID,
		string(tankIDs),
		nullStr(sched.Cron),
		string(times),
		string(pattern),
		sched.Priority,
		safetyGate,
		enabled,
		sched.CreatedBy,
		createdAt,
		now,
	)
	return err
}

func (s *sqliteStore) ListEnabledSchedules(ctx context.Context) ([]*FeedingSchedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT schedule_id, tank_ids_json, COALESCE(cron,''), COALESCE(times_json,'[]'),
		        pattern_json, priority, safety_gate, enabled, created_by, created_at, updated_at
		 FROM feeding_schedules WHERE enabled=1 ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSchedules(rows)
}

func (s *sqliteStore) ListAllSchedules(ctx context.Context) ([]*FeedingSchedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT schedule_id, tank_ids_json, COALESCE(cron,''), COALESCE(times_json,'[]'),
		        pattern_json, priority, safety_gate, enabled, created_by, created_at, updated_at
		 FROM feeding_schedules ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSchedules(rows)
}

func (s *sqliteStore) GetSchedule(ctx context.Context, scheduleID string) (*FeedingSchedule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT schedule_id, tank_ids_json, COALESCE(cron,''), COALESCE(times_json,'[]'),
		        pattern_json, priority, safety_gate, enabled, created_by, created_at, updated_at
		 FROM feeding_schedules WHERE schedule_id=?`, scheduleID)
	sched, err := scanScheduleRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return sched, err
}

func (s *sqliteStore) DeleteSchedule(ctx context.Context, scheduleID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM feeding_schedules WHERE schedule_id=?`, scheduleID)
	return err
}

func (s *sqliteStore) SetScheduleEnabled(ctx context.Context, scheduleID string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE feeding_schedules SET enabled=?, updated_at=? WHERE schedule_id=?`,
		v, fmtNow(), scheduleID)
	return err
}

// --- scan helpers ---

func scanSchedules(rows *sql.Rows) ([]*FeedingSchedule, error) {
	var out []*FeedingSchedule
	for rows.Next() {
		sched, err := scanScheduleRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sched)
	}
	return out, rows.Err()
}

// scanScheduleRow accepts either *sql.Row or *sql.Rows via interface.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanScheduleRow(row rowScanner) (*FeedingSchedule, error) {
	var (
		scheduleID, tankIDsJSON, cron, timesJSON, patternJSON string
		priority, createdBy, createdAtStr, updatedAtStr       string
		safetyGate, enabled                                   int
	)
	err := row.Scan(
		&scheduleID, &tankIDsJSON, &cron, &timesJSON,
		&patternJSON, &priority, &safetyGate, &enabled,
		&createdBy, &createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	sched := &FeedingSchedule{
		ScheduleID: scheduleID,
		Cron:       cron,
		Priority:   priority,
		SafetyGate: safetyGate == 1,
		Enabled:    enabled == 1,
		CreatedBy:  createdBy,
	}
	sched.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	sched.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)

	_ = json.Unmarshal([]byte(tankIDsJSON), &sched.TankIDs)
	_ = json.Unmarshal([]byte(timesJSON), &sched.Times)
	_ = json.Unmarshal([]byte(patternJSON), &sched.Pattern)

	if sched.TankIDs == nil {
		sched.TankIDs = []string{}
	}
	if sched.Times == nil {
		sched.Times = []string{}
	}
	return sched, nil
}
