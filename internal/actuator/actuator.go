package actuator

// PositionInTank describes where a tank-internal actuator (e.g., a feeder)
// is mounted. Angle is in degrees (0=N, 90=E, 180=S, 270=W).
// 8-방위 UI maps to enum chips on top of the underlying float.
type PositionInTank struct {
	AngleDeg        float64 `yaml:"angle_deg" json:"angle_deg"`
	RadiusM         float64 `yaml:"radius_m" json:"radius_m"`
	FeedingModifier float64 `yaml:"feeding_modifier" json:"feeding_modifier"`
}

// Actuator is a control device.
type Actuator struct {
	DeviceID       string          `yaml:"device_id" json:"device_id"`
	DeviceType     string          `yaml:"device_type" json:"device_type"`
	SiteID         string          `yaml:"site_id" json:"site_id,omitempty"`
	TankID         string          `yaml:"tank_id" json:"tank_id,omitempty"`
	WTGID          string          `yaml:"wtg_id" json:"wtg_id,omitempty"`
	ControllerID   string          `yaml:"controller_id" json:"controller_id,omitempty"`
	Model          string          `yaml:"model" json:"model,omitempty"`
	RatedPowerW    float64         `yaml:"rated_power_w" json:"rated_power_w,omitempty"`
	PositionInTank *PositionInTank `yaml:"position_in_tank,omitempty" json:"position_in_tank,omitempty"`
	Capabilities   []string        `yaml:"capabilities" json:"capabilities"`
	Metadata       map[string]any  `yaml:"metadata" json:"metadata,omitempty"`
	// C-13b 액추에이터 모델 라이브러리 + 안전/운영 메타.
	// ModelID            : actuator_models.model_id 참조 (nullable).
	// MountLocation      : 어디 설치 (tank_inlet/wtg_intake/feeder_zone/external/...).
	// SafetyRoles        : 운영 안전 의도 multi-select (oxygen_critical/feed_actuator/filtration/...).
	// OperatingMode      : auto | manual | standby | maintenance | fault. 기본 auto.
	// AlarmThresholds    : 자유 형식 임계값 (예: {"pressure_min_kpa":30}).
	// LastMaintenanceAt  : 마지막 정비 시점 (RFC3339).
	// NextMaintenanceDueAt : 다음 정비 예정 (RFC3339, consumable_replacement_days 기반 자동 계산 가능).
	ModelID              string         `yaml:"model_id,omitempty" json:"model_id,omitempty"`
	MountLocation        string         `yaml:"mount_location,omitempty" json:"mount_location,omitempty"`
	SafetyRoles          []string       `yaml:"safety_role,omitempty" json:"safety_role,omitempty"`
	OperatingMode        string         `yaml:"operating_mode,omitempty" json:"operating_mode,omitempty"`
	AlarmThresholds      map[string]any `yaml:"alarm_thresholds,omitempty" json:"alarm_thresholds,omitempty"`
	LastMaintenanceAt    string         `yaml:"last_maintenance_at,omitempty" json:"last_maintenance_at,omitempty"`
	NextMaintenanceDueAt string         `yaml:"next_maintenance_due_at,omitempty" json:"next_maintenance_due_at,omitempty"`
}

// Config — actuators.yaml root structure.
type Config struct {
	Actuators []Actuator `yaml:"actuators"`
}
