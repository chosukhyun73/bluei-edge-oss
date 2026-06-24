package wtg

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

func TestLoadGroups_Valid(t *testing.T) {
	path := writeYAML(t, `
water_treatment_groups:
  - wtg_id: wtg_01
    site_id: site_01
    name: WTG 1
    tank_ids:
      - tank_01
      - tank_02
    capacity:
      nh3_processing_kg_per_h: 0.5
`)
	groups, err := LoadGroups(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].WTGID != "wtg_01" {
		t.Errorf("expected wtg_id=wtg_01, got %q", groups[0].WTGID)
	}
}

func TestLoadGroups_Empty(t *testing.T) {
	path := writeYAML(t, `water_treatment_groups: []`)
	groups, err := LoadGroups(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestLoadGroups_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	groups, err := LoadGroups(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestLoadGroups_MissingWTGID(t *testing.T) {
	path := writeYAML(t, `
water_treatment_groups:
  - site_id: site_01
`)
	_, err := LoadGroups(path)
	if err == nil {
		t.Fatal("expected error for missing wtg_id")
	}
}

func TestLoadGroups_MissingSiteID(t *testing.T) {
	path := writeYAML(t, `
water_treatment_groups:
  - wtg_id: wtg_01
`)
	_, err := LoadGroups(path)
	if err == nil {
		t.Fatal("expected error for missing site_id")
	}
}

func TestLoadGroups_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
water_treatment_groups:
  - wtg_id: dup_wtg
    site_id: site_01
  - wtg_id: dup_wtg
    site_id: site_02
`)
	_, err := LoadGroups(path)
	if err == nil {
		t.Fatal("expected error for duplicate wtg_id")
	}
}

func TestLoadGroups_DuplicateTankInGroup(t *testing.T) {
	path := writeYAML(t, `
water_treatment_groups:
  - wtg_id: wtg_01
    site_id: site_01
    tank_ids:
      - tank_01
      - tank_01
`)
	_, err := LoadGroups(path)
	if err == nil {
		t.Fatal("expected error for duplicate tank_id in group")
	}
}

func TestLoadGroups_NegativeNH3(t *testing.T) {
	path := writeYAML(t, `
water_treatment_groups:
  - wtg_id: wtg_01
    site_id: site_01
    capacity:
      nh3_processing_kg_per_h: -1
`)
	_, err := LoadGroups(path)
	if err == nil {
		t.Fatal("expected error for negative nh3_processing_kg_per_h")
	}
}
