package sensor

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

func TestLoadSensors_Valid(t *testing.T) {
	path := writeYAML(t, `
sensors:
  - sensor_id: wq_01
    sensor_type: water_quality
    site_id: site_01
    position: in_tank
    capabilities:
      - temperature
      - ph
`)
	sens, err := LoadSensors(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sens) != 1 {
		t.Fatalf("expected 1 sensor, got %d", len(sens))
	}
	if sens[0].SensorID != "wq_01" {
		t.Errorf("expected sensor_id=wq_01, got %q", sens[0].SensorID)
	}
}

func TestLoadSensors_Empty(t *testing.T) {
	path := writeYAML(t, `sensors: []`)
	sens, err := LoadSensors(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(sens) != 0 {
		t.Errorf("expected 0 sensors, got %d", len(sens))
	}
}

func TestLoadSensors_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	sens, err := LoadSensors(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(sens) != 0 {
		t.Errorf("expected 0 sensors, got %d", len(sens))
	}
}

func TestLoadSensors_MissingSensorID(t *testing.T) {
	path := writeYAML(t, `
sensors:
  - sensor_type: water_quality
`)
	_, err := LoadSensors(path)
	if err == nil {
		t.Fatal("expected error for missing sensor_id")
	}
}

func TestLoadSensors_MissingSensorType(t *testing.T) {
	path := writeYAML(t, `
sensors:
  - sensor_id: wq_01
`)
	_, err := LoadSensors(path)
	if err == nil {
		t.Fatal("expected error for missing sensor_type")
	}
}

func TestLoadSensors_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
sensors:
  - sensor_id: dup_sensor
    sensor_type: water_quality
  - sensor_id: dup_sensor
    sensor_type: feed_weight
`)
	_, err := LoadSensors(path)
	if err == nil {
		t.Fatal("expected error for duplicate sensor_id")
	}
}
