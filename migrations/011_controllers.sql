-- Phase 1 multi-tank: Controller registry (ESP32 etc.).
-- Lifecycle: pending → active → disabled/fault.
-- 1 controller : 1 actuator (1:1) per current spec.
-- Two-step registration: POST /v1/controllers/register (pending) → /activate (active).
-- References: docs/39-multi-tank-feeder-system-design.md §2.

CREATE TABLE IF NOT EXISTS controllers (
  controller_id     TEXT PRIMARY KEY,
  tank_id           TEXT,                 -- nullable while pending
  site_id           TEXT,                 -- nullable while pending
  actuator_id       TEXT,                 -- 1:1 — usually equal to controller_id
  mac_address       TEXT NOT NULL,
  ip_address        TEXT,
  firmware_version  TEXT NOT NULL DEFAULT '',
  status            TEXT NOT NULL DEFAULT 'pending',  -- 'pending' | 'active' | 'disabled' | 'fault'
  registered_at     TEXT NOT NULL,
  last_seen_at      TEXT,
  commissioning_json TEXT NOT NULL DEFAULT '{}',
  metadata_json     TEXT NOT NULL DEFAULT '{}',
  updated_at        TEXT NOT NULL,
  UNIQUE (mac_address)
);

CREATE INDEX IF NOT EXISTS idx_controllers_status ON controllers(status);
CREATE INDEX IF NOT EXISTS idx_controllers_tank   ON controllers(tank_id);
CREATE INDEX IF NOT EXISTS idx_controllers_site   ON controllers(site_id);
