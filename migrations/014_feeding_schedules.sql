-- Phase 1 multi-tank: operator-manual feeding schedule (hybrid: time list + cron option).
-- Scoping for Phase 5 (AI 적응형 학습 + Schedule UI). Schema scaffold only.
-- Safety gates (C-3, C-3p, C-3l, C-3w) still apply when schedule fires.
-- References: docs/39-multi-tank-feeder-system-design.md §6.

CREATE TABLE IF NOT EXISTS feeding_schedules (
  schedule_id       TEXT PRIMARY KEY,
  tank_ids_json     TEXT NOT NULL DEFAULT '[]',
  cron              TEXT,                       -- e.g. '0 6,12,18 * * *' (optional)
  times_json        TEXT,                       -- e.g. '["06:00","12:00","18:00"]' (optional)
  pattern_json      TEXT NOT NULL DEFAULT '{}', -- {type, pulse_duration_ms, gap_ms, total_pulses, target_amount_g}
  priority          TEXT NOT NULL DEFAULT 'manual_override',  -- 'manual_override' | 'ai_advisory'
  safety_gate       INTEGER NOT NULL DEFAULT 1,                -- C-3 / C-3p / C-3l / C-3w 적용 여부 (always 1 권장)
  enabled           INTEGER NOT NULL DEFAULT 1,
  created_by        TEXT NOT NULL DEFAULT '',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feeding_schedules_enabled ON feeding_schedules(enabled);
