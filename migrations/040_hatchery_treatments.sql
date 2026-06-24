-- 종묘장 처치 기록(투약/약품 CTE) — 산란/자어 batch 단위. MT 성전환·소독·항생제 등.
-- 수출규제 KDE(물질·용량·휴약기간)로서 백엔드 _project_gx10_treatment 가 lot_code(알/종자 lot)
-- 계보에 귀속시킨다(seed lot meta.kde_bundle.treatments + canonical TREATMENT 이벤트).
-- 오프라인 정본: 재고차감(item_id/consumed_qty)·원가·notes 등 전체 운영기록 보관.
-- 클라우드로는 KDE 부분집합만 전송(item_id/consumed_qty 등 raw 미전송).
CREATE TABLE IF NOT EXISTS hatchery_treatments (
  treatment_id     TEXT PRIMARY KEY,
  group_id         TEXT NOT NULL,
  subject_kind     TEXT NOT NULL,          -- spawn | larval (처치 대상 배치 종류)
  batch_id         TEXT,                   -- spawn_batches.batch_id 또는 larval_batches.batch_id
  lot_code         TEXT,                   -- 알/종자 lot(=spawn_batches.lot_code) — KDE 귀속 키
  tank_id          TEXT,
  species          TEXT,
  treatment_type   TEXT NOT NULL,          -- sex_reversal|disinfection|antibiotic|vaccine|chemical|probiotic|anesthetic|other
  substance        TEXT NOT NULL,          -- 약품/물질명 (예: 17a-MT, 포르말린)
  dose             REAL,
  dose_unit        TEXT,
  route            TEXT,                   -- 침지|경구|주사 등
  reason           TEXT,
  withdrawal_until TEXT,                   -- 휴약기간 종료(출하금지 해제) — RFC3339
  administered_at  TEXT NOT NULL,          -- 처치 시각 RFC3339
  operator_id      TEXT,
  item_id          TEXT,                   -- 재고 차감 약품 품목(오프라인 전용)
  consumed_qty     REAL,                   -- 품목 사용량(오프라인 원가, 클라우드 미전송)
  notes            TEXT,
  metadata_json    TEXT,
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_hatchery_treatment_group ON hatchery_treatments(group_id);
CREATE INDEX IF NOT EXISTS idx_hatchery_treatment_lot ON hatchery_treatments(lot_code);
