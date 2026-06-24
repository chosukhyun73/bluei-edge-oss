package site

// SiteType is the kind of physical operating location.
type SiteType string

const (
	TypeLand   SiteType = "land"
	TypeMarine SiteType = "marine"
)

// LandLocation is the address + optional fixed coordinates for a land site.
type LandLocation struct {
	Address     string       `yaml:"address" json:"address"`
	Coordinates *Coordinates `yaml:"coordinates,omitempty" json:"coordinates,omitempty"`
}

// Coordinates is a lat/lon pair (WGS84).
type Coordinates struct {
	Lat float64 `yaml:"lat" json:"lat"`
	Lon float64 `yaml:"lon" json:"lon"`
}

// MarineGPSPoint is one reference point of a marine cage (cages drift, so we keep multiple).
type MarineGPSPoint struct {
	Position  string  `yaml:"position" json:"position"` // 'north' | 'south' | 'east' | 'west' | custom
	Lat       float64 `yaml:"lat" json:"lat"`
	Lon       float64 `yaml:"lon" json:"lon"`
	UpdatedAt string  `yaml:"updated_at" json:"updated_at"` // RFC3339
}

// MarineLocation is the GPS + heading representation for marine sites.
type MarineLocation struct {
	GPSPoints  []MarineGPSPoint `yaml:"gps_points" json:"gps_points"`
	HeadingDeg float64          `yaml:"heading_deg" json:"heading_deg"` // derived from 2 GPS points
}

// SiteLand is a RAS facility.
type SiteLand struct {
	SiteID        string         `yaml:"site_id" json:"site_id"`
	FarmID        string         `yaml:"farm_id" json:"farm_id"`
	Name          string         `yaml:"name" json:"name"`
	Location      LandLocation   `yaml:"location" json:"location"`
	Timezone      string         `yaml:"timezone" json:"timezone"`
	EquipmentList []string       `yaml:"equipment_list" json:"equipment_list"`
	SensorList    []string       `yaml:"sensor_list" json:"sensor_list"`
	Metadata      map[string]any `yaml:"metadata" json:"metadata,omitempty"`
}

// SiteMarine is a marine cage location.
type SiteMarine struct {
	SiteID               string         `yaml:"site_id" json:"site_id"`
	FarmID               string         `yaml:"farm_id" json:"farm_id"`
	Name                 string         `yaml:"name" json:"name"`
	Location             MarineLocation `yaml:"location" json:"location"`
	Timezone             string         `yaml:"timezone" json:"timezone"`
	SensorList           []string       `yaml:"sensor_list" json:"sensor_list"`
	EnvironmentPolicyRef string         `yaml:"environment_policy_ref" json:"environment_policy_ref,omitempty"`
	Metadata             map[string]any `yaml:"metadata" json:"metadata,omitempty"`
}

// ConfigLand — sites_land.yaml root structure.
type ConfigLand struct {
	SitesLand []SiteLand `yaml:"sites_land"`
}

// ConfigMarine — sites_marine.yaml root structure.
type ConfigMarine struct {
	SitesMarine []SiteMarine `yaml:"sites_marine"`
}
