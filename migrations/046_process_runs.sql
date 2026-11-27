-- 생산 실행: 공정 측정값, 모의 센서 상태, 생산 lot, 단계 이동, 사람 입력 점검, 이탈, 발행 문서 대장.
-- 시각은 모두 UTC unix 밀리초(INTEGER)다 — 구간 조회를 숫자 비교로 한다.
-- 공정 측정값은 보관 정리(internal/retention) 대상이 아니다(docs/54 §21.1).
-- References: docs/54-haccp-process-flow-ccp-design.md §7, §9, §17.4, §26.

CREATE TABLE IF NOT EXISTS process_readings (
  reading_id  INTEGER PRIMARY KEY AUTOINCREMENT,
  site_id     TEXT NOT NULL,
  sensor_id   TEXT NOT NULL,
  value       REAL NOT NULL,
  source      TEXT NOT NULL,     -- mock | sensor
  recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_process_readings_sensor ON process_readings(sensor_id, recorded_at);

-- 모의 센서가 지금 내보내는 값의 설정. 줄이 없으면 정상 값을 낸다.
CREATE TABLE IF NOT EXISTS mock_sensor_state (
  sensor_id TEXT PRIMARY KEY,
  mode      TEXT NOT NULL,       -- fixed(지정 값) | outage(값을 내지 않음)
  value     REAL NOT NULL DEFAULT 0,
  until_at  INTEGER NOT NULL     -- 이 시각이 지나면 정상으로 돌아간다
);

CREATE TABLE IF NOT EXISTS process_lots (
  lot_id      TEXT PRIMARY KEY,
  site_id     TEXT NOT NULL,
  plan_id     TEXT NOT NULL,
  version_id  TEXT NOT NULL,     -- 이 lot 을 판정하는 게시본
  lot_no      TEXT NOT NULL,
  status      TEXT NOT NULL,     -- running | closed
  inputs_json TEXT NOT NULL DEFAULT '[]',  -- [{supplier, supplier_lot, species, qty_kg}]
  output_kg   REAL NOT NULL DEFAULT 0,
  started_by  TEXT NOT NULL,
  started_at  INTEGER NOT NULL,
  closed_by   TEXT NOT NULL DEFAULT '',
  closed_at   INTEGER NOT NULL DEFAULT 0,
  reviewed_by TEXT NOT NULL DEFAULT '',
  reviewed_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE (site_id, lot_no)
);

-- lot 의 단계 이동. enter = 단계 진입, exit = 단계를 떠나 연결(link_id)로 나감.
CREATE TABLE IF NOT EXISTS process_lot_events (
  event_id INTEGER PRIMARY KEY AUTOINCREMENT,
  lot_id   TEXT NOT NULL,
  kind     TEXT NOT NULL,        -- enter | exit
  step_id  TEXT NOT NULL,
  link_id  TEXT NOT NULL DEFAULT '',
  at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_process_lot_events_lot ON process_lot_events(lot_id, event_id);

-- 사람이 넣은 측정·점검 값(값의 출처가 사람인 한계기준, 주기 점검).
CREATE TABLE IF NOT EXISTS process_checks (
  check_id INTEGER PRIMARY KEY AUTOINCREMENT,
  lot_id   TEXT NOT NULL,
  limit_id TEXT NOT NULL,
  value    REAL NOT NULL,
  by_user  TEXT NOT NULL,
  at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_process_checks_lot ON process_checks(lot_id, check_id);

CREATE TABLE IF NOT EXISTS process_deviations (
  dev_id            TEXT PRIMARY KEY,
  lot_id            TEXT NOT NULL,
  ccp_id            TEXT NOT NULL,
  limit_id          TEXT NOT NULL,
  kind              TEXT NOT NULL,   -- deviation(한계기준 이탈) | outage(모니터링 불능)
  detected_at       INTEGER NOT NULL,
  detail            TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'open',  -- open | closed
  corrective_action TEXT NOT NULL DEFAULT '',
  disposition       TEXT NOT NULL DEFAULT '',      -- 제품 처분
  closed_by         TEXT NOT NULL DEFAULT '',
  closed_at         INTEGER NOT NULL DEFAULT 0,
  UNIQUE (lot_id, limit_id, kind)
);

CREATE TABLE IF NOT EXISTS issued_documents (
  doc_id    TEXT PRIMARY KEY,
  site_id   TEXT NOT NULL,
  kind      TEXT NOT NULL,       -- plan | lot
  ref_id    TEXT NOT NULL,       -- version_id | lot_id
  title     TEXT NOT NULL,
  sha256    TEXT NOT NULL,       -- PDF 파일 해시
  data_hash TEXT NOT NULL,       -- 원본 데이터(JSON) 해시
  issued_by TEXT NOT NULL,
  issued_at INTEGER NOT NULL,
  path      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_issued_documents_site ON issued_documents(site_id, issued_at);
