-- Phase 1 multi-tank: 3-layer domain (farms → sites → tanks).
-- A single `sites` table with site_type discriminator (land | marine).
-- Land-only and marine-only columns are nullable.
-- References: docs/39-multi-tank-feeder-system-design.md §1.

CREATE TABLE IF NOT EXISTS farms (
  farm_id            TEXT PRIMARY KEY,
  license_no         TEXT NOT NULL DEFAULT '',
  operator           TEXT NOT NULL DEFAULT '',
  certifications_json TEXT NOT NULL DEFAULT '[]',
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sites (
  site_id            TEXT PRIMARY KEY,
  farm_id            TEXT NOT NULL,
  site_type          TEXT NOT NULL,            -- 'land' | 'marine'
  name               TEXT NOT NULL DEFAULT '',
  timezone           TEXT NOT NULL DEFAULT 'Asia/Seoul',
  -- Land-specific (nullable on marine)
  address            TEXT,
  -- Coordinates (single point for land; for marine see sites_marine_gps)
  lat                REAL,
  lon                REAL,
  heading_deg        REAL,                     -- marine only, derived
  -- Generic
  metadata_json      TEXT NOT NULL DEFAULT '{}',
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  FOREIGN KEY (farm_id) REFERENCES farms(farm_id)
);

CREATE INDEX IF NOT EXISTS idx_sites_farm ON sites(farm_id);
CREATE INDEX IF NOT EXISTS idx_sites_type ON sites(site_type);

-- Marine sites can have multiple GPS reference points (e.g., north/south corner of a cage).
CREATE TABLE IF NOT EXISTS sites_marine_gps (
  site_id     TEXT NOT NULL,
  position    TEXT NOT NULL,                   -- 'north' | 'south' | 'east' | 'west' | custom
  lat         REAL NOT NULL,
  lon         REAL NOT NULL,
  updated_at  TEXT NOT NULL,
  PRIMARY KEY (site_id, position),
  FOREIGN KEY (site_id) REFERENCES sites(site_id)
);

-- Extend tank_profiles for the new 3-layer domain + lot tracking + lifecycle.
-- group_id (from 008) remains for backward compat; wtg_id is the new authoritative grouping.
ALTER TABLE tank_profiles ADD COLUMN site_id TEXT;
ALTER TABLE tank_profiles ADD COLUMN wtg_id TEXT;
ALTER TABLE tank_profiles ADD COLUMN lot_no TEXT;
ALTER TABLE tank_profiles ADD COLUMN lifecycle_stage TEXT;        -- 'fry' | 'fingerling' | 'growout'
ALTER TABLE tank_profiles ADD COLUMN mutable_lifecycle INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tank_profiles_site ON tank_profiles(site_id);
CREATE INDEX IF NOT EXISTS idx_tank_profiles_wtg  ON tank_profiles(wtg_id);
CREATE INDEX IF NOT EXISTS idx_tank_profiles_lot  ON tank_profiles(lot_no);
