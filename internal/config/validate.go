package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// ValidationError collects all config validation failures.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return "config validation failed:\n  " + strings.Join(e.Errors, "\n  ")
}

func (e *ValidationError) add(msg string, args ...any) {
	e.Errors = append(e.Errors, fmt.Sprintf(msg, args...))
}

func (e *ValidationError) OK() bool { return len(e.Errors) == 0 }

// Validate checks the loaded Config for required fields and invariants.
// Returns a *ValidationError with all failures (not just the first).
func Validate(cfg *Config) error {
	ve := &ValidationError{}

	if cfg.Site.SiteID == "" {
		ve.add("site.site_id is required")
	}
	if cfg.Edge.EdgeID == "" {
		ve.add("edge.edge_id is required")
	}
	if cfg.Edge.DataDir == "" {
		ve.add("edge.data_dir is required")
	} else if err := ensureWritable(cfg.Edge.DataDir); err != nil {
		ve.add("edge.data_dir not writable: %v", err)
	}

	if cfg.Storage.SQLitePath == "" {
		ve.add("storage.sqlite_path is required")
	} else {
		dir := filepath.Dir(cfg.Storage.SQLitePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			ve.add("cannot create sqlite parent dir %s: %v", dir, err)
		}
	}

	if cfg.API.Port < 1 || cfg.API.Port > 65535 {
		ve.add("api.port must be 1-65535, got %d", cfg.API.Port)
	}

	// LAN/0.0.0.0 bind requires auth
	if cfg.API.BindHost != "" && cfg.API.BindHost != "127.0.0.1" && cfg.API.BindHost != "localhost" {
		ip := net.ParseIP(cfg.API.BindHost)
		if ip == nil || !ip.IsLoopback() {
			if !cfg.API.Auth.Enabled {
				ve.add("api.auth.enabled must be true when bind_host is not loopback (got %s)", cfg.API.BindHost)
			}
		}
	}

	// Automatic control requires explicit safety policy — reject for Phase 1
	if cfg.Control.AutomaticCommandsEnabled {
		ve.add("control.automatic_commands_enabled=true is not allowed in Phase 1")
	}

	if cfg.Inference.Enabled {
		if cfg.Inference.Mode != "ollama" {
			ve.add("inference.mode must be ollama when inference is enabled, got %q", cfg.Inference.Mode)
		}
		if cfg.Inference.Ollama.Endpoint == "" {
			ve.add("inference.ollama.endpoint is required when inference is enabled")
		}
		if cfg.Inference.Ollama.Model == "" {
			ve.add("inference.ollama.model is required when inference is enabled")
		}
		if cfg.Inference.Ollama.TimeoutSec < 0 {
			ve.add("inference.ollama.timeout_sec must be >= 0")
		}
	}

	if cfg.Tanks.ConfigPath == "" {
		ve.add("tanks.config_path is required")
	}

	// DecisionPolicy grace_minutes — 1분 미만은 사실상 즉시 실행 (의미 없음).
	if cfg.DecisionPolicy.GraceMinutes != 0 && cfg.DecisionPolicy.GraceMinutes < 1 {
		ve.add("decision_policy.grace_minutes must be >= 1, got %d", cfg.DecisionPolicy.GraceMinutes)
	}

	// WeightHistoryWorker — enabled 이면 interval_sec >= 60 필요.
	if cfg.WeightHistoryWorker.Enabled && cfg.WeightHistoryWorker.IntervalSec < 60 {
		ve.add("weight_history_worker.interval_sec must be >= 60 when enabled, got %d", cfg.WeightHistoryWorker.IntervalSec)
	}
	if cfg.WeightHistoryWorker.InitialDelaySec < 0 {
		ve.add("weight_history_worker.initial_delay_sec must be >= 0, got %d", cfg.WeightHistoryWorker.InitialDelaySec)
	}

	// Collector adapter id uniqueness
	seen := map[string]bool{}
	for _, a := range cfg.Collector.Adapters {
		if seen[a.ID] {
			ve.add("collector adapter id %q is duplicated", a.ID)
		}
		seen[a.ID] = true
	}

	if !ve.OK() {
		return ve
	}
	return nil
}

func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".write_probe")
	f, err := os.Create(probe)
	if err != nil {
		return err
	}
	f.Close()
	os.Remove(probe)
	return nil
}
