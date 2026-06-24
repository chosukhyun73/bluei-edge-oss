package storage

import (
	"context"
	"database/sql"
)

// C-13b actuator_models — 액추에이터 모델 라이브러리.
// actuators.model_id 가 이 테이블의 model_id 를 가리킨다 (nullable).
// camera_models 와 동일 패턴 — 카테고리/제어방식/용량/응답시간/제어범위 등 모델 특성.

// ActuatorModel — actuator_models row 도메인 모델.
// device_category 와 control_method 는 마이그 CHECK 와 동일한 enum.
type ActuatorModel struct {
	ModelID                   string   `json:"model_id"`
	Vendor                    string   `json:"vendor"`
	ProductCode               string   `json:"product_code"`
	DisplayName               string   `json:"display_name"`
	DeviceCategory            string   `json:"device_category"`
	RatedPowerW               *float64 `json:"rated_power_w,omitempty"`
	CapacityValue             *float64 `json:"capacity_value,omitempty"`
	CapacityUnit              string   `json:"capacity_unit,omitempty"`
	ControlMethod             string   `json:"control_method,omitempty"`
	ResponseTimeS             *float64 `json:"response_time_s,omitempty"`
	ControlRangeMin           *float64 `json:"control_range_min,omitempty"`
	ControlRangeMax           *float64 `json:"control_range_max,omitempty"`
	ControlRangeUnit          string   `json:"control_range_unit,omitempty"`
	ConsumableReplacementDays *int     `json:"consumable_replacement_days,omitempty"`
	Notes                     string   `json:"notes,omitempty"`
	// CategorySpecs — 카테고리별 spec 의 JSON 문자열 (예: circulation_pump 의 max_head_m 등).
	// 공통 컬럼으로 표현 안 되는 카테고리 고유 spec 을 담는다.
	CategorySpecs string `json:"category_specs,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

func (s *sqliteStore) UpsertActuatorModel(ctx context.Context, m *ActuatorModel) error {
	createdAt := m.CreatedAt
	if createdAt == "" {
		createdAt = fmtNow()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO actuator_models(model_id,vendor,product_code,display_name,device_category,rated_power_w,capacity_value,capacity_unit,control_method,response_time_s,control_range_min,control_range_max,control_range_unit,consumable_replacement_days,notes,category_specs,created_at,updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
         ON CONFLICT(model_id) DO UPDATE SET
           vendor=excluded.vendor,
           product_code=excluded.product_code,
           display_name=excluded.display_name,
           device_category=excluded.device_category,
           rated_power_w=excluded.rated_power_w,
           capacity_value=excluded.capacity_value,
           capacity_unit=excluded.capacity_unit,
           control_method=excluded.control_method,
           response_time_s=excluded.response_time_s,
           control_range_min=excluded.control_range_min,
           control_range_max=excluded.control_range_max,
           control_range_unit=excluded.control_range_unit,
           consumable_replacement_days=excluded.consumable_replacement_days,
           notes=excluded.notes,
           category_specs=excluded.category_specs,
           updated_at=excluded.updated_at`,
		m.ModelID,
		m.Vendor,
		m.ProductCode,
		m.DisplayName,
		m.DeviceCategory,
		ptrFloat(m.RatedPowerW),
		ptrFloat(m.CapacityValue),
		nullStr(m.CapacityUnit),
		nullStr(m.ControlMethod),
		ptrFloat(m.ResponseTimeS),
		ptrFloat(m.ControlRangeMin),
		ptrFloat(m.ControlRangeMax),
		nullStr(m.ControlRangeUnit),
		ptrInt(m.ConsumableReplacementDays),
		nullStr(m.Notes),
		nullStr(m.CategorySpecs),
		createdAt,
		fmtNow(),
	)
	return err
}

func (s *sqliteStore) GetActuatorModel(ctx context.Context, modelID string) (*ActuatorModel, error) {
	return s.scanActuatorModel(s.db.QueryRowContext(ctx, actuatorModelSelectSQL+` WHERE model_id=?`, modelID))
}

func (s *sqliteStore) ListActuatorModels(ctx context.Context) ([]*ActuatorModel, error) {
	rows, err := s.db.QueryContext(ctx, actuatorModelSelectSQL+` ORDER BY vendor, product_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ActuatorModel{}
	for rows.Next() {
		m, err := scanActuatorModelScanner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteActuatorModel(ctx context.Context, modelID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM actuator_models WHERE model_id=?`, modelID)
	return err
}

// CountActuatorsForModel — FK reject 정책 지원. 자식 인스턴스 있는 모델은 삭제 차단.
func (s *sqliteStore) CountActuatorsForModel(ctx context.Context, modelID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actuators WHERE model_id=?`, modelID).Scan(&n)
	return n, err
}

const actuatorModelSelectSQL = `SELECT model_id,vendor,product_code,display_name,device_category,rated_power_w,capacity_value,COALESCE(capacity_unit,''),COALESCE(control_method,''),response_time_s,control_range_min,control_range_max,COALESCE(control_range_unit,''),consumable_replacement_days,COALESCE(notes,''),COALESCE(category_specs,''),created_at,updated_at FROM actuator_models`

func (s *sqliteStore) scanActuatorModel(row *sql.Row) (*ActuatorModel, error) {
	m, err := scanActuatorModelScanner(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func scanActuatorModelScanner(row scanner) (*ActuatorModel, error) {
	m := &ActuatorModel{}
	var ratedW, capVal, respT, ctrlMin, ctrlMax sql.NullFloat64
	var consumDays sql.NullInt64
	if err := row.Scan(
		&m.ModelID, &m.Vendor, &m.ProductCode, &m.DisplayName, &m.DeviceCategory,
		&ratedW, &capVal, &m.CapacityUnit,
		&m.ControlMethod, &respT,
		&ctrlMin, &ctrlMax, &m.ControlRangeUnit,
		&consumDays, &m.Notes, &m.CategorySpecs,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if ratedW.Valid {
		v := ratedW.Float64
		m.RatedPowerW = &v
	}
	if capVal.Valid {
		v := capVal.Float64
		m.CapacityValue = &v
	}
	if respT.Valid {
		v := respT.Float64
		m.ResponseTimeS = &v
	}
	if ctrlMin.Valid {
		v := ctrlMin.Float64
		m.ControlRangeMin = &v
	}
	if ctrlMax.Valid {
		v := ctrlMax.Float64
		m.ControlRangeMax = &v
	}
	if consumDays.Valid {
		v := int(consumDays.Int64)
		m.ConsumableReplacementDays = &v
	}
	return m, nil
}
