package species

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadSpeciesProfiles reads and validates the species_profiles sub-config.
// Empty file or empty map is valid.
func LoadSpeciesProfiles(path string) (map[string]Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read species_profiles config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse species_profiles config %s: %w", path, err)
	}
	if cfg.SpeciesProfiles == nil {
		return map[string]Profile{}, nil
	}
	for key, p := range cfg.SpeciesProfiles {
		if key == "" {
			return nil, fmt.Errorf("species_profiles: empty key is not allowed")
		}
		if p.DisplayName == "" {
			return nil, fmt.Errorf("species_profiles[%q]: display_name is required", key)
		}
		// 각 lifecycle stage 의 weight_range_g[0] < weight_range_g[1] 검증
		for stageName, stage := range p.LifecycleStages {
			if stage.WeightRangeG[0] >= stage.WeightRangeG[1] {
				return nil, fmt.Errorf("species_profiles[%q].lifecycle_stages[%q]: weight_range_g[0] (%.2f) must be < weight_range_g[1] (%.2f)",
					key, stageName, stage.WeightRangeG[0], stage.WeightRangeG[1])
			}
		}
	}
	return cfg.SpeciesProfiles, nil
}
