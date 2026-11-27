package storage

import (
	"context"
	"database/sql"
)

// Production runs: readings, mock sensor state, lots, moves, checks,
// deviations, issued documents. docs/54 §26. Times are UTC unix milliseconds.

func (s *sqliteStore) InsertProcessReading(ctx context.Context, siteID string, r *ProcessReading) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO process_readings(site_id,sensor_id,value,source,recorded_at) VALUES(?,?,?,?,?)`,
		siteID, r.SensorID, r.Value, r.Source, r.At)
	return err
}

func (s *sqliteStore) ListProcessReadings(ctx context.Context, sensorID string, from, to int64) ([]ProcessReading, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT sensor_id,value,source,recorded_at FROM process_readings
		  WHERE sensor_id=? AND recorded_at>=? AND recorded_at<=? ORDER BY recorded_at, reading_id`, sensorID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProcessReading
	for rows.Next() {
		var r ProcessReading
		if err := rows.Scan(&r.SensorID, &r.Value, &r.Source, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) LatestProcessReading(ctx context.Context, sensorID string) (*ProcessReading, error) {
	r := &ProcessReading{}
	err := s.db.QueryRowContext(ctx,
		`SELECT sensor_id,value,source,recorded_at FROM process_readings
		  WHERE sensor_id=? ORDER BY recorded_at DESC, reading_id DESC LIMIT 1`, sensorID).
		Scan(&r.SensorID, &r.Value, &r.Source, &r.At)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (s *sqliteStore) SetMockSensorState(ctx context.Context, st *MockSensorState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mock_sensor_state(sensor_id,mode,value,until_at) VALUES(?,?,?,?)
		 ON CONFLICT(sensor_id) DO UPDATE SET mode=excluded.mode, value=excluded.value, until_at=excluded.until_at`,
		st.SensorID, st.Mode, st.Value, st.UntilAt)
	return err
}

func (s *sqliteStore) ListMockSensorStates(ctx context.Context) ([]MockSensorState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sensor_id,mode,value,until_at FROM mock_sensor_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MockSensorState
	for rows.Next() {
		var st MockSensorState
		if err := rows.Scan(&st.SensorID, &st.Mode, &st.Value, &st.UntilAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

const processLotCols = `lot_id,site_id,plan_id,version_id,lot_no,status,inputs_json,output_kg,
	started_by,started_at,closed_by,closed_at,reviewed_by,reviewed_at`

func scanProcessLot(sc rowScanner) (*ProcessLot, error) {
	l := &ProcessLot{}
	err := sc.Scan(&l.LotID, &l.SiteID, &l.PlanID, &l.VersionID, &l.LotNo, &l.Status, &l.InputsJSON, &l.OutputKg,
		&l.StartedBy, &l.StartedAt, &l.ClosedBy, &l.ClosedAt, &l.ReviewedBy, &l.ReviewedAt)
	return l, err
}

func (s *sqliteStore) InsertProcessLot(ctx context.Context, l *ProcessLot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO process_lots(lot_id,site_id,plan_id,version_id,lot_no,status,inputs_json,started_by,started_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		l.LotID, l.SiteID, l.PlanID, l.VersionID, l.LotNo, l.Status, l.InputsJSON, l.StartedBy, l.StartedAt)
	return err
}

func (s *sqliteStore) GetProcessLot(ctx context.Context, lotID string) (*ProcessLot, error) {
	l, err := scanProcessLot(s.db.QueryRowContext(ctx, `SELECT `+processLotCols+` FROM process_lots WHERE lot_id=?`, lotID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (s *sqliteStore) listProcessLots(ctx context.Context, where string, args ...any) ([]*ProcessLot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+processLotCols+` FROM process_lots WHERE `+where+` ORDER BY started_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProcessLot
	for rows.Next() {
		l, err := scanProcessLot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListProcessLots(ctx context.Context, siteID string) ([]*ProcessLot, error) {
	return s.listProcessLots(ctx, `site_id=?`, siteID)
}

func (s *sqliteStore) ListRunningProcessLots(ctx context.Context) ([]*ProcessLot, error) {
	return s.listProcessLots(ctx, `status='running'`)
}

func (s *sqliteStore) CloseProcessLot(ctx context.Context, lotID string, outputKg float64, by string, at int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE process_lots SET status='closed', output_kg=?, closed_by=?, closed_at=? WHERE lot_id=? AND status='running'`,
		outputKg, by, at, lotID)
	return err
}

func (s *sqliteStore) ReviewProcessLot(ctx context.Context, lotID, by string, at int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE process_lots SET reviewed_by=?, reviewed_at=? WHERE lot_id=? AND status='closed' AND reviewed_by=''`,
		by, at, lotID)
	return err
}

func (s *sqliteStore) InsertProcessLotEvent(ctx context.Context, lotID string, e *ProcessLotEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO process_lot_events(lot_id,kind,step_id,link_id,at) VALUES(?,?,?,?,?)`,
		lotID, e.Kind, e.StepID, e.LinkID, e.At)
	return err
}

func (s *sqliteStore) ListProcessLotEvents(ctx context.Context, lotID string) ([]ProcessLotEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind,step_id,link_id,at FROM process_lot_events WHERE lot_id=? ORDER BY event_id`, lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProcessLotEvent
	for rows.Next() {
		var e ProcessLotEvent
		if err := rows.Scan(&e.Kind, &e.StepID, &e.LinkID, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *sqliteStore) InsertProcessCheck(ctx context.Context, lotID string, c *ProcessCheck) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO process_checks(lot_id,limit_id,value,by_user,at) VALUES(?,?,?,?,?)`,
		lotID, c.LimitID, c.Value, c.ByUser, c.At)
	return err
}

func (s *sqliteStore) ListProcessChecks(ctx context.Context, lotID string) ([]ProcessCheck, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT limit_id,value,by_user,at FROM process_checks WHERE lot_id=? ORDER BY check_id`, lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProcessCheck
	for rows.Next() {
		var c ProcessCheck
		if err := rows.Scan(&c.LimitID, &c.Value, &c.ByUser, &c.At); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) InsertProcessDeviation(ctx context.Context, d *ProcessDeviation) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO process_deviations(dev_id,lot_id,ccp_id,limit_id,kind,detected_at,detail)
		 VALUES(?,?,?,?,?,?,?)`,
		d.DevID, d.LotID, d.CCPID, d.LimitID, d.Kind, d.DetectedAt, d.Detail)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *sqliteStore) ListProcessDeviations(ctx context.Context, lotID string) ([]*ProcessDeviation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dev_id,lot_id,ccp_id,limit_id,kind,detected_at,detail,status,corrective_action,disposition,closed_by,closed_at
		   FROM process_deviations WHERE lot_id=? ORDER BY detected_at, dev_id`, lotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProcessDeviation
	for rows.Next() {
		d := &ProcessDeviation{}
		if err := rows.Scan(&d.DevID, &d.LotID, &d.CCPID, &d.LimitID, &d.Kind, &d.DetectedAt, &d.Detail, &d.Status,
			&d.CorrectiveAction, &d.Disposition, &d.ClosedBy, &d.ClosedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *sqliteStore) CloseProcessDeviation(ctx context.Context, devID, action, disposition, by string, at int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE process_deviations SET status='closed', corrective_action=?, disposition=?, closed_by=?, closed_at=?
		  WHERE dev_id=? AND status='open'`, action, disposition, by, at, devID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

const issuedDocCols = `doc_id,site_id,kind,ref_id,title,sha256,data_hash,issued_by,issued_at,path`

func scanIssuedDoc(sc rowScanner) (*IssuedDocument, error) {
	d := &IssuedDocument{}
	err := sc.Scan(&d.DocID, &d.SiteID, &d.Kind, &d.RefID, &d.Title, &d.SHA256, &d.DataHash, &d.IssuedBy, &d.IssuedAt, &d.Path)
	return d, err
}

func (s *sqliteStore) InsertIssuedDocument(ctx context.Context, d *IssuedDocument) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO issued_documents(`+issuedDocCols+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.DocID, d.SiteID, d.Kind, d.RefID, d.Title, d.SHA256, d.DataHash, d.IssuedBy, d.IssuedAt, d.Path)
	return err
}

func (s *sqliteStore) GetIssuedDocument(ctx context.Context, docID string) (*IssuedDocument, error) {
	d, err := scanIssuedDoc(s.db.QueryRowContext(ctx, `SELECT `+issuedDocCols+` FROM issued_documents WHERE doc_id=?`, docID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (s *sqliteStore) ListIssuedDocuments(ctx context.Context, siteID string) ([]*IssuedDocument, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+issuedDocCols+` FROM issued_documents WHERE site_id=? ORDER BY issued_at DESC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*IssuedDocument
	for rows.Next() {
		d, err := scanIssuedDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
