package wtg

// SharedEquipment maps logical roles to device_ids in the actuators table.
type SharedEquipment struct {
	HeatPump        string `yaml:"heat_pump" json:"heat_pump,omitempty"`
	UVSterilizer    string `yaml:"uv_sterilizer" json:"uv_sterilizer,omitempty"`
	CirculationPump string `yaml:"circulation_pump" json:"circulation_pump,omitempty"`
	Biofilter       string `yaml:"biofilter" json:"biofilter,omitempty"`
}

// Capacity describes the steady-state treatment capacity of a WTG.
// Inputs to D-7.
type Capacity struct {
	VolumeM3            float64 `yaml:"volume_m3" json:"volume_m3"`
	NH3ProcessingKgPerH float64 `yaml:"nh3_processing_kg_per_h" json:"nh3_processing_kg_per_h"`
	FlowRateM3PerH      float64 `yaml:"flow_rate_m3_per_h" json:"flow_rate_m3_per_h"`
}

// FeedingPolicy controls how the orchestrator schedules feed cycles across
// the tanks sharing this WTG.
type FeedingPolicy struct {
	MinIntervalBetweenTanksMin   int  `yaml:"min_interval_between_tanks_min" json:"min_interval_between_tanks_min"`
	MaxConcurrentTanks           int  `yaml:"max_concurrent_tanks" json:"max_concurrent_tanks"`
	PauseOnPredictedQualityAlert bool `yaml:"pause_on_predicted_quality_alert" json:"pause_on_predicted_quality_alert"`
}

// Group is a single Water Treatment Group definition.
type Group struct {
	WTGID           string          `yaml:"wtg_id" json:"wtg_id"`
	SiteID          string          `yaml:"site_id" json:"site_id"`
	Name            string          `yaml:"name" json:"name"`
	TankIDs         []string        `yaml:"tank_ids" json:"tank_ids"`
	SharedEquipment SharedEquipment `yaml:"shared_equipment" json:"shared_equipment"`
	IntakeSensor    string          `yaml:"intake_sensor" json:"intake_sensor,omitempty"`
	OutletSensor    string          `yaml:"outlet_sensor" json:"outlet_sensor,omitempty"`
	Capacity        Capacity        `yaml:"capacity" json:"capacity"`
	FeedingPolicy   FeedingPolicy   `yaml:"feeding_policy" json:"feeding_policy"`
}

// Config — water_treatment_groups.yaml root structure.
type Config struct {
	Groups []Group `yaml:"water_treatment_groups"`
}
