package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"bluei.kr/edge/internal/config"
)

func TestLoadAndValidate_Example(t *testing.T) {
	cfg, _, hash, err := config.Load("../../configs/edge.example.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Site.SiteID == "" {
		t.Error("site_id is empty")
	}
	if cfg.Edge.EdgeID == "" {
		t.Error("edge_id is empty")
	}
	if hash == "" {
		t.Error("hash is empty")
	}
}

func TestValidate_MissingSiteID(t *testing.T) {
	cfg := &config.Config{}
	err := config.Validate(cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ve, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Site:    config.SiteConfig{SiteID: "s1"},
		Edge:    config.EdgeConfig{EdgeID: "e1", DataDir: dir},
		API:     config.APIConfig{Port: 99999},
		Storage: config.StorageConfig{SQLitePath: filepath.Join(dir, "e.db")},
	}
	err := config.Validate(cfg)
	if err == nil {
		t.Fatal("expected port validation error")
	}
}

func TestValidate_AutomaticControlBlocked(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Site:    config.SiteConfig{SiteID: "s1"},
		Edge:    config.EdgeConfig{EdgeID: "e1", DataDir: dir},
		API:     config.APIConfig{Port: 8080, BindHost: "127.0.0.1"},
		Storage: config.StorageConfig{SQLitePath: filepath.Join(dir, "e.db")},
		Control: config.ControlConfig{AutomaticCommandsEnabled: true},
	}
	err := config.Validate(cfg)
	if err == nil {
		t.Fatal("expected automatic_commands_enabled to be rejected")
	}
}

func TestLoadInferenceConfig_Example(t *testing.T) {
	cfg, _, _, err := config.Load("../../configs/edge.example.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Inference.Enabled {
		t.Fatal("example inference must be disabled by default")
	}
	if cfg.Inference.Mode != "disabled" {
		t.Fatalf("unexpected inference mode: %q", cfg.Inference.Mode)
	}
	if cfg.Inference.Ollama.Endpoint != "http://127.0.0.1:11435" {
		t.Fatalf("unexpected ollama endpoint: %q", cfg.Inference.Ollama.Endpoint)
	}
	if cfg.Inference.Ollama.Model != "gemma4:26b" {
		t.Fatalf("unexpected ollama model: %q", cfg.Inference.Ollama.Model)
	}
}

func TestValidate_InferenceEnabledRequiresOllamaConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Site:    config.SiteConfig{SiteID: "s1"},
		Edge:    config.EdgeConfig{EdgeID: "e1", DataDir: dir},
		API:     config.APIConfig{Port: 8080, BindHost: "127.0.0.1"},
		Storage: config.StorageConfig{SQLitePath: filepath.Join(dir, "e.db")},
		Tanks:   config.TanksRef{ConfigPath: "./configs/tanks.example.yaml"},
		Inference: config.InferenceConfig{
			Enabled: true,
			Mode:    "ollama",
		},
	}
	err := config.Validate(cfg)
	if err == nil {
		t.Fatal("expected enabled inference without ollama config to fail")
	}
}

func TestLoadDevices(t *testing.T) {
	dc, err := config.LoadDevices("../../configs/devices.example.yaml")
	if err != nil {
		t.Fatalf("LoadDevices: %v", err)
	}
	if len(dc.Devices) == 0 {
		t.Error("expected at least one device")
	}
}

func TestLoadRules(t *testing.T) {
	rc, err := config.LoadRules("../../configs/rules.example.yaml")
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	if len(rc.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, _, _, err := config.Load("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func init() {
	// Ensure test temp dirs are always cleaned up
	os.Setenv("TMPDIR", os.TempDir())
}
