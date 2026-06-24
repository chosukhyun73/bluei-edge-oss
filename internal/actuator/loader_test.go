package actuator

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

func TestLoadActuators_Valid(t *testing.T) {
	path := writeYAML(t, `
actuators:
  - device_id: feeder_01
    device_type: feeder
    tank_id: tank_01
    capabilities:
      - feed.start
`)
	acts, err := LoadActuators(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("expected 1 actuator, got %d", len(acts))
	}
	if acts[0].DeviceID != "feeder_01" {
		t.Errorf("expected device_id=feeder_01, got %q", acts[0].DeviceID)
	}
}

func TestLoadActuators_Empty(t *testing.T) {
	path := writeYAML(t, `actuators: []`)
	acts, err := LoadActuators(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(acts) != 0 {
		t.Errorf("expected 0 actuators, got %d", len(acts))
	}
}

func TestLoadActuators_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	acts, err := LoadActuators(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(acts) != 0 {
		t.Errorf("expected 0 actuators, got %d", len(acts))
	}
}

func TestLoadActuators_MissingDeviceID(t *testing.T) {
	path := writeYAML(t, `
actuators:
  - device_type: feeder
    tank_id: tank_01
`)
	_, err := LoadActuators(path)
	if err == nil {
		t.Fatal("expected error for missing device_id")
	}
}

func TestLoadActuators_MissingDeviceType(t *testing.T) {
	path := writeYAML(t, `
actuators:
  - device_id: act_01
`)
	_, err := LoadActuators(path)
	if err == nil {
		t.Fatal("expected error for missing device_type")
	}
}

func TestLoadActuators_FeederWithoutTankID(t *testing.T) {
	path := writeYAML(t, `
actuators:
  - device_id: feeder_01
    device_type: feeder
`)
	_, err := LoadActuators(path)
	if err == nil {
		t.Fatal("expected error: feeder without tank_id")
	}
}

func TestLoadActuators_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
actuators:
  - device_id: dup_act
    device_type: pump
    site_id: site_01
  - device_id: dup_act
    device_type: pump
    site_id: site_02
`)
	_, err := LoadActuators(path)
	if err == nil {
		t.Fatal("expected error for duplicate device_id")
	}
}
