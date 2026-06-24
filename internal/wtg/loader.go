package wtg

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadGroups reads and validates the water_treatment_groups sub-config.
// Empty file or empty list is valid.
func LoadGroups(path string) ([]Group, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read water_treatment_groups config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse water_treatment_groups config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, g := range cfg.Groups {
		if g.WTGID == "" {
			return nil, fmt.Errorf("water_treatment_groups[%d]: wtg_id is required", i)
		}
		if len(g.WTGID) > 64 {
			return nil, fmt.Errorf("water_treatment_groups[%d]: wtg_id too long (max 64)", i)
		}
		if seen[g.WTGID] {
			return nil, fmt.Errorf("water_treatment_groups[%d]: duplicate wtg_id %q", i, g.WTGID)
		}
		seen[g.WTGID] = true
		if g.SiteID == "" {
			return nil, fmt.Errorf("water_treatment_groups[%d] %q: site_id is required", i, g.WTGID)
		}
		// tank_ids must be unique within a group
		tankSeen := map[string]bool{}
		for j, tid := range g.TankIDs {
			if tankSeen[tid] {
				return nil, fmt.Errorf("water_treatment_groups[%d] %q: duplicate tank_id %q at index %d", i, g.WTGID, tid, j)
			}
			tankSeen[tid] = true
		}
		if g.Capacity.NH3ProcessingKgPerH < 0 {
			return nil, fmt.Errorf("water_treatment_groups[%d] %q: capacity.nh3_processing_kg_per_h must be >= 0", i, g.WTGID)
		}
	}
	return cfg.Groups, nil
}
