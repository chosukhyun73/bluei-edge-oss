-- 현장(사이트) 바깥 환경 관측 — 서버(/gx10/farm-environment)에서 받은 기상청 부이(바람·풍향·파고·기온),
-- 관측소 일별 수온(최저·최고·대표), 물때 예보(고조·저조). 해상 장기 환경 분석(2년) 화면용 (2026-11-17).
-- 부이가 수십 km 밖이라 참고값 → 해상 안전 판정(environmental_snapshots)에는 넣지 않는다.
-- events 가 아니라서 서버로 다시 올라가지 않고 보존 정책으로 지워지지 않는다(30분 × 2년 ≈ 3.5만 행/항목).
CREATE TABLE IF NOT EXISTS site_env_observations (
  site_id      TEXT NOT NULL,
  metric       TEXT NOT NULL,   -- wind_speed | wind_direction | wave_height | air_temperature
                                -- | sea_temp_daily_min | sea_temp_daily_max | sea_temp_daily_avg | tide_high | tide_low
  observed_at  TEXT NOT NULL,   -- RFC3339 UTC (일별 값은 그날 정오 KST)
  value        REAL NOT NULL,
  unit         TEXT,
  source       TEXT NOT NULL,   -- kma_buoy | nifs_daily | satellite_daily | khoa_tide_forecast
  station_id   TEXT,
  station_name TEXT,
  distance_km  REAL,
  recorded_at  TEXT NOT NULL,
  PRIMARY KEY (site_id, metric, observed_at)
);
CREATE INDEX IF NOT EXISTS idx_site_env_obs_time ON site_env_observations (site_id, observed_at);
