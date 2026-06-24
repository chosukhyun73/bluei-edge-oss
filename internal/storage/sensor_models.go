package storage

import (
	"context"
	"database/sql"
)

// C-13a sensor_models — 센서 모델 라이브러리.
// sensors.model_id 가 이 테이블의 model_id 를 가리킨다 (nullable).
// 카메라 (C-11) camera_models.go 패턴을 동일하게 따른다.

func (s *sqliteStore) UpsertSensorModel(ctx context.Context, m *SensorModel) error {
	createdAt := m.CreatedAt
	if createdAt == "" {
		createdAt = fmtNow()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sensor_models(model_id,vendor,product_code,display_name,measurement_type,unit,range_min,range_max,accuracy_value,accuracy_unit,response_time_s,protocol,calibration_interval_days,wet_dry,notes,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(model_id) DO UPDATE SET
           vendor=excluded.vendor,
           product_code=excluded.product_code,
           display_name=excluded.display_name,
           measurement_type=excluded.measurement_type,
           unit=excluded.unit,
           range_min=excluded.range_min,
           range_max=excluded.range_max,
           accuracy_value=excluded.accuracy_value,
           accuracy_unit=excluded.accuracy_unit,
           response_time_s=excluded.response_time_s,
           protocol=excluded.protocol,
           calibration_interval_days=excluded.calibration_interval_days,
           wet_dry=excluded.wet_dry,
           notes=excluded.notes,
           updated_at=excluded.updated_at`,
		m.ModelID,
		m.Vendor,
		m.ProductCode,
		m.DisplayName,
		m.MeasurementType,
		m.Unit,
		ptrFloat(m.RangeMin),
		ptrFloat(m.RangeMax),
		ptrFloat(m.AccuracyValue),
		nullStr(m.AccuracyUnit),
		ptrFloat(m.ResponseTimeS),
		nullStr(m.Protocol),
		ptrInt(m.CalibrationIntervalDays),
		nullStr(m.WetDry),
		nullStr(m.Notes),
		createdAt,
		fmtNow(),
	)
	return err
}

func (s *sqliteStore) GetSensorModel(ctx context.Context, modelID string) (*SensorModel, error) {
	return s.scanSensorModel(s.db.QueryRowContext(ctx, sensorModelSelectSQL+` WHERE model_id=?`, modelID))
}

func (s *sqliteStore) ListSensorModels(ctx context.Context) ([]*SensorModel, error) {
	rows, err := s.db.QueryContext(ctx, sensorModelSelectSQL+` ORDER BY vendor, product_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SensorModel{}
	for rows.Next() {
		m, err := scanSensorModelScanner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteSensorModel(ctx context.Context, modelID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sensor_models WHERE model_id=?`, modelID)
	return err
}

// CountSensorsForModel — FK reject 정책 지원. 자식 인스턴스 있는 모델은 삭제 차단.
func (s *sqliteStore) CountSensorsForModel(ctx context.Context, modelID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sensors WHERE model_id=?`, modelID).Scan(&n)
	return n, err
}

const sensorModelSelectSQL = `SELECT model_id,vendor,product_code,display_name,measurement_type,unit,range_min,range_max,accuracy_value,COALESCE(accuracy_unit,''),response_time_s,COALESCE(protocol,''),calibration_interval_days,COALESCE(wet_dry,''),COALESCE(notes,''),created_at,updated_at FROM sensor_models`

func (s *sqliteStore) scanSensorModel(row *sql.Row) (*SensorModel, error) {
	m, err := scanSensorModelScanner(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func scanSensorModelScanner(row scanner) (*SensorModel, error) {
	m := &SensorModel{}
	var rangeMin, rangeMax, accuracyValue, responseTime sql.NullFloat64
	var calibInterval sql.NullInt64
	if err := row.Scan(
		&m.ModelID, &m.Vendor, &m.ProductCode, &m.DisplayName,
		&m.MeasurementType, &m.Unit,
		&rangeMin, &rangeMax, &accuracyValue, &m.AccuracyUnit,
		&responseTime, &m.Protocol,
		&calibInterval, &m.WetDry, &m.Notes,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if rangeMin.Valid {
		v := rangeMin.Float64
		m.RangeMin = &v
	}
	if rangeMax.Valid {
		v := rangeMax.Float64
		m.RangeMax = &v
	}
	if accuracyValue.Valid {
		v := accuracyValue.Float64
		m.AccuracyValue = &v
	}
	if responseTime.Valid {
		v := responseTime.Float64
		m.ResponseTimeS = &v
	}
	if calibInterval.Valid {
		v := int(calibInterval.Int64)
		m.CalibrationIntervalDays = &v
	}
	return m, nil
}
