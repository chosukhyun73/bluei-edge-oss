package storage

import (
	"context"
	"database/sql"
	"time"
)

func (s *sqliteStore) InsertTankDocument(ctx context.Context, d *TankDocument) error {
	subjectType := d.SubjectType
	if subjectType == "" {
		subjectType = "tank"
	}
	subjectID := d.SubjectID
	if subjectID == "" {
		subjectID = d.TankID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tank_documents(
		   document_id,tank_id,lot_no,cte_type,doc_type,event_ref,
		   filename,mime_type,size_bytes,sha256,stored_path,notes,uploaded_by,uploaded_at,
		   subject_type,subject_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.DocumentID,
		d.TankID,
		nullStr(d.LotNo),
		d.CTEType,
		d.DocType,
		nullStr(d.EventRef),
		d.Filename,
		d.MimeType,
		d.SizeBytes,
		d.SHA256,
		d.StoredPath,
		nullStr(d.Notes),
		d.UploadedBy,
		fmtTime(d.UploadedAt),
		subjectType,
		subjectID,
	)
	return err
}

const documentCols = `document_id,tank_id,COALESCE(lot_no,''),cte_type,doc_type,COALESCE(event_ref,''),
		        filename,mime_type,size_bytes,sha256,stored_path,COALESCE(notes,''),uploaded_by,uploaded_at,
		        COALESCE(subject_type,'tank'),COALESCE(subject_id,tank_id)`

func (s *sqliteStore) GetTankDocument(ctx context.Context, documentID string) (*TankDocument, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+documentCols+` FROM tank_documents WHERE document_id=?`, documentID)
	return scanTankDocument(row)
}

func (s *sqliteStore) ListTankDocuments(ctx context.Context, tankID string) ([]*TankDocument, error) {
	return s.queryDocuments(ctx,
		`SELECT `+documentCols+` FROM tank_documents WHERE tank_id=? ORDER BY uploaded_at DESC`, tankID)
}

func (s *sqliteStore) ListDocumentsBySubject(ctx context.Context, subjectType, subjectID string) ([]*TankDocument, error) {
	return s.queryDocuments(ctx,
		`SELECT `+documentCols+` FROM tank_documents
		 WHERE COALESCE(subject_type,'tank')=? AND COALESCE(subject_id,tank_id)=? ORDER BY uploaded_at DESC`,
		subjectType, subjectID)
}

func (s *sqliteStore) queryDocuments(ctx context.Context, query string, args ...any) ([]*TankDocument, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TankDocument
	for rows.Next() {
		d, scanErr := scanDocumentRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanDocumentRow(sc rowScanner) (*TankDocument, error) {
	d := &TankDocument{}
	var uploadedAt string
	if err := sc.Scan(
		&d.DocumentID, &d.TankID, &d.LotNo, &d.CTEType, &d.DocType, &d.EventRef,
		&d.Filename, &d.MimeType, &d.SizeBytes, &d.SHA256, &d.StoredPath, &d.Notes,
		&d.UploadedBy, &uploadedAt, &d.SubjectType, &d.SubjectID,
	); err != nil {
		return nil, err
	}
	d.UploadedAt, _ = time.Parse(time.RFC3339Nano, uploadedAt)
	return d, nil
}

func scanTankDocument(row *sql.Row) (*TankDocument, error) {
	d, err := scanDocumentRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}
