package farm

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFarms reads and validates the farms sub-config.
// Empty file or empty farms list is valid (zero-config friendly).
func LoadFarms(path string) ([]Farm, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read farms config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse farms config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, f := range cfg.Farms {
		if f.FarmID == "" {
			return nil, fmt.Errorf("farms[%d]: farm_id is required", i)
		}
		if len(f.FarmID) > 64 {
			return nil, fmt.Errorf("farms[%d]: farm_id too long (max 64)", i)
		}
		if seen[f.FarmID] {
			return nil, fmt.Errorf("farms[%d]: duplicate farm_id %q", i, f.FarmID)
		}
		seen[f.FarmID] = true
		if f.Operator == "" {
			return nil, fmt.Errorf("farms[%d] %q: operator is required", i, f.FarmID)
		}
	}
	return cfg.Farms, nil
}
