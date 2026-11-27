package config

import (
	"net"
	"os"
	"path/filepath"
)

// ValidateProcessing checks the config of processing-bluei (processing-plant
// service). It is the part of Validate that does not concern aquaculture:
// identity, storage and the API. Tanks, collectors and control are not used.
func ValidateProcessing(cfg *Config) error {
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
	} else if err := os.MkdirAll(filepath.Dir(cfg.Storage.SQLitePath), 0o755); err != nil {
		ve.add("cannot create sqlite parent dir: %v", err)
	}

	if cfg.API.Port < 1 || cfg.API.Port > 65535 {
		ve.add("api.port must be 1-65535, got %d", cfg.API.Port)
	}
	// LAN/0.0.0.0 bind requires auth
	if cfg.API.BindHost != "" && cfg.API.BindHost != "127.0.0.1" && cfg.API.BindHost != "localhost" {
		if ip := net.ParseIP(cfg.API.BindHost); (ip == nil || !ip.IsLoopback()) && !cfg.API.Auth.Enabled {
			ve.add("api.auth.enabled must be true when bind_host is not loopback (got %s)", cfg.API.BindHost)
		}
	}

	if !ve.OK() {
		return ve
	}
	return nil
}
