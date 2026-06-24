package config

import (
	"crypto/sha256"
	"fmt"
	"os"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/controller"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/sensor"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/species"
	"bluei.kr/edge/internal/wtg"
	"gopkg.in/yaml.v3"
)

// Load reads the edge config file and its sub-configs (devices, rules, logging).
func Load(path string) (*Config, []byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, "", fmt.Errorf("parse config %s: %w", path, err)
	}

	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	return &cfg, data, hash, nil
}

// LoadDevices reads the devices sub-config.
func LoadDevices(path string) (*DevicesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read devices config %s: %w", path, err)
	}
	var dc DevicesConfig
	if err := yaml.Unmarshal(data, &dc); err != nil {
		return nil, fmt.Errorf("parse devices config %s: %w", path, err)
	}
	return &dc, nil
}

// LoadTanks reads the tanks sub-config.
func LoadTanks(path string) (*TanksConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tanks config %s: %w", path, err)
	}
	var tc TanksConfig
	if err := yaml.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("parse tanks config %s: %w", path, err)
	}
	return &tc, nil
}

// LoadRules reads the rules sub-config.
func LoadRules(path string) (*RulesConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rules config %s: %w", path, err)
	}
	var rc RulesConfig
	if err := yaml.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parse rules config %s: %w", path, err)
	}
	return &rc, nil
}

// LoadLogging reads the logging sub-config.
func LoadLogging(path string) (*LoggingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read logging config %s: %w", path, err)
	}
	var lc LoggingConfig
	if err := yaml.Unmarshal(data, &lc); err != nil {
		return nil, fmt.Errorf("parse logging config %s: %w", path, err)
	}
	return &lc, nil
}

// LoadFarms delegates to the farm package loader.
func LoadFarms(path string) ([]farm.Farm, error) { return farm.LoadFarms(path) }

// LoadSitesLand delegates to the site package loader.
func LoadSitesLand(path string) ([]site.SiteLand, error) { return site.LoadSitesLand(path) }

// LoadSitesMarine delegates to the site package loader.
func LoadSitesMarine(path string) ([]site.SiteMarine, error) { return site.LoadSitesMarine(path) }

// LoadWTGs delegates to the wtg package loader.
func LoadWTGs(path string) ([]wtg.Group, error) { return wtg.LoadGroups(path) }

// LoadActuators delegates to the actuator package loader.
func LoadActuators(path string) ([]actuator.Actuator, error) { return actuator.LoadActuators(path) }

// LoadSensors delegates to the sensor package loader.
func LoadSensors(path string) ([]sensor.Sensor, error) { return sensor.LoadSensors(path) }

// LoadControllers delegates to the controller package loader.
func LoadControllers(path string) ([]controller.Controller, error) {
	return controller.LoadControllers(path)
}

// LoadSpeciesProfiles delegates to the species package loader.
func LoadSpeciesProfiles(path string) (map[string]species.Profile, error) {
	return species.LoadSpeciesProfiles(path)
}

// LoadVisionAlgorithms reads the vision algorithm library config.
func LoadVisionAlgorithms(path string) (*VisionAlgorithmsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vision algorithms config %s: %w", path, err)
	}
	var vc VisionAlgorithmsConfig
	if err := yaml.Unmarshal(data, &vc); err != nil {
		return nil, fmt.Errorf("parse vision algorithms config %s: %w", path, err)
	}
	return &vc, nil
}
