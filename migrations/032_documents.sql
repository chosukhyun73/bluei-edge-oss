-- GDST 증빙 서류 첨부 (provenance documents).
-- 각 생산 CTE 에 거래명세서/처방전/진단서/면허 등 서류를 첨부한다.
-- blob 파일은 {data_dir}/documents/{tank_id}/ 에 저장, 이 테이블은 메타데이터 projection.
-- 권위 기록은 events 의 tank.document.attached (append-only).
-- References: docs/49-gdst-traceability-contract.md.

CREATE TABLE IF NOT EXISTS tank_documents (
  document_id   TEXT PRIMARY KEY,
  tank_id       TEXT NOT NULL,
  lot_no        TEXT,
  cte_type      TEXT NOT NULL,   -- stocking|feeding|treatment|mortality|transfer|sale|harvest|other
  doc_type      TEXT NOT NULL,   -- prescription|diagnosis_certificate|transaction_statement|...
  event_ref     TEXT,            -- 연결된 CTE corr id (treatment_id/feeding_id/transfer_id 등)
  filename      TEXT NOT NULL,
  mime_type     TEXT NOT NULL,
  size_bytes    INTEGER NOT NULL,
  sha256        TEXT NOT NULL,
  stored_path   TEXT NOT NULL,   -- data_dir 기준 상대 경로
  notes         TEXT,
  uploaded_by   TEXT NOT NULL,
  uploaded_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tank_documents_tank ON tank_documents(tank_id);
CREATE INDEX IF NOT EXISTS idx_tank_documents_lot  ON tank_documents(lot_no);
CREATE INDEX IF NOT EXISTS idx_tank_documents_ref  ON tank_documents(event_ref);
