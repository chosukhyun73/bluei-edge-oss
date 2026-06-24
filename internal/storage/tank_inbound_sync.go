package storage

import (
	"context"
	"time"
)

// ApplyInboundEvent reflects a platform-originated event pulled DOWN from the
// cloud (GET /gx10/sync/tank-deltas) onto local state, idempotently.
//
// It records an audit event in the events table (keyed by eventID so a re-pull
// is a no-op) and, when reduceCount > 0, reduces the tank's active lifecycle
// initial_count by that amount — which is the live fish count the dashboard /
// state-vector reads. The whole thing is one transaction so the count is
// reduced exactly once per ledger row.
//
// Echo-safe by construction: eventType is intentionally NOT in
// edgeToBackendEventType, and synced_at is pre-set, so this event is never
// pushed back UP to the platform.
//
// Returns applied=false when eventID was already recorded (dedup hit).
func (s *sqliteStore) ApplyInboundEvent(
	ctx context.Context,
	eventID, eventType, tankID string,
	reduceCount int,
	payloadJSON, siteID, edgeID string,
) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO events(
		   event_id,event_type,schema_version,site_id,edge_id,recorded_at,
		   source_module,source_adapter,source_device_id,
		   payload_json,event_json,synced_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		eventID, eventType, "1.0", siteID, edgeID, now,
		"sync", "tank_deltas", tankID,
		payloadJSON, payloadJSON, now)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil // already applied — skip the count reduction
	}

	if reduceCount > 0 && tankID != "" {
		// 활성 lifecycle 의 initial_count = 라이브 마릿수(state-vector 직독). 판매분만큼
		// 줄이고, 0 이하가 되면 전량 소진으로 마감 처리한다.
		if _, err := tx.ExecContext(ctx,
			`UPDATE current_tank_lifecycle
			   SET initial_count = MAX(0, initial_count - ?),
			       status = CASE WHEN initial_count - ? <= 0 THEN 'harvested' ELSE status END,
			       updated_at = ?
			 WHERE tank_id = ? AND status = 'active'`,
			reduceCount, reduceCount, now, tankID); err != nil {
			return false, err
		}
	}

	return true, tx.Commit()
}
