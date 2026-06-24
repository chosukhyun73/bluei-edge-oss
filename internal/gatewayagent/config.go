package gatewayagent

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GatewayID         string         `yaml:"gateway_id"`
	AdapterID         string         `yaml:"adapter_id"`
	EdgeBaseURL       string         `yaml:"edge_base_url"`
	PostTimeoutSec    int            `yaml:"post_timeout_sec"`
	HealthIntervalSec int            `yaml:"health_interval_sec"`
	RetryQueuePath    string         `yaml:"retry_queue_path"`
	Sources           []SourceConfig `yaml:"sources"`
}

type SourceConfig struct {
	SourceID string        `yaml:"source_id"`
	Type     string        `yaml:"type"` // file | serial
	Path     string        `yaml:"path"`
	Baud     int           `yaml:"baud"`
	Defaults FrameDefaults `yaml:"defaults"`
}

type FrameDefaults struct {
	TankID   string `yaml:"tank_id"`
	DeviceID string `yaml:"device_id"`
	SensorID string `yaml:"sensor_id"`
	Metric   string `yaml:"metric"`
	Unit     string `yaml:"unit"`
	Quality  string `yaml:"quality"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read gateway config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse gateway config %s: %w", path, err)
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.AdapterID == "" {
		cfg.AdapterID = cfg.GatewayID
	}
	if cfg.EdgeBaseURL == "" {
		cfg.EdgeBaseURL = "http://127.0.0.1:8080"
	}
	if cfg.PostTimeoutSec <= 0 {
		cfg.PostTimeoutSec = 5
	}
	if cfg.HealthIntervalSec <= 0 {
		cfg.HealthIntervalSec = 30
	}
	if cfg.RetryQueuePath == "" {
		cfg.RetryQueuePath = "storage/queue/lattepanda-gateway.jsonl"
	}
	for i := range cfg.Sources {
		if cfg.Sources[i].Type == "" {
			cfg.Sources[i].Type = "serial"
		}
		if cfg.Sources[i].Baud == 0 {
			cfg.Sources[i].Baud = 115200
		}
		if cfg.Sources[i].Defaults.Quality == "" {
			cfg.Sources[i].Defaults.Quality = "ok"
		}
	}
}

func (c Config) Validate() error {
	if c.GatewayID == "" {
		return fmt.Errorf("gateway_id is required")
	}
	if c.EdgeBaseURL == "" {
		return fmt.Errorf("edge_base_url is required")
	}
	if c.PostTimeoutSec <= 0 || time.Duration(c.PostTimeoutSec)*time.Second <= 0 {
		return fmt.Errorf("post_timeout_sec must be positive")
	}
	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	for _, src := range c.Sources {
		if src.SourceID == "" {
			return fmt.Errorf("source_id is required")
		}
		if src.Type != "file" && src.Type != "serial" {
			return fmt.Errorf("source %s has unsupported type %q", src.SourceID, src.Type)
		}
		if src.Path == "" {
			return fmt.Errorf("source %s path is required", src.SourceID)
		}
	}
	return nil
}
