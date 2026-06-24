package site

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadSitesLand reads and validates the sites_land sub-config.
// Empty file or empty list is valid.
func LoadSitesLand(path string) ([]SiteLand, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sites_land config %s: %w", path, err)
	}
	var cfg ConfigLand
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse sites_land config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, s := range cfg.SitesLand {
		if s.SiteID == "" {
			return nil, fmt.Errorf("sites_land[%d]: site_id is required", i)
		}
		if len(s.SiteID) > 64 {
			return nil, fmt.Errorf("sites_land[%d]: site_id too long (max 64)", i)
		}
		if seen[s.SiteID] {
			return nil, fmt.Errorf("sites_land[%d]: duplicate site_id %q", i, s.SiteID)
		}
		seen[s.SiteID] = true
		if s.FarmID == "" {
			return nil, fmt.Errorf("sites_land[%d] %q: farm_id is required", i, s.SiteID)
		}
		// timezone 기본값
		if cfg.SitesLand[i].Timezone == "" {
			cfg.SitesLand[i].Timezone = "Asia/Seoul"
		}
	}
	return cfg.SitesLand, nil
}

// LoadSitesMarine reads and validates the sites_marine sub-config.
// Empty file or empty list is valid.
func LoadSitesMarine(path string) ([]SiteMarine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sites_marine config %s: %w", path, err)
	}
	var cfg ConfigMarine
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse sites_marine config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, s := range cfg.SitesMarine {
		if s.SiteID == "" {
			return nil, fmt.Errorf("sites_marine[%d]: site_id is required", i)
		}
		if len(s.SiteID) > 64 {
			return nil, fmt.Errorf("sites_marine[%d]: site_id too long (max 64)", i)
		}
		if seen[s.SiteID] {
			return nil, fmt.Errorf("sites_marine[%d]: duplicate site_id %q", i, s.SiteID)
		}
		seen[s.SiteID] = true
		if s.FarmID == "" {
			return nil, fmt.Errorf("sites_marine[%d] %q: farm_id is required", i, s.SiteID)
		}
		// timezone 기본값
		if cfg.SitesMarine[i].Timezone == "" {
			cfg.SitesMarine[i].Timezone = "Asia/Seoul"
		}
	}
	return cfg.SitesMarine, nil
}
