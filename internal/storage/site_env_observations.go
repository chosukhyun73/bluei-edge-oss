package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SiteEnvObservation — 현장 바깥 환경 관측 1건 (migrations/043).
type SiteEnvObservation struct {
	SiteID      string    `json:"site_id"`
	Metric      string    `json:"metric"`
	ObservedAt  time.Time `json:"observed_at"`
	Value       float64   `json:"value"`
	Unit        string    `json:"unit,omitempty"`
	Source      string    `json:"source"`
	StationID   string    `json:"station_id,omitempty"`
	StationName string    `json:"station_name,omitempty"`
	DistanceKM  *float64  `json:"distance_km,omitempty"`
}

// UpsertSiteEnvObservations — (site, metric, observed_at) 기준 덮어쓰기. 한 트랜잭션.
func (s *sqliteStore) UpsertSiteEnvObservations(ctx context.Context, obs []SiteEnvObservation) error {
	if len(obs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR REPLACE INTO site_env_observations
		 (site_id,metric,observed_at,value,unit,source,station_id,station_name,distance_km,recorded_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := fmtTime(time.Now())
	for _, o := range obs {
		if _, err := stmt.ExecContext(ctx, o.SiteID, o.Metric, fmtTime(o.ObservedAt), o.Value,
			nullStr(o.Unit), o.Source, nullStr(o.StationID), nullStr(o.StationName), o.DistanceKM, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListSiteEnvObservations — 기간 안 관측 (시간 오름차순). metric 이 비면 전부.
func (s *sqliteStore) ListSiteEnvObservations(ctx context.Context, siteID, metric string, since, until time.Time) ([]SiteEnvObservation, error) {
	q := `SELECT site_id,metric,observed_at,value,COALESCE(unit,''),source,COALESCE(station_id,''),
	             COALESCE(station_name,''),distance_km
	      FROM site_env_observations WHERE site_id=? AND observed_at>=? AND observed_at<?`
	args := []any{siteID, fmtTime(since), fmtTime(until)}
	if metric != "" {
		q += ` AND metric=?`
		args = append(args, metric)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY observed_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SiteEnvObservation
	for rows.Next() {
		var o SiteEnvObservation
		var at string
		if err := rows.Scan(&o.SiteID, &o.Metric, &at, &o.Value, &o.Unit, &o.Source, &o.StationID,
			&o.StationName, &o.DistanceKM); err != nil {
			return nil, err
		}
		o.ObservedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, o)
	}
	return out, rows.Err()
}

// EnvDailyAgg — 하루·항목 하나의 최저·평균·최고.
type EnvDailyAgg struct {
	Date   string  `json:"date"`
	Metric string  `json:"metric"`
	Min    float64 `json:"min"`
	Avg    float64 `json:"avg"`
	Max    float64 `json:"max"`
	Count  int     `json:"count"`
}

// AggregateSiteEnvDaily — 현장 바깥 관측을 현지 날짜·항목별로 묶는다(observed_at 기준).
func (s *sqliteStore) AggregateSiteEnvDaily(ctx context.Context, siteID string, since time.Time, tzOffsetSeconds int) ([]EnvDailyAgg, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(observed_at, ?) d, metric, MIN(value), AVG(value), MAX(value), COUNT(*)
		FROM site_env_observations WHERE site_id=? AND observed_at>=?
		GROUP BY d, metric ORDER BY d, metric`,
		fmt.Sprintf("%+d seconds", tzOffsetSeconds), siteID, fmtTime(since))
	if err != nil {
		return nil, err
	}
	return scanEnvDaily(rows)
}

// AggregateTankSensorDailyByObserved — 수조 센서(수온·DO 등)를 관측 시각(observed_at) 기준 현지
// 날짜·항목별로 묶는다. 앱에서 이어 받은 지난 기록은 들어온 시각(recorded_at)이 전부 같아서
// recorded_at 으로 묶으면 안 된다. 수조 전체를 합친 값(해상 가두리는 같은 관측소 값).
func (s *sqliteStore) AggregateTankSensorDailyByObserved(ctx context.Context, since time.Time, tzOffsetSeconds int) ([]EnvDailyAgg, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT date(json_extract(payload_json,'$.observed_at'), ?) d,
		       json_extract(payload_json,'$.metric') m,
		       MIN(CAST(json_extract(payload_json,'$.value') AS REAL)),
		       AVG(CAST(json_extract(payload_json,'$.value') AS REAL)),
		       MAX(CAST(json_extract(payload_json,'$.value') AS REAL)), COUNT(*)
		FROM events
		WHERE event_type='sensor.reading.recorded'
		  AND json_extract(payload_json,'$.value') IS NOT NULL
		  AND json_extract(payload_json,'$.observed_at') >= ?
		GROUP BY d, m ORDER BY d, m`,
		fmt.Sprintf("%+d seconds", tzOffsetSeconds), fmtTime(since))
	if err != nil {
		return nil, err
	}
	return scanEnvDaily(rows)
}

func scanEnvDaily(rows *sql.Rows) ([]EnvDailyAgg, error) {
	defer rows.Close()
	var out []EnvDailyAgg
	for rows.Next() {
		var a EnvDailyAgg
		if err := rows.Scan(&a.Date, &a.Metric, &a.Min, &a.Avg, &a.Max, &a.Count); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
