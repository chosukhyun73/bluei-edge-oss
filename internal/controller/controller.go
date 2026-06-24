package controller

// Status is the lifecycle state of a controller.
type Status string

const (
	StatusPending  Status = "pending"
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
	StatusFault    Status = "fault"
)

// Commissioning records the result of the commissioning test (DAC, stop, latency).
type Commissioning struct {
	DACOk     bool     `yaml:"dac_ok" json:"dac_ok"`
	StopOk    bool     `yaml:"stop_ok" json:"stop_ok"`
	MotorOk   bool     `yaml:"motor_ok" json:"motor_ok"` // 풀 동작 시퀀스(공급/살포 램프) 실행 완료
	LatencyMs *int     `yaml:"latency_ms" json:"latency_ms,omitempty"`
	HasWeight bool     `yaml:"has_weight" json:"has_weight"`         // 중량계 감지 여부
	WeightG   *float64 `yaml:"weight_g" json:"weight_g,omitempty"`   // 0점 직전 측정값
	Tared     bool     `yaml:"tared" json:"tared"`                   // 0점(tare) 수행 여부
	TestedAt  *string  `yaml:"tested_at" json:"tested_at,omitempty"` // RFC3339
}

// Controller is a physical microcontroller registered with the edge runtime.
type Controller struct {
	ControllerID    string         `yaml:"controller_id" json:"controller_id"`
	TankID          string         `yaml:"tank_id" json:"tank_id,omitempty"`
	SiteID          string         `yaml:"site_id" json:"site_id,omitempty"`
	ActuatorID      string         `yaml:"actuator_id" json:"actuator_id,omitempty"`
	MACAddress      string         `yaml:"mac_address" json:"mac_address"`
	IPAddress       string         `yaml:"ip_address" json:"ip_address,omitempty"`
	FirmwareVersion string         `yaml:"firmware_version" json:"firmware_version"`
	Status          Status         `yaml:"status" json:"status"`
	RegisteredAt    string         `yaml:"registered_at" json:"registered_at"` // RFC3339
	LastSeenAt      string         `yaml:"last_seen_at" json:"last_seen_at,omitempty"`
	Commissioning   Commissioning  `yaml:"commissioning" json:"commissioning"`
	Metadata        map[string]any `yaml:"metadata" json:"metadata,omitempty"`
}

// Config — controllers.yaml root structure.
type Config struct {
	Controllers []Controller `yaml:"controllers"`
}
