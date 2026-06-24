package farm

import (
	"os"
	"path/filepath"
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

func TestLoadFarms_Valid(t *testing.T) {
	path := writeYAML(t, `
farms:
  - farm_id: farm_001
    operator: TestOp
    license_no: "LIC-001"
    certifications:
      - asc
    sites:
      - site_001
`)
	farms, err := LoadFarms(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(farms) != 1 {
		t.Fatalf("expected 1 farm, got %d", len(farms))
	}
	if farms[0].FarmID != "farm_001" {
		t.Errorf("expected farm_id=farm_001, got %q", farms[0].FarmID)
	}
}

func TestLoadFarms_Empty(t *testing.T) {
	path := writeYAML(t, `farms: []`)
	farms, err := LoadFarms(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(farms) != 0 {
		t.Errorf("expected 0 farms, got %d", len(farms))
	}
}

func TestLoadFarms_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	farms, err := LoadFarms(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(farms) != 0 {
		t.Errorf("expected 0 farms, got %d", len(farms))
	}
}

func TestLoadFarms_MissingFarmID(t *testing.T) {
	path := writeYAML(t, `
farms:
  - operator: TestOp
`)
	_, err := LoadFarms(path)
	if err == nil {
		t.Fatal("expected error for missing farm_id")
	}
}

func TestLoadFarms_MissingOperator(t *testing.T) {
	path := writeYAML(t, `
farms:
  - farm_id: farm_001
`)
	_, err := LoadFarms(path)
	if err == nil {
		t.Fatal("expected error for missing operator")
	}
}

func TestLoadFarms_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
farms:
  - farm_id: dup_farm
    operator: Op1
  - farm_id: dup_farm
    operator: Op2
`)
	_, err := LoadFarms(path)
	if err == nil {
		t.Fatal("expected error for duplicate farm_id")
	}
}

func TestLoadFarms_IDTooLong(t *testing.T) {
	longID := filepath.Join("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") // 63 a's
	longID = longID + "bb"                                                                     // 65 chars total
	path := writeYAML(t, "farms:\n  - farm_id: "+longID+"\n    operator: Op\n")
	_, err := LoadFarms(path)
	if err == nil {
		t.Fatal("expected error for farm_id > 64 chars")
	}
}

func TestLoadFarms_FileNotFound(t *testing.T) {
	_, err := LoadFarms("/nonexistent/path/farms.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
