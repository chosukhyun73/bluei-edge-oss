package controller

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var macAddressRe = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)

var validStatuses = map[Status]bool{
	StatusPending:  true,
	StatusActive:   true,
	StatusDisabled: true,
	StatusFault:    true,
}

// LoadControllers reads and validates the controllers sub-config.
// Empty file or empty list is valid.
func LoadControllers(path string) ([]Controller, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read controllers config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse controllers config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, c := range cfg.Controllers {
		if c.ControllerID == "" {
			return nil, fmt.Errorf("controllers[%d]: controller_id is required", i)
		}
		if len(c.ControllerID) > 64 {
			return nil, fmt.Errorf("controllers[%d]: controller_id too long (max 64)", i)
		}
		if seen[c.ControllerID] {
			return nil, fmt.Errorf("controllers[%d]: duplicate controller_id %q", i, c.ControllerID)
		}
		seen[c.ControllerID] = true
		if c.MACAddress == "" {
			return nil, fmt.Errorf("controllers[%d] %q: mac_address is required", i, c.ControllerID)
		}
		if !macAddressRe.MatchString(c.MACAddress) {
			return nil, fmt.Errorf("controllers[%d] %q: mac_address %q is not a valid MAC (expected XX:XX:XX:XX:XX:XX)", i, c.ControllerID, c.MACAddress)
		}
		// status 기본값 처리 (비어있으면 pending)
		if cfg.Controllers[i].Status == "" {
			cfg.Controllers[i].Status = StatusPending
		} else if !validStatuses[cfg.Controllers[i].Status] {
			return nil, fmt.Errorf("controllers[%d] %q: status %q must be one of pending/active/disabled/fault", i, c.ControllerID, c.Status)
		}
	}
	return cfg.Controllers, nil
}
