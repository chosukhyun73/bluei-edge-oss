package actuator

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadActuators reads and validates the actuators sub-config.
// Empty file or empty list is valid.
func LoadActuators(path string) ([]Actuator, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read actuators config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse actuators config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, a := range cfg.Actuators {
		if a.DeviceID == "" {
			return nil, fmt.Errorf("actuators[%d]: device_id is required", i)
		}
		if len(a.DeviceID) > 64 {
			return nil, fmt.Errorf("actuators[%d]: device_id too long (max 64)", i)
		}
		if seen[a.DeviceID] {
			return nil, fmt.Errorf("actuators[%d]: duplicate device_id %q", i, a.DeviceID)
		}
		seen[a.DeviceID] = true
		if a.DeviceType == "" {
			return nil, fmt.Errorf("actuators[%d] %q: device_type is required", i, a.DeviceID)
		}
		// feeder 타입은 tank_id 필수
		if a.DeviceType == "feeder" && a.TankID == "" {
			return nil, fmt.Errorf("actuators[%d] %q: tank_id is required for device_type=feeder", i, a.DeviceID)
		}
	}
	return cfg.Actuators, nil
}
