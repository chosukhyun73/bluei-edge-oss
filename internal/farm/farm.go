package farm

// Farm is the top-level business entity (operator, license, certifications).
// Identified by farm_id (operator-chosen short identifier).
type Farm struct {
	FarmID         string         `yaml:"farm_id" json:"farm_id"`
	LicenseNo      string         `yaml:"license_no" json:"license_no"`
	Operator       string         `yaml:"operator" json:"operator"`
	Certifications []string       `yaml:"certifications" json:"certifications"`
	Sites          []string       `yaml:"sites" json:"sites"`
	Metadata       map[string]any `yaml:"metadata" json:"metadata,omitempty"`
}

// Config — farms.yaml root structure.
type Config struct {
	Farms []Farm `yaml:"farms"`
}
