package site

import (
	"os"
	"testing"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoadSitesLand_Valid(t *testing.T) {
	path := writeYAML(t, `
sites_land:
  - site_id: site_land_01
    farm_id: farm_001
    name: Test RAS
    timezone: Asia/Seoul
`)
	sites, err := LoadSitesLand(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	if sites[0].SiteID != "site_land_01" {
		t.Errorf("expected site_id=site_land_01, got %q", sites[0].SiteID)
	}
}

func TestLoadSitesLand_TimezoneDefault(t *testing.T) {
	path := writeYAML(t, `
sites_land:
  - site_id: site_land_tz
    farm_id: farm_001
    name: TZ Test
`)
	sites, err := LoadSitesLand(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sites[0].Timezone != "Asia/Seoul" {
		t.Errorf("expected default timezone=Asia/Seoul, got %q", sites[0].Timezone)
	}
}

func TestLoadSitesLand_Empty(t *testing.T) {
	path := writeYAML(t, `sites_land: []`)
	sites, err := LoadSitesLand(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestLoadSitesLand_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	sites, err := LoadSitesLand(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestLoadSitesLand_MissingSiteID(t *testing.T) {
	path := writeYAML(t, `
sites_land:
  - farm_id: farm_001
    name: NoID
`)
	_, err := LoadSitesLand(path)
	if err == nil {
		t.Fatal("expected error for missing site_id")
	}
}

func TestLoadSitesLand_MissingFarmID(t *testing.T) {
	path := writeYAML(t, `
sites_land:
  - site_id: site_land_01
    name: NoFarm
`)
	_, err := LoadSitesLand(path)
	if err == nil {
		t.Fatal("expected error for missing farm_id")
	}
}

func TestLoadSitesLand_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
sites_land:
  - site_id: dup_site
    farm_id: farm_001
  - site_id: dup_site
    farm_id: farm_002
`)
	_, err := LoadSitesLand(path)
	if err == nil {
		t.Fatal("expected error for duplicate site_id")
	}
}

func TestLoadSitesMarine_Valid(t *testing.T) {
	path := writeYAML(t, `
sites_marine:
  - site_id: site_marine_01
    farm_id: farm_001
    name: Test Marine
    timezone: Asia/Seoul
    location:
      gps_points: []
      heading_deg: 0
`)
	sites, err := LoadSitesMarine(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 marine site, got %d", len(sites))
	}
}

func TestLoadSitesMarine_Empty(t *testing.T) {
	path := writeYAML(t, `sites_marine: []`)
	sites, err := LoadSitesMarine(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestLoadSitesMarine_MissingFarmID(t *testing.T) {
	path := writeYAML(t, `
sites_marine:
  - site_id: site_marine_01
    name: NoFarm
`)
	_, err := LoadSitesMarine(path)
	if err == nil {
		t.Fatal("expected error for missing farm_id")
	}
}

func TestLoadSitesMarine_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
sites_marine:
  - site_id: dup_marine
    farm_id: farm_001
  - site_id: dup_marine
    farm_id: farm_002
`)
	_, err := LoadSitesMarine(path)
	if err == nil {
		t.Fatal("expected error for duplicate site_id")
	}
}
