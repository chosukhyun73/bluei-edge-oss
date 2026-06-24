package controller

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

func TestLoadControllers_Valid(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - controller_id: ctrl_01
    mac_address: "94:B9:7E:C2:F1:3C"
    firmware_version: v0.1.0
    registered_at: 2026-05-01T00:00:00Z
    status: pending
`)
	ctrls, err := LoadControllers(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctrls) != 1 {
		t.Fatalf("expected 1 controller, got %d", len(ctrls))
	}
	if ctrls[0].ControllerID != "ctrl_01" {
		t.Errorf("expected controller_id=ctrl_01, got %q", ctrls[0].ControllerID)
	}
}

func TestLoadControllers_StatusDefault(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - controller_id: ctrl_default
    mac_address: "AA:BB:CC:DD:EE:FF"
    registered_at: 2026-05-01T00:00:00Z
`)
	ctrls, err := LoadControllers(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctrls[0].Status != StatusPending {
		t.Errorf("expected default status=pending, got %q", ctrls[0].Status)
	}
}

func TestLoadControllers_Empty(t *testing.T) {
	path := writeYAML(t, `controllers: []`)
	ctrls, err := LoadControllers(path)
	if err != nil {
		t.Fatalf("expected no error for empty list, got %v", err)
	}
	if len(ctrls) != 0 {
		t.Errorf("expected 0 controllers, got %d", len(ctrls))
	}
}

func TestLoadControllers_EmptyFile(t *testing.T) {
	path := writeYAML(t, ``)
	ctrls, err := LoadControllers(path)
	if err != nil {
		t.Fatalf("expected no error for empty file, got %v", err)
	}
	if len(ctrls) != 0 {
		t.Errorf("expected 0 controllers, got %d", len(ctrls))
	}
}

func TestLoadControllers_MissingControllerID(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - mac_address: "94:B9:7E:C2:F1:3C"
    registered_at: 2026-05-01T00:00:00Z
`)
	_, err := LoadControllers(path)
	if err == nil {
		t.Fatal("expected error for missing controller_id")
	}
}

func TestLoadControllers_MissingMAC(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - controller_id: ctrl_01
    registered_at: 2026-05-01T00:00:00Z
`)
	_, err := LoadControllers(path)
	if err == nil {
		t.Fatal("expected error for missing mac_address")
	}
}

func TestLoadControllers_InvalidMAC(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - controller_id: ctrl_01
    mac_address: "not-a-mac"
    registered_at: 2026-05-01T00:00:00Z
`)
	_, err := LoadControllers(path)
	if err == nil {
		t.Fatal("expected error for invalid mac_address")
	}
}

func TestLoadControllers_InvalidStatus(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - controller_id: ctrl_01
    mac_address: "94:B9:7E:C2:F1:3C"
    registered_at: 2026-05-01T00:00:00Z
    status: unknown_status
`)
	_, err := LoadControllers(path)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestLoadControllers_DuplicateID(t *testing.T) {
	path := writeYAML(t, `
controllers:
  - controller_id: dup_ctrl
    mac_address: "94:B9:7E:C2:F1:3C"
    registered_at: 2026-05-01T00:00:00Z
  - controller_id: dup_ctrl
    mac_address: "AA:BB:CC:DD:EE:FF"
    registered_at: 2026-05-01T00:00:00Z
`)
	_, err := LoadControllers(path)
	if err == nil {
		t.Fatal("expected error for duplicate controller_id")
	}
}
