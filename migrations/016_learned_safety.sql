-- Phase 4 C-3l 학습 안전 게이트 + C-3w 환경 안전 게이트 스키마.
-- References: docs/39-multi-tank-feeder-system-design.md §5.3, §9.

-- 운영자 이의제기: 자동화 결정에 대한 현장 불일치 기록.
-- operator_disputes 의 집계로 learned_rules 를 마이닝 (MineFromDisputes).
CREATE TABLE IF NOT EXISTS operator_disputes (
  dispute_id    TEXT PRIMARY KEY,
  decision_id   TEXT NOT NULL,
  tank_id       TEXT NOT NULL,
  dispute_type  TEXT NOT NULL,    -- 'false_positive' | 'false_negative' | 'threshold_too_high' | 'threshold_too_low' | 'other'
  comment       TEXT NOT NULL DEFAULT '',
  disputed_at   TEXT NOT NULL,
  created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_disputes_tank ON operator_disputes (tank_id, disputed_at DESC);
CREATE INDEX IF NOT EXISTS idx_disputes_decision ON operator_disputes (decision_id);

-- C-3l 학습 규칙: dispute 집계 또는 사고 로그에서 자동 마이닝된 급이 차단 조건.
CREATE TABLE IF NOT EXISTS learned_rules (
  rule_id         TEXT PRIMARY KEY,
  condition_json  TEXT NOT NULL,    -- { metric, operator, threshold, window_h }
  severity        TEXT NOT NULL,    -- 'low' | 'medium' | 'high'
  source          TEXT NOT NULL,    -- 'operator_dispute' | 'incident_log'
  confidence      REAL NOT NULL DEFAULT 0,   -- 0..1
  hit_count       INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL,
  last_matched_at TEXT,
  enabled         INTEGER NOT NULL DEFAULT 1  -- 1=활성, 0=비활성 (운영자 수동 비활성화)
);

CREATE INDEX IF NOT EXISTS idx_learned_rules_enabled ON learned_rules (enabled, created_at DESC);

-- C-3w 환경 스냅샷: 풍속/파고/조수 등 외부 기상해양 데이터.
-- site_id 기준으로 저장 (해상 케이지 site 에만 유의미).
CREATE TABLE IF NOT EXISTS environmental_snapshots (
  snapshot_id     TEXT PRIMARY KEY,
  site_id         TEXT NOT NULL,
  wind_speed_ms   REAL,           -- m/s
  wave_height_m   REAL,           -- m
  tide_phase      TEXT,           -- 'rising' | 'falling' | 'high' | 'low'
  tide_minutes_to_low INTEGER,    -- 다음 간조까지 남은 분
  temperature_c   REAL,           -- 기온 (°C)
  recorded_at     TEXT NOT NULL,
  source          TEXT NOT NULL DEFAULT 'mock'   -- 'mock' | 'http'
);

CREATE INDEX IF NOT EXISTS idx_env_snapshots_site ON environmental_snapshots (site_id, recorded_at DESC);
