package sensor

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadSensors reads and validates the sensors sub-config.
// Empty file or empty list is valid.
func LoadSensors(path string) ([]Sensor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sensors config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse sensors config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, s := range cfg.Sensors {
		if s.SensorID == "" {
			return nil, fmt.Errorf("sensors[%d]: sensor_id is required", i)
		}
		if len(s.SensorID) > 64 {
			return nil, fmt.Errorf("sensors[%d]: sensor_id too long (max 64)", i)
		}
		if seen[s.SensorID] {
			return nil, fmt.Errorf("sensors[%d]: duplicate sensor_id %q", i, s.SensorID)
		}
		seen[s.SensorID] = true
		if s.SensorType == "" {
			return nil, fmt.Errorf("sensors[%d] %q: sensor_type is required", i, s.SensorID)
		}
	}
	return cfg.Sensors, nil
}
