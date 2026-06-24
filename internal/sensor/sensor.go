package sensor

// Sensor is a measurement-only device.
type Sensor struct {
	SensorID     string         `yaml:"sensor_id" json:"sensor_id"`
	SensorType   string         `yaml:"sensor_type" json:"sensor_type"`
	SiteID       string         `yaml:"site_id" json:"site_id,omitempty"`
	TankID       string         `yaml:"tank_id" json:"tank_id,omitempty"`
	WTGID        string         `yaml:"wtg_id" json:"wtg_id,omitempty"`
	Position     string         `yaml:"position" json:"position,omitempty"`
	Hardware     string         `yaml:"hardware" json:"hardware,omitempty"`
	Capabilities []string       `yaml:"capabilities" json:"capabilities"`
	Metadata     map[string]any `yaml:"metadata" json:"metadata,omitempty"`
	// C-13a — 센서 모델 라이브러리 link + 인스턴스 메타.
	// ModelID            : sensor_models.model_id 참조 (nullable).
	// MountLocation      : 어디 위에 설치 (water_intake/water_outlet/tank_top/...). 카메라 mount_location 과 평행 구조.
	// InstalledDepthM    : 수면 기준 (양수=수면 위, 음수=수중). 카메라 height_from_water_m 와 평행.
	// MeasurementRole    : 운영자 의도 (safety_gate_c3/feeding_decision/...). 다중 선택.
	// CalibrationLastAt  : 마지막 교정 일시 (RFC3339).
	// CalibrationDueAt   : 다음 교정 예정 일시 (model.calibration_interval_days 기반 자동 계산 또는 수동 입력).
	ModelID           string   `yaml:"model_id" json:"model_id,omitempty"`
	MountLocation     string   `yaml:"mount_location" json:"mount_location,omitempty"`
	InstalledDepthM   *float64 `yaml:"installed_depth_m" json:"installed_depth_m,omitempty"`
	MeasurementRole   []string `yaml:"measurement_role" json:"measurement_role,omitempty"`
	CalibrationLastAt string   `yaml:"calibration_last_at" json:"calibration_last_at,omitempty"`
	CalibrationDueAt  string   `yaml:"calibration_due_at" json:"calibration_due_at,omitempty"`
}

// Config — sensors.yaml root structure.
type Config struct {
	Sensors []Sensor `yaml:"sensors"`
}
