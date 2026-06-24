-- 서류 첨부 대상 일반화: tank 전속 → subject 기반 (tank | inventory_purchase).
-- 구매(거래명세서/계산서) 등 수조와 무관한 서류도 첨부할 수 있게 한다.
-- 기존 tank 문서는 subject_type='tank', subject_id=tank_id 로 backfill.
-- References: docs/49-gdst-traceability-contract.md.

ALTER TABLE tank_documents ADD COLUMN subject_type TEXT;  -- tank | inventory_purchase
ALTER TABLE tank_documents ADD COLUMN subject_id   TEXT;

UPDATE tank_documents SET subject_type='tank', subject_id=tank_id
 WHERE subject_type IS NULL OR subject_type='';

CREATE INDEX IF NOT EXISTS idx_tank_documents_subject ON tank_documents(subject_type, subject_id);
