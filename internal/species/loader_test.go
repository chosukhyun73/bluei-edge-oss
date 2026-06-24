package species

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

func TestLoadSpeciesProfiles_Valid(t *testing.T) {
	path := writeYAML(t, `
species_profiles:
  atlantic_salmon:
    display_name: "대서양연어"
    lifecycle_stages:
      fry:
        fcr_target: 1.0
        weight_range_g: [0, 100]
        feed_type: fry_micropellet
        feeding_pattern_default:
          pulse_duration_ms: 1500
          gap_ms: 45000
          total_pulses: 3
    waste_model:
      excretion_ratio: 0.30
      nh3_excretion_per_kg_feed: 35
      p_excretion_per_kg_feed: 8
      typical_consumption_window_sec: 60
`)
	profiles, err := LoadSpeciesProfiles(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p, ok := profiles["atlantic_salmon"]
	if !ok {
		t.Fatal("expected key atlantic_salmon")
	}
	if p.DisplayName != "대서양연어" {
		t.Errorf("expected display_name=대서양연어, got %q", p.DisplayName)
	}
}

func TestLoadSpeciesProfiles_Empty(t *testing.T) {
	path := writeYAML(t, `species_profiles: {}`)
	profiles, err := LoadSpeciesProfiles(path)
	if err != nil {
		t.Fatalf("expected no error for empty map, got %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestLoadSpeciesProfiles_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	profiles, err := LoadSpeciesProfiles(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestLoadSpeciesProfiles_MissingDisplayName(t *testing.T) {
	path := writeYAML(t, `
species_profiles:
  test_fish:
    lifecycle_stages: {}
    waste_model:
      excretion_ratio: 0.30
`)
	_, err := LoadSpeciesProfiles(path)
	if err == nil {
		t.Fatal("expected error for missing display_name")
	}
}

func TestLoadSpeciesProfiles_InvalidWeightRange(t *testing.T) {
	path := writeYAML(t, `
species_profiles:
  bad_fish:
    display_name: "Bad Fish"
    lifecycle_stages:
      growout:
        fcr_target: 1.0
        weight_range_g: [500, 100]
        feed_type: pellet
        feeding_pattern_default:
          pulse_duration_ms: 1000
          gap_ms: 1000
          total_pulses: 1
    waste_model:
      excretion_ratio: 0.30
      nh3_excretion_per_kg_feed: 35
      p_excretion_per_kg_feed: 8
      typical_consumption_window_sec: 60
`)
	_, err := LoadSpeciesProfiles(path)
	if err == nil {
		t.Fatal("expected error for weight_range_g[0] >= weight_range_g[1]")
	}
}
