package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies PRAGMAs.
func Open(path string) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	// modernc/sqlite expects `_pragma=...` DSN params; the older `_journal_mode=`
	// style is silently ignored on this driver. Each pragma below is applied
	// per-connection when the pool opens a new conn.
	//
	// busy_timeout=5000 lets SQLite wait up to 5s for a writer's lock before
	// returning SQLITE_BUSY. This is the inner SQLite-level wait — distinct
	// from the Go database/sql pool wait, which is governed by MaxOpenConns.
	dsn := path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=temp_store(MEMORY)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// WAL allows multiple concurrent readers + one writer. With MaxOpenConns=1
	// every read serializes behind any in-flight write (e.g. the sync worker's
	// 46k-row scan), causing API routes to hang under load. Allow a small pool
	// so reads keep flowing while a long write/scan holds one connection.
	//
	// Phase A.1 — read 경합 완화. SQLite 는 read 동시성 안전, conn 늘려도 OK.
	// 4 tank × 10 collect 병렬 (= 최대 40 read) 을 받아내려면 8 → 16 으로 상향.
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return &sqliteStore{db: db}, nil
}

// Migrate reads and executes the SQL migration file.
// schema_migrations 테이블로 적용 추적 — 이미 적용된 migration 은 skip.
// non-idempotent SQL (ALTER TABLE 등) 도 안전하게 처리.
func Migrate(s Store, sqlPath string) error {
	ss := s.(*sqliteStore)

	if _, err := ss.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	var count int
	if err := ss.db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE name=?`, sqlPath,
	).Scan(&count); err != nil {
		return fmt.Errorf("check schema_migrations: %w", err)
	}
	if count > 0 {
		return nil
	}

	data, err := os.ReadFile(sqlPath)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", sqlPath, err)
	}
	if _, err := ss.db.Exec(string(data)); err != nil {
		return fmt.Errorf("apply migration: %w", err)
	}
	if _, err := ss.db.Exec(
		`INSERT INTO schema_migrations(name, applied_at) VALUES (?, datetime('now'))`,
		sqlPath,
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}
	return nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// KV ---

func (s *sqliteStore) KVGet(ctx context.Context, key string) (string, bool, error) {
	var val string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM runtime_kv WHERE key=?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return val, err == nil, err
}

func (s *sqliteStore) KVSet(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runtime_kv(key,value,updated_at) VALUES(?,?,?)
         ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, fmtNow())
	return err
}

// Device status ---

func (s *sqliteStore) UpsertDeviceStatus(ctx context.Context, deviceID, deviceType, status, health, lastEventID string, lastSeenAt *time.Time, detailsJSON string) error {
	ls := (*string)(nil)
	if lastSeenAt != nil {
		t := lastSeenAt.UTC().Format(time.RFC3339Nano)
		ls = &t
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO current_device_status(device_id,device_type,status,health,last_event_id,last_seen_at,updated_at,details_json)
         VALUES(?,?,?,?,?,?,?,?)
         ON CONFLICT(device_id) DO UPDATE SET
           device_type=excluded.device_type,
           status=excluded.status,
           health=excluded.health,
           last_event_id=excluded.last_event_id,
           last_seen_at=excluded.last_seen_at,
           updated_at=excluded.updated_at,
           details_json=excluded.details_json`,
		deviceID, deviceType, status, health, lastEventID, ls, fmtNow(), detailsJSON)
	return err
}

func (s *sqliteStore) ListDeviceStatuses(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id,device_type,status,health,last_event_id,last_seen_at,updated_at,details_json FROM current_device_status ORDER BY device_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var deviceID, deviceType, status, health, lastEventID, detailsJSON string
		var lastSeenAt, updatedAt sql.NullString
		if err := rows.Scan(&deviceID, &deviceType, &status, &health, &lastEventID, &lastSeenAt, &updatedAt, &detailsJSON); err != nil {
			return nil, err
		}
		device := map[string]any{
			"device_id":     deviceID,
			"device_type":   deviceType,
			"status":        status,
			"health":        health,
			"last_event_id": lastEventID,
			"last_seen_at":  lastSeenAt.String,
			"updated_at":    updatedAt.String,
		}
		var details map[string]any
		if err := json.Unmarshal([]byte(detailsJSON), &details); err == nil {
			device["details"] = details
		}
		out = append(out, device)
	}
	return out, rows.Err()
}

// Alerts ---

func (s *sqliteStore) UpsertAlert(ctx context.Context, a *OpenAlert) (bool, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM open_alerts WHERE alert_dedupe_key=?`,
		a.AlertDedupeKey,
	).Scan(&exists); err != nil && err != sql.ErrNoRows {
		return false, err
	}
	created := exists != 1

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO open_alerts(alert_id,alert_dedupe_key,alert_type,severity,subject_kind,subject_id,rule_id,status,raised_at,updated_at,payload_json)
         VALUES(?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(alert_dedupe_key) DO UPDATE SET
           severity=excluded.severity,
           status=excluded.status,
           updated_at=excluded.updated_at,
           payload_json=excluded.payload_json`,
		a.AlertID, a.AlertDedupeKey, a.AlertType, a.Severity,
		a.SubjectKind, a.SubjectID, a.RuleID, a.Status,
		fmtTime(a.RaisedAt), fmtTime(a.UpdatedAt), a.PayloadJSON)
	if err != nil {
		return false, err
	}
	return created, nil
}

func (s *sqliteStore) ClearAlert(ctx context.Context, dedupeKey string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM open_alerts WHERE alert_dedupe_key=?`, dedupeKey)
	return err
}

// ClearAlertByID — alert_id 직접 매칭으로 close. 운영자 close API 가 사용.
// 영향 행 수 반환 (0 = 이미 close 또는 미존재).
func (s *sqliteStore) ClearAlertByID(ctx context.Context, alertID string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM open_alerts WHERE alert_id=?`, alertID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetOpenAlertByID — alert_id 로 조회. close 시 event payload 발행을 위해 필요.
func (s *sqliteStore) GetOpenAlertByID(ctx context.Context, alertID string) (*OpenAlert, error) {
	a := &OpenAlert{}
	var raisedAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT alert_id,alert_dedupe_key,alert_type,severity,subject_kind,subject_id,
                COALESCE(rule_id,''),status,raised_at,updated_at,payload_json
         FROM open_alerts WHERE alert_id=?`, alertID).
		Scan(&a.AlertID, &a.AlertDedupeKey, &a.AlertType, &a.Severity,
			&a.SubjectKind, &a.SubjectID, &a.RuleID, &a.Status,
			&raisedAt, &updatedAt, &a.PayloadJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.RaisedAt, _ = time.Parse(time.RFC3339Nano, raisedAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return a, nil
}

func (s *sqliteStore) GetOpenAlert(ctx context.Context, dedupeKey string) (*OpenAlert, error) {
	a := &OpenAlert{}
	var raisedAt, updatedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT alert_id,alert_dedupe_key,alert_type,severity,subject_kind,subject_id,
                COALESCE(rule_id,''),status,raised_at,updated_at,payload_json
         FROM open_alerts WHERE alert_dedupe_key=?`, dedupeKey).
		Scan(&a.AlertID, &a.AlertDedupeKey, &a.AlertType, &a.Severity,
			&a.SubjectKind, &a.SubjectID, &a.RuleID, &a.Status,
			&raisedAt, &updatedAt, &a.PayloadJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.RaisedAt, _ = time.Parse(time.RFC3339Nano, raisedAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return a, nil
}

func (s *sqliteStore) ListOpenAlerts(ctx context.Context) ([]*OpenAlert, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT alert_id,alert_dedupe_key,alert_type,severity,subject_kind,subject_id,
                COALESCE(rule_id,''),status,raised_at,updated_at,payload_json
         FROM open_alerts ORDER BY raised_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OpenAlert
	for rows.Next() {
		a := &OpenAlert{}
		var raisedAt, updatedAt string
		if err := rows.Scan(&a.AlertID, &a.AlertDedupeKey, &a.AlertType, &a.Severity,
			&a.SubjectKind, &a.SubjectID, &a.RuleID, &a.Status,
			&raisedAt, &updatedAt, &a.PayloadJSON); err != nil {
			return nil, err
		}
		a.RaisedAt, _ = time.Parse(time.RFC3339Nano, raisedAt)
		a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Commands ---

func (s *sqliteStore) InsertCommand(ctx context.Context, cmd *ControlCommand) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO control_commands(command_id,idempotency_key,target_device_id,command_type,status,requested_at,expires_at,last_event_id,payload_json)
         VALUES(?,?,?,?,?,?,?,?,?)`,
		cmd.CommandID, cmd.IdempotencyKey, cmd.TargetDeviceID, cmd.CommandType,
		cmd.Status, fmtTime(cmd.RequestedAt), fmtTime(cmd.ExpiresAt),
		cmd.LastEventID, cmd.PayloadJSON)
	return err
}

func (s *sqliteStore) GetCommandByIdempotencyKey(ctx context.Context, key string) (*ControlCommand, error) {
	return s.scanCommand(s.db.QueryRowContext(ctx,
		`SELECT command_id,idempotency_key,target_device_id,command_type,status,requested_at,expires_at,last_event_id,payload_json
         FROM control_commands WHERE idempotency_key=?`, key))
}

func (s *sqliteStore) GetCommand(ctx context.Context, commandID string) (*ControlCommand, error) {
	return s.scanCommand(s.db.QueryRowContext(ctx,
		`SELECT command_id,idempotency_key,target_device_id,command_type,status,requested_at,expires_at,last_event_id,payload_json
         FROM control_commands WHERE command_id=?`, commandID))
}

func (s *sqliteStore) scanCommand(row *sql.Row) (*ControlCommand, error) {
	cmd := &ControlCommand{}
	var reqAt, expAt string
	err := row.Scan(&cmd.CommandID, &cmd.IdempotencyKey, &cmd.TargetDeviceID, &cmd.CommandType,
		&cmd.Status, &reqAt, &expAt, &cmd.LastEventID, &cmd.PayloadJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cmd.RequestedAt, _ = time.Parse(time.RFC3339Nano, reqAt)
	cmd.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expAt)
	return cmd, nil
}

func (s *sqliteStore) UpdateCommandStatus(ctx context.Context, commandID, status, lastEventID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE control_commands SET status=?, last_event_id=? WHERE command_id=?`,
		status, lastEventID, commandID)
	return err
}

func (s *sqliteStore) ListNextCommandForDevices(ctx context.Context, deviceIDs []string, now time.Time) (*ControlCommand, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(deviceIDs))
	args := make([]any, 0, len(deviceIDs)+1)
	for i, id := range deviceIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, fmtTime(now))
	query := `SELECT command_id,idempotency_key,target_device_id,command_type,status,requested_at,expires_at,last_event_id,payload_json
		FROM control_commands
		WHERE target_device_id IN (` + strings.Join(placeholders, ",") + `)
		  AND status IN ('accepted','queued')
		  AND expires_at > ?
		ORDER BY requested_at ASC LIMIT 1`
	return s.scanCommand(s.db.QueryRowContext(ctx, query, args...))
}

func (s *sqliteStore) ListExpiredCommands(ctx context.Context, now time.Time) ([]*ControlCommand, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT command_id,idempotency_key,target_device_id,command_type,status,requested_at,expires_at,last_event_id,payload_json
         FROM control_commands
         WHERE status IN ('requested','accepted','queued','dispatched','acknowledged') AND expires_at <= ?`,
		fmtTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ControlCommand
	for rows.Next() {
		cmd := &ControlCommand{}
		var reqAt, expAt string
		if err := rows.Scan(&cmd.CommandID, &cmd.IdempotencyKey, &cmd.TargetDeviceID, &cmd.CommandType,
			&cmd.Status, &reqAt, &expAt, &cmd.LastEventID, &cmd.PayloadJSON); err != nil {
			return nil, err
		}
		cmd.RequestedAt, _ = time.Parse(time.RFC3339Nano, reqAt)
		cmd.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expAt)
		out = append(out, cmd)
	}
	return out, rows.Err()
}

// Sync batches ---

func (s *sqliteStore) InsertSyncBatch(ctx context.Context, b *SyncBatch, eventSeqs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO sync_batches(batch_id,from_sequence,to_sequence,status,attempt,created_at)
         VALUES(?,?,?,?,?,?)`,
		b.BatchID, b.FromSequence, b.ToSequence, b.Status, 0, fmtTime(b.CreatedAt))
	if err != nil {
		return err
	}

	for _, seq := range eventSeqs {
		var evtID string
		if err := tx.QueryRowContext(ctx, `SELECT event_id FROM events WHERE sequence=?`, seq).Scan(&evtID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO sync_batch_events(batch_id,event_sequence,event_id,status) VALUES(?,?,?,?)`,
			b.BatchID, seq, evtID, "pending")
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) UpdateSyncBatchStatus(ctx context.Context, batchID, status string, sentAt, ackAt *time.Time, remoteAckID, errorJSON string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sync_batches SET status=?, sent_at=?, acknowledged_at=?, remote_ack_id=?, error_json=? WHERE batch_id=?`,
		status, fmtTimePtr(sentAt), fmtTimePtr(ackAt), nullStr(remoteAckID), nullStr(errorJSON), batchID)
	return err
}

func (s *sqliteStore) GetPendingSyncBatch(ctx context.Context) (*SyncBatch, error) {
	b := &SyncBatch{}
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT batch_id,from_sequence,to_sequence,status,attempt,created_at FROM sync_batches
         WHERE status IN ('created','pending','failed','retry_scheduled')
         ORDER BY created_at ASC LIMIT 1`).
		Scan(&b.BatchID, &b.FromSequence, &b.ToSequence, &b.Status, &b.Attempt, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return b, nil
}

func (s *sqliteStore) GetSyncBatchEvents(ctx context.Context, batchID string) ([]*Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.sequence,e.event_id,e.event_type,e.schema_version,e.site_id,e.edge_id,e.recorded_at,
               e.source_module,COALESCE(e.source_adapter,''),COALESCE(e.source_device_id,''),
               e.payload_json,e.event_json,COALESCE(e.correlation_id,''),COALESCE(e.causation_id,''),e.synced_at
         FROM sync_batch_events sbe
         JOIN events e ON e.sequence = sbe.event_sequence
         WHERE sbe.batch_id=?
         ORDER BY e.sequence ASC`,
		batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *sqliteStore) CountPendingBatches(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sync_batches WHERE status IN ('created','pending','retry_scheduled')`).Scan(&n)
	return n, err
}

func (s *sqliteStore) CountFailedBatches(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sync_batches WHERE status='failed'`).Scan(&n)
	return n, err
}

// helpers

func fmtNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
