package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CollapseRedundantReadings marks all-but-the-latest unsynced telemetry synced,
// per logical stream: sensor.reading.recorded per (tank_id, metric) and
// device.health.updated per device. The backend keeps only the current value
// (env metrics bucket to 30-min slots; device health is a latest-state row), so
// sending every raw event is pure waste and overloads the slow per-event ingest.
// Collapsing to one snapshot per stream is exactly what "current state" needs.
// Superseded rows stay in the DB (retention owns their lifecycle); only the sync
// flag is set. Returns the total number of events collapsed.
func (s *sqliteStore) CollapseRedundantReadings(ctx context.Context) (int64, error) {
	var total int64

	// (a) environment readings — latest per tank + metric.
	res, err := s.db.ExecContext(ctx, `
		UPDATE events
		SET synced_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE event_type = 'sensor.reading.recorded'
		  AND synced_at IS NULL
		  AND sequence NOT IN (
		    SELECT MAX(sequence)
		    FROM events
		    WHERE event_type = 'sensor.reading.recorded' AND synced_at IS NULL
		    GROUP BY json_extract(payload_json,'$.location.tank_id'),
		             json_extract(payload_json,'$.metric')
		  )`)
	if err != nil {
		return total, err
	}
	if n, e := res.RowsAffected(); e == nil {
		total += n
	}

	// (b) device health — latest per device.
	res, err = s.db.ExecContext(ctx, `
		UPDATE events
		SET synced_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE event_type = 'device.health.updated'
		  AND synced_at IS NULL
		  AND sequence NOT IN (
		    SELECT MAX(sequence)
		    FROM events
		    WHERE event_type = 'device.health.updated' AND synced_at IS NULL
		    GROUP BY COALESCE(source_device_id, json_extract(payload_json,'$.device_id'))
		  )`)
	if err != nil {
		return total, err
	}
	if n, e := res.RowsAffected(); e == nil {
		total += n
	}

	return total, nil
}

func (s *sqliteStore) AppendEvent(ctx context.Context, e *Event) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events(event_id,event_type,schema_version,site_id,edge_id,recorded_at,
                           source_module,source_adapter,source_device_id,
                           payload_json,event_json,correlation_id,causation_id)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.EventID, e.EventType, e.SchemaVersion, e.SiteID, e.EdgeID,
		e.RecordedAt.UTC().Format(time.RFC3339Nano),
		e.SourceModule, nullStr(e.SourceAdapter), nullStr(e.SourceDevice),
		e.PayloadJSON, e.EventJSON,
		nullStr(e.CorrelationID), nullStr(e.CausationID))
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	seq, _ := res.LastInsertId()
	return seq, nil
}

func (s *sqliteStore) QueryEvents(ctx context.Context, f EventFilter) ([]*Event, error) {
	q := `SELECT sequence,event_id,event_type,schema_version,site_id,edge_id,recorded_at,
               source_module,COALESCE(source_adapter,''),COALESCE(source_device_id,''),
               payload_json,event_json,COALESCE(correlation_id,''),COALESCE(causation_id,''),synced_at
        FROM events WHERE 1=1`
	var args []any

	if f.EventType != "" {
		q += " AND event_type=?"
		args = append(args, f.EventType)
	}
	if f.DeviceID != "" {
		q += " AND source_device_id=?"
		args = append(args, f.DeviceID)
	}
	if f.EvidencePhase != "" {
		q += " AND json_extract(payload_json,'$.evidence.phase')=?"
		args = append(args, f.EvidencePhase)
	}
	if f.PayloadClipID != "" {
		q += " AND json_extract(payload_json,'$.clip_id')=?"
		args = append(args, f.PayloadClipID)
	}
	if f.PayloadCycleID != "" {
		q += " AND json_extract(payload_json,'$.cycle_id')=?"
		args = append(args, f.PayloadCycleID)
	}
	if f.Since != nil {
		q += " AND recorded_at >= ?"
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.Until != nil {
		q += " AND recorded_at <= ?"
		args = append(args, f.Until.UTC().Format(time.RFC3339Nano))
	}
	if f.AfterSeq > 0 {
		q += " AND sequence > ?"
		args = append(args, f.AfterSeq)
	}
	q += " ORDER BY sequence DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		e := &Event{}
		var recAt string
		var syncedAt sql.NullString
		if err := rows.Scan(&e.Sequence, &e.EventID, &e.EventType, &e.SchemaVersion,
			&e.SiteID, &e.EdgeID, &recAt,
			&e.SourceModule, &e.SourceAdapter, &e.SourceDevice,
			&e.PayloadJSON, &e.EventJSON,
			&e.CorrelationID, &e.CausationID, &syncedAt); err != nil {
			return nil, err
		}
		e.RecordedAt, _ = time.Parse(time.RFC3339Nano, recAt)
		if syncedAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, syncedAt.String); err == nil {
				e.SyncedAt = &t
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListUnsyncedUnbatchedEvents returns oldest-first events awaiting sync. When
// includeTypes is non-empty, only those event_types are returned — the sync
// service passes the backend-supported set so batches stay dense with events
// the backend can actually project (media.clip/snapshot/feed.* never clog the
// queue). sync.* meta events are always excluded regardless.
func (s *sqliteStore) ListUnsyncedUnbatchedEvents(ctx context.Context, includeTypes []string, limit int) ([]*Event, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	var typeClause string
	var args []any
	if len(includeTypes) > 0 {
		ph := make([]byte, 0, len(includeTypes)*2)
		for i := range includeTypes {
			if i > 0 {
				ph = append(ph, ',')
			}
			ph = append(ph, '?')
			args = append(args, includeTypes[i])
		}
		typeClause = "AND e.event_type IN (" + string(ph) + ")"
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT e.sequence,e.event_id,e.event_type,e.schema_version,e.site_id,e.edge_id,e.recorded_at,
               e.source_module,COALESCE(e.source_adapter,''),COALESCE(e.source_device_id,''),
               e.payload_json,e.event_json,COALESCE(e.correlation_id,''),COALESCE(e.causation_id,''),e.synced_at
        FROM events e
        WHERE e.synced_at IS NULL
          -- sync.* 는 배치 생성/ACK 등 sync 자기 메타이벤트다. 이걸 다시 배치에 넣으면
          -- 자기증식 루프가 되어 큐를 오염시킨다(pending 의 대다수였음). 백엔드도 미지원.
          AND e.event_type NOT LIKE 'sync.%%'
          %s
          AND NOT EXISTS (
            SELECT 1
            FROM sync_batch_events sbe
            JOIN sync_batches sb ON sb.batch_id = sbe.batch_id
            WHERE sbe.event_sequence = e.sequence
              AND sb.status IN ('created','pending','retry_scheduled','failed','sent')
          )
        ORDER BY e.sequence ASC
        LIMIT %d`, typeClause, limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]*Event, error) {
	var out []*Event
	for rows.Next() {
		e := &Event{}
		var recAt string
		var syncedAt sql.NullString
		if err := rows.Scan(&e.Sequence, &e.EventID, &e.EventType, &e.SchemaVersion,
			&e.SiteID, &e.EdgeID, &recAt,
			&e.SourceModule, &e.SourceAdapter, &e.SourceDevice,
			&e.PayloadJSON, &e.EventJSON,
			&e.CorrelationID, &e.CausationID, &syncedAt); err != nil {
			return nil, err
		}
		e.RecordedAt, _ = time.Parse(time.RFC3339Nano, recAt)
		if syncedAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, syncedAt.String); err == nil {
				e.SyncedAt = &t
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UnsyncedCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE synced_at IS NULL`).Scan(&n)
	return n, err
}

func (s *sqliteStore) OldestUnsyncedAge(ctx context.Context) (time.Duration, error) {
	var oldest sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(recorded_at) FROM events WHERE synced_at IS NULL`).Scan(&oldest)
	if err != nil || !oldest.Valid {
		return 0, err
	}
	t, err := time.Parse(time.RFC3339Nano, oldest.String)
	if err != nil {
		return 0, nil
	}
	return time.Since(t), nil
}

// MetricFilter narrows retention queries to a subset of payload metric values.
// Include 가 있으면 해당 metric 만, Exclude 가 있으면 해당 metric 을 제외한다.
// metric 필드가 없는 이벤트는 Include 필터에서 제외되고 Exclude 필터는 통과한다.
type MetricFilter struct {
	Include []string
	Exclude []string
}

// metricPredicate returns SQL predicate fragments (starting with " AND ") and
// appends the corresponding args.
func metricPredicate(f MetricFilter, args *[]any) string {
	var sb strings.Builder
	if len(f.Include) > 0 {
		sb.WriteString(" AND json_extract(payload_json,'$.metric') IN (?" + strings.Repeat(",?", len(f.Include)-1) + ")")
		for _, m := range f.Include {
			*args = append(*args, m)
		}
	}
	if len(f.Exclude) > 0 {
		sb.WriteString(" AND (json_extract(payload_json,'$.metric') IS NULL OR json_extract(payload_json,'$.metric') NOT IN (?" + strings.Repeat(",?", len(f.Exclude)-1) + "))")
		for _, m := range f.Exclude {
			*args = append(*args, m)
		}
	}
	return sb.String()
}

// SelectEventsOlderThan returns up to `limit` events of a type recorded before
// cutoff, ordered by sequence ascending. Uses idx_events_type_time.
func (s *sqliteStore) SelectEventsOlderThan(ctx context.Context, eventType string, filter MetricFilter, cutoff time.Time, limit int) ([]ArchivableEvent, error) {
	if limit <= 0 {
		limit = 5000
	}
	args := []any{eventType, cutoff.UTC().Format(time.RFC3339Nano)}
	pred := metricPredicate(filter, &args)
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT sequence, recorded_at, event_json
           FROM events
          WHERE event_type = ? AND recorded_at < ?`+pred+`
          ORDER BY sequence ASC
          LIMIT ?`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArchivableEvent
	for rows.Next() {
		var a ArchivableEvent
		var recAt string
		if err := rows.Scan(&a.Sequence, &recAt, &a.EventJSON); err != nil {
			return nil, err
		}
		a.RecordedAt, _ = time.Parse(time.RFC3339Nano, recAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteEventsOlderThanUpToSeq deletes events of a type recorded before cutoff
// whose sequence is <= maxSeq. Pairing maxSeq with the batch's last sequence
// makes the delete remove exactly the rows just archived.
func (s *sqliteStore) DeleteEventsOlderThanUpToSeq(ctx context.Context, eventType string, filter MetricFilter, cutoff time.Time, maxSeq int64) (int64, error) {
	args := []any{eventType, cutoff.UTC().Format(time.RFC3339Nano)}
	pred := metricPredicate(filter, &args)
	args = append(args, maxSeq)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM events
          WHERE event_type = ? AND recorded_at < ?`+pred+` AND sequence <= ?`,
		args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *sqliteStore) MarkSynced(ctx context.Context, eventIDs []string) error {
	now := fmtNow()
	for _, id := range eventIDs {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE events SET synced_at=? WHERE event_id=?`, now, id); err != nil {
			return err
		}
	}
	return nil
}
