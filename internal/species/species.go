package species

// FeedingPattern is the default pulse / gap shape for one lifecycle stage.
type FeedingPattern struct {
	PulseDurationMs int `yaml:"pulse_duration_ms" json:"pulse_duration_ms"`
	GapMs           int `yaml:"gap_ms" json:"gap_ms"`
	TotalPulses     int `yaml:"total_pulses" json:"total_pulses"`
}

// LifecycleStage is the per-stage default (fry / fingerling / growout).
type LifecycleStage struct {
	FCRTarget             float64        `yaml:"fcr_target" json:"fcr_target"`
	WeightRangeG          [2]float64     `yaml:"weight_range_g" json:"weight_range_g"`
	FeedType              string         `yaml:"feed_type" json:"feed_type"`
	FeedingPatternDefault FeedingPattern `yaml:"feeding_pattern_default" json:"feeding_pattern_default"`
}

// WasteModel is the species-level excretion / waste parameterization.
// Inputs to D-6 (Feed-to-Waste Estimator).
type WasteModel struct {
	ExcretionRatio              float64 `yaml:"excretion_ratio" json:"excretion_ratio"`
	NH3ExcretionPerKgFeed       float64 `yaml:"nh3_excretion_per_kg_feed" json:"nh3_excretion_per_kg_feed"`
	PExcretionPerKgFeed         float64 `yaml:"p_excretion_per_kg_feed" json:"p_excretion_per_kg_feed"`
	TypicalConsumptionWindowSec int     `yaml:"typical_consumption_window_sec" json:"typical_consumption_window_sec"`
}

// Profile is the per-species record.
type Profile struct {
	DisplayName     string                    `yaml:"display_name" json:"display_name"`
	LifecycleStages map[string]LifecycleStage `yaml:"lifecycle_stages" json:"lifecycle_stages"`
	WasteModel      WasteModel                `yaml:"waste_model" json:"waste_model"`
	Source          string                    `yaml:"source" json:"source,omitempty"`                 // 'default' | 'override' | 'calibrated'
	FAOASFISCode    string                    `yaml:"fao_asfis_code" json:"fao_asfis_code,omitempty"` // GDST KDE: FAO ASFIS 3-alpha (예: RSE)
	ScientificName  string                    `yaml:"scientific_name" json:"scientific_name,omitempty"`
}

// Config — species_profiles.yaml root structure.
type Config struct {
	SpeciesProfiles map[string]Profile `yaml:"species_profiles"`
}
