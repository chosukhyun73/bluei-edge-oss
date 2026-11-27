// Package plantform holds record-form definitions of a processing plant:
// checklists, logs and single-event sheets. A form is data, not code — the
// plant keeps its own edition of each form and may change it. Built-in
// default packs are only a starting point.
// References: docs/56-haccp-team-leader-work-system-design.md §17, §18.
package plantform

import (
	"fmt"
	"regexp"
)

const (
	KindChecklist = "checklist" // 구역별 문항에 예/아니오로 답한다
	KindLog       = "log"       // 같은 칸에 여러 줄을 적어 간다
	KindSheet     = "sheet"     // 한 건에 한 장 (불만 접수, 교육 일지)
)

// Form is one record form.
type Form struct {
	ID        string    `json:"id" yaml:"id"`
	Name      string    `json:"name" yaml:"name"`
	Kind      string    `json:"kind" yaml:"kind"`
	Cycle     string    `json:"cycle" yaml:"cycle"`   // 주기 (사람이 읽는 문구)
	Owner     string    `json:"owner" yaml:"owner"`   // 담당
	Covers    []string  `json:"covers" yaml:"covers"` // 덮는 평가표 문항: P1~P17(선행요건), H1~H8(HACCP)
	Notes     []Note    `json:"notes,omitempty" yaml:"notes"`
	Sections  []Section `json:"sections,omitempty" yaml:"sections"`     // checklist
	Columns   []Column  `json:"columns,omitempty" yaml:"columns"`       // log
	Rows      []string  `json:"rows,omitempty" yaml:"rows"`             // log: 미리 정해진 행 이름
	BlankRows int       `json:"blank_rows,omitempty" yaml:"blank_rows"` // log: Rows 가 없을 때 빈 행 수
	Fields    []Field   `json:"fields,omitempty" yaml:"fields"`         // sheet
	Deviation bool      `json:"deviation" yaml:"deviation"`             // 이탈 내용·개선조치 칸
	Signers   []string  `json:"signers" yaml:"signers"`                 // 서명란 (역할 이름)
}

// Note is a fixed text printed above the table: limits, method, corrective action.
type Note struct {
	Label string `json:"label" yaml:"label"`
	Text  string `json:"text" yaml:"text"`
}

type Section struct {
	ID        string     `json:"id" yaml:"id"`
	Title     string     `json:"title" yaml:"title"`
	Questions []Question `json:"questions" yaml:"questions"`
}

type Question struct {
	ID   string `json:"id" yaml:"id"`
	Text string `json:"text" yaml:"text"`
}

type Column struct {
	ID    string  `json:"id" yaml:"id"`
	Name  string  `json:"name" yaml:"name"`
	Width float64 `json:"width,omitempty" yaml:"width"` // 열 너비 비율, 0 이면 1
}

type Field struct {
	ID    string `json:"id" yaml:"id"`
	Label string `json:"label" yaml:"label"`
	Lines int    `json:"lines,omitempty" yaml:"lines"` // 적는 칸의 줄 수, 0 이면 1
}

var (
	idRe    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	coverRe = regexp.MustCompile(`^(P([1-9]|1[0-7])|H[1-8])$`)
)

// Validate reports what makes f unusable. An empty result means f can be saved and printed.
func Validate(f *Form) []string {
	var out []string
	bad := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	if !idRe.MatchString(f.ID) {
		bad("id %q: 영문 소문자로 시작하고 소문자·숫자·밑줄만 쓴다", f.ID)
	}
	if f.Name == "" {
		bad("name 이 비어 있다")
	}
	if len(f.Signers) == 0 {
		bad("서명란(signers)이 없다")
	}
	for _, c := range f.Covers {
		if !coverRe.MatchString(c) {
			bad("covers %q: P1~P17 또는 H1~H8 이어야 한다", c)
		}
	}
	seen := map[string]bool{}
	id := func(kind, v string) {
		if !idRe.MatchString(v) {
			bad("%s id %q 형식이 틀렸다", kind, v)
		} else if seen[v] {
			bad("%s id %q 가 겹친다", kind, v)
		}
		seen[v] = true
	}
	switch f.Kind {
	case KindChecklist:
		if len(f.Sections) == 0 {
			bad("점검표에 구역(sections)이 없다")
		}
		for _, s := range f.Sections {
			id("구역", s.ID)
			if s.Title == "" || len(s.Questions) == 0 {
				bad("구역 %q: 제목과 문항이 있어야 한다", s.ID)
			}
			for _, q := range s.Questions {
				id("문항", q.ID)
				if q.Text == "" {
					bad("문항 %q 의 문장이 비어 있다", q.ID)
				}
			}
		}
	case KindLog:
		if len(f.Columns) == 0 {
			bad("일지에 칸(columns)이 없다")
		}
		for _, c := range f.Columns {
			id("칸", c.ID)
			if c.Name == "" {
				bad("칸 %q 의 이름이 비어 있다", c.ID)
			}
		}
	case KindSheet:
		if len(f.Fields) == 0 {
			bad("서식에 항목(fields)이 없다")
		}
		for _, fd := range f.Fields {
			id("항목", fd.ID)
			if fd.Label == "" {
				bad("항목 %q 의 이름이 비어 있다", fd.ID)
			}
		}
	default:
		bad("kind %q: checklist | log | sheet 중 하나여야 한다", f.Kind)
	}
	return out
}
