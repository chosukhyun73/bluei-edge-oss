package haccp

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*.yaml
var templateFS embed.FS

// Template is a site-independent starting point for a plan. Its entries are
// unconfirmed: the plant has to set its own limits and confirm each item.
type Template struct {
	ID           string      `json:"id" yaml:"id"`
	Name         string      `json:"name" yaml:"name"`
	ProductGroup string      `json:"product_group" yaml:"product_group"`
	Source       string      `json:"source" yaml:"source"`
	Notice       string      `json:"notice" yaml:"notice"`
	Content      PlanContent `json:"content" yaml:"content"`
}

// ListTemplates returns all built-in templates ordered by id.
func ListTemplates() ([]Template, error) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			return nil, err
		}
		var t Template
		if err := yaml.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("template %s: %w", e.Name(), err)
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LoadTemplate returns the template with the given id, or nil if there is none.
func LoadTemplate(id string) (*Template, error) {
	all, err := ListTemplates()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, nil
}
