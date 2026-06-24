package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// GroupProfile — 양식동/순환시스템 단위. Cage/Tank 들의 상위 논리 그룹.
type GroupProfile struct {
	GroupID     string         `yaml:"group_id" json:"group_id"`
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Color       string         `yaml:"color" json:"color"` // HEX e.g. #22c55e
	Metadata    map[string]any `yaml:"metadata" json:"metadata,omitempty"`
}

// GroupsConfig — groups.yaml 최상단 구조.
type GroupsConfig struct {
	Groups []GroupProfile `yaml:"groups"`
}

// GroupsRef — edge.yaml 에서 groups.yaml 경로를 참조하는 포인터.
type GroupsRef struct {
	ConfigPath string `yaml:"config_path"`
}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// LoadGroups reads and validates the groups sub-config.
func LoadGroups(path string) ([]GroupProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read groups config %s: %w", path, err)
	}
	var gc GroupsConfig
	if err := yaml.Unmarshal(data, &gc); err != nil {
		return nil, fmt.Errorf("parse groups config %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, g := range gc.Groups {
		if g.GroupID == "" {
			return nil, fmt.Errorf("groups[%d]: group_id is required", i)
		}
		if len(g.GroupID) > 64 {
			return nil, fmt.Errorf("groups[%d]: group_id too long (max 64)", i)
		}
		if seen[g.GroupID] {
			return nil, fmt.Errorf("groups[%d]: duplicate group_id %q", i, g.GroupID)
		}
		seen[g.GroupID] = true
		if g.Name == "" {
			return nil, fmt.Errorf("groups[%d] %q: name is required", i, g.GroupID)
		}
		if g.Color != "" && !hexColorRe.MatchString(g.Color) {
			return nil, fmt.Errorf("groups[%d] %q: color must be #RRGGBB hex, got %q", i, g.GroupID, g.Color)
		}
	}
	return gc.Groups, nil
}
