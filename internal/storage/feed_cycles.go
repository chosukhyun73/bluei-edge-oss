package storage

import (
	"context"
	"database/sql"
	"time"
)

// FeedCycle은 feed_cycles 테이블의 한 행을 나타냅니다.
type FeedCycle struct {
	CycleID           string
	TankID            string
	ControllerID      string
	Mode              string  // "adaptive" | "fixed"
	TargetAmountG     float64 // adaptive 전용, 0 if fixed
	PulsesExecuted    int
	TotalAmountG      float64
	TerminationReason string
	StartedAt         time.Time
	CompletedAt       *time.Time
	ParamsJSON        string // AdaptiveParams 또는 FixedParams JSON
	VisionSummaryJSON string
	QualityImpactJSON string
	Priority          string // "manual_override" | "ai_advisory" | "ai_autonomous"
	IntentID          string // operator_intents.intent_id 연결, 빈 문자열이면 NULL 저장
	// Phase A — ESP32 모터 출력 (0 = controller default 사용 / NULL 저장)
	SpeedRpm int // 살포 모터 14~42 rpm (DAC1 GPIO 25). 0이면 ESP32 default.
	Amount   int // 공급 모터 0~255 (DAC2 GPIO 26). 0이면 ESP32 default.
	// Phase 5 (load cell) — HX711 weight event 누적. 0 이면 stub fallback (TotalAmountG).
	ActualTotalAmountG  float64
	SiloDepletionWarned bool
}

func (s *sqliteStore) InsertFeedCycle(ctx context.Context, c *FeedCycle) error {
	paramsJSON := c.ParamsJSON
	if paramsJSON == "" {
		paramsJSON = "{}"
	}
	priority := c.Priority
	if priority == "" {
		priority = "manual_override"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO feed_cycles(
		  cycle_id, tank_id, controller_id, mode, target_amount_g,
		  pulses_executed, total_amount_g, termination_reason,
		  started_at, completed_at, vision_summary_json, quality_impact_json, priority, intent_id,
		  speed_rpm, amount)
		 VALUES(?,?,?,?,?,0,0.0,NULL,?,NULL,'{}','{}',?,?,?,?)`,
		c.CycleID,
		c.TankID,
		nullStr(c.ControllerID),
		c.Mode,
		nullFloat(c.TargetAmountG),
		fmtTime(c.StartedAt),
		priority,
		nullStr(c.IntentID),
		nullInt(c.SpeedRpm),
		nullInt(c.Amount),
	)
	return err
}

// AbortOrphanFeedCycles — backend 시작 시 worker 메모리에 없는 in-flight cycle 정리.
// completed_at IS NULL 인 모든 cycle 을 강제 종료. 재가동/장애복구 후 일관성 회복.
func (s *sqliteStore) AbortOrphanFeedCycles(ctx context.Context, terminationReason string, abortedAt time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE feed_cycles
		 SET completed_at = ?, termination_reason = ?
		 WHERE completed_at IS NULL`,
		fmtTime(abortedAt), terminationReason,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *sqliteStore) UpdateFeedCycleProgress(ctx context.Context, cycleID string, pulsesExecuted int, totalAmountG float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE feed_cycles SET pulses_executed=?, total_amount_g=? WHERE cycle_id=?`,
		pulsesExecuted, totalAmountG, cycleID,
	)
	return err
}

// UpdateFeedCycleActualTotal — Phase 5 (load cell). HX711 weight event 수신 시
// 누적 실측 공급량을 갱신한다. 0 이면 컬럼은 NULL 로 두지 않고 그대로 0 저장
// (worker 가 weight event 없는 동안엔 호출하지 않음 — fallback 은 storage 단이 아니라
// dashboard/조회 단에서 처리).
func (s *sqliteStore) UpdateFeedCycleActualTotal(ctx context.Context, cycleID string, actualTotalG float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE feed_cycles SET actual_total_amount_g=? WHERE cycle_id=?`,
		actualTotalG, cycleID,
	)
	return err
}

// SetFeedCycleSiloDepletionWarned — silo 빈 감지 시 idempotent flag set.
func (s *sqliteStore) SetFeedCycleSiloDepletionWarned(ctx context.Context, cycleID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE feed_cycles SET silo_depletion_warned=1 WHERE cycle_id=?`,
		cycleID,
	)
	return err
}

func (s *sqliteStore) CompleteFeedCycle(ctx context.Context, cycleID string, pulsesExecuted int, totalAmountG float64, terminationReason string, completedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE feed_cycles
		 SET pulses_executed=?, total_amount_g=?, termination_reason=?, completed_at=?
		 WHERE cycle_id=?`,
		pulsesExecuted, totalAmountG, terminationReason, fmtTime(completedAt), cycleID,
	)
	return err
}

func (s *sqliteStore) GetFeedCycle(ctx context.Context, cycleID string) (*FeedCycle, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT cycle_id, tank_id, COALESCE(controller_id,''), mode,
		        COALESCE(target_amount_g,0), pulses_executed, total_amount_g,
		        COALESCE(termination_reason,''),
		        started_at, completed_at,
		        COALESCE(vision_summary_json,'{}'), COALESCE(quality_impact_json,'{}'),
		        COALESCE(priority,'manual_override'),
		        COALESCE(intent_id,''),
		        COALESCE(speed_rpm,0),
		        COALESCE(amount,0),
		        COALESCE(actual_total_amount_g,0),
		        COALESCE(silo_depletion_warned,0)
		 FROM feed_cycles WHERE cycle_id=?`, cycleID)
	return scanFeedCycle(row)
}

func (s *sqliteStore) ListActiveFeedCycles(ctx context.Context) ([]*FeedCycle, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cycle_id, tank_id, COALESCE(controller_id,''), mode,
		        COALESCE(target_amount_g,0), pulses_executed, total_amount_g,
		        COALESCE(termination_reason,''),
		        started_at, completed_at,
		        COALESCE(vision_summary_json,'{}'), COALESCE(quality_impact_json,'{}'),
		        COALESCE(priority,'manual_override'),
		        COALESCE(intent_id,''),
		        COALESCE(speed_rpm,0),
		        COALESCE(amount,0),
		        COALESCE(actual_total_amount_g,0),
		        COALESCE(silo_depletion_warned,0)
		 FROM feed_cycles WHERE completed_at IS NULL ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFeedCycles(rows)
}

func (s *sqliteStore) ListRecentFeedCycles(ctx context.Context, tankID string, limit int) ([]*FeedCycle, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT cycle_id, tank_id, COALESCE(controller_id,''), mode,
		        COALESCE(target_amount_g,0), pulses_executed, total_amount_g,
		        COALESCE(termination_reason,''),
		        started_at, completed_at,
		        COALESCE(vision_summary_json,'{}'), COALESCE(quality_impact_json,'{}'),
		        COALESCE(priority,'manual_override'),
		        COALESCE(intent_id,''),
		        COALESCE(speed_rpm,0),
		        COALESCE(amount,0),
		        COALESCE(actual_total_amount_g,0),
		        COALESCE(silo_depletion_warned,0)
		 FROM feed_cycles WHERE tank_id=? ORDER BY started_at DESC LIMIT ?`,
		tankID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFeedCycles(rows)
}

func scanFeedCycle(row *sql.Row) (*FeedCycle, error) {
	c := &FeedCycle{}
	var startedAt, targetAmountG string
	var completedAt sql.NullString
	var siloWarnedInt int
	err := row.Scan(
		&c.CycleID, &c.TankID, &c.ControllerID, &c.Mode,
		&targetAmountG,
		&c.PulsesExecuted, &c.TotalAmountG,
		&c.TerminationReason,
		&startedAt, &completedAt,
		&c.VisionSummaryJSON, &c.QualityImpactJSON,
		&c.Priority,
		&c.IntentID,
		&c.SpeedRpm,
		&c.Amount,
		&c.ActualTotalAmountG,
		&siloWarnedInt,
	)
	c.SiloDepletionWarned = siloWarnedInt != 0
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completedAt.String)
		c.CompletedAt = &t
	}
	return c, nil
}

func scanFeedCycles(rows *sql.Rows) ([]*FeedCycle, error) {
	var out []*FeedCycle
	for rows.Next() {
		c := &FeedCycle{}
		var startedAt, targetAmountG string
		var completedAt sql.NullString
		var siloWarnedInt int
		if err := rows.Scan(
			&c.CycleID, &c.TankID, &c.ControllerID, &c.Mode,
			&targetAmountG,
			&c.PulsesExecuted, &c.TotalAmountG,
			&c.TerminationReason,
			&startedAt, &completedAt,
			&c.VisionSummaryJSON, &c.QualityImpactJSON,
			&c.Priority,
			&c.IntentID,
			&c.SpeedRpm,
			&c.Amount,
			&c.ActualTotalAmountG,
			&siloWarnedInt,
		); err != nil {
			return nil, err
		}
		c.SiloDepletionWarned = siloWarnedInt != 0
		c.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, completedAt.String)
			c.CompletedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
