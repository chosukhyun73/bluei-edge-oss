package plantform

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed defaults/*.yaml
var defaultFS embed.FS

// Pack is a built-in set of default forms for one country and plant class.
// The plant copies the forms it wants into its own edition and changes them there.
type Pack struct {
	ID    string `json:"id" yaml:"id"`
	Name  string `json:"name" yaml:"name"`
	Forms []Form `json:"forms" yaml:"forms"`
}

// Packs returns all built-in packs ordered by id.
func Packs() ([]Pack, error) {
	entries, err := defaultFS.ReadDir("defaults")
	if err != nil {
		return nil, err
	}
	var out []Pack
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := defaultFS.ReadFile("defaults/" + e.Name())
		if err != nil {
			return nil, err
		}
		var p Pack
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true) // 따옴표 없는 쉼표로 문장이 잘리면 모르는 키가 생긴다 — 여기서 잡는다
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("form pack %s: %w", e.Name(), err)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LoadPack returns the pack with the given id, or nil if there is none.
func LoadPack(id string) (*Pack, error) {
	all, err := Packs()
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
