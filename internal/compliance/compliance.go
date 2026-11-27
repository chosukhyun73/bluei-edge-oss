// Package compliance holds the per-scheme requirement tables (대응표) and
// evaluates them against what a plant has registered. The plant keeps one set
// of records; each scheme is only a different reading of it.
// References: docs/54-haccp-process-flow-ccp-design.md §18.4, §23.
package compliance

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml
var profileFS embed.FS

// Profile is one scheme's requirement table.
type Profile struct {
	ID                string `json:"id" yaml:"id"`
	Name              string `json:"name" yaml:"name"`
	Version           string `json:"version" yaml:"version"`
	CheckedAt         string `json:"checked_at" yaml:"checked_at"`
	MinRetentionYears int    `json:"min_retention_years" yaml:"min_retention_years"`
	RetentionNote     string `json:"retention_note,omitempty" yaml:"retention_note"`
	Items             []Item `json:"items" yaml:"items"`
}

// Item is one requirement. Check names how it is judged (see Evaluate).
type Item struct {
	ID       string `json:"id" yaml:"id"`
	Title    string `json:"title" yaml:"title"`
	Clause   string `json:"clause,omitempty" yaml:"clause"`
	Required bool   `json:"required" yaml:"required"`
	Check    string `json:"check" yaml:"check"`
	Note     string `json:"note,omitempty" yaml:"note"`
}

// Item statuses.
const (
	StatusMet    = "met"    // 충족
	StatusUnmet  = "unmet"  // 미충족
	StatusLater  = "later"  // 실행 기록이 있어야 판정된다(2단계 이후)
	StatusManual = "manual" // 서류로 관리한다. 시스템이 판정하지 않는다
)

// Checks an item can name. "cert_own:<group>" is also valid, for a group in certGroups.
const (
	CheckLicense       = "license"
	CheckHumanWelfare  = "human_welfare_policy"
	CheckHACCPCert     = "haccp_cert"
	CheckPlanPublished = "plan_published"
	CheckCCPAuto       = "ccp_auto"
	CheckReviewer      = "reviewer_account"
	CheckRetention     = "retention"
	CheckLater         = "later"
	CheckManual        = "manual"

	certOwnPrefix = "cert_own:"
)

// certGroups maps a cert_own group to the certificate schemes that satisfy it.
var certGroups = map[string][]string{
	"msc_coc":     {"msc_coc"},
	"asc_coc":     {"asc_coc"},
	"bap":         {"bap"},
	"haccp_kr":    {"haccp_kr"},
	"food_safety": {"brcgs", "ifs", "sqf", "fssc22000", "iso22000"},
}

// State is what the plant has registered, reduced to what the checks need.
type State struct {
	LicenseComplete     bool
	HumanWelfareSet     bool
	HACCPCertNo         string
	RetentionYears      int
	HasReviewer         bool            // 활성 관리자·검토자 계정
	PublishedPlans      int             // 게시본이 있는 계획 수
	AllCCPsAutoRecorded bool            // 게시본의 모든 CCP 에 수기가 아닌 한계기준 항목이 있다
	ValidOwnCerts       map[string]bool // 자사 보유, 만료되지 않은 인증서의 제도
}

// Result is one item with its status.
type Result struct {
	Item
	Status string `json:"status"`
}

// Evaluate judges every item of p against st.
func Evaluate(p Profile, st State) []Result {
	out := make([]Result, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, Result{Item: it, Status: status(p, it, st)})
	}
	return out
}

func met(ok bool) string {
	if ok {
		return StatusMet
	}
	return StatusUnmet
}

func status(p Profile, it Item, st State) string {
	switch it.Check {
	case CheckLicense:
		return met(st.LicenseComplete)
	case CheckHumanWelfare:
		return met(st.HumanWelfareSet)
	case CheckHACCPCert:
		return met(st.HACCPCertNo != "" || st.ValidOwnCerts["haccp_kr"])
	case CheckPlanPublished:
		return met(st.PublishedPlans > 0)
	case CheckCCPAuto:
		return met(st.PublishedPlans > 0 && st.AllCCPsAutoRecorded)
	case CheckReviewer:
		return met(st.HasReviewer)
	case CheckRetention:
		return met(st.RetentionYears >= p.MinRetentionYears)
	case CheckLater:
		return StatusLater
	case CheckManual:
		return StatusManual
	}
	if group, ok := strings.CutPrefix(it.Check, certOwnPrefix); ok {
		for _, scheme := range certGroups[group] {
			if st.ValidOwnCerts[scheme] {
				return StatusMet
			}
		}
	}
	return StatusUnmet
}

func knownCheck(check string) bool {
	switch check {
	case CheckLicense, CheckHumanWelfare, CheckHACCPCert, CheckPlanPublished, CheckCCPAuto,
		CheckReviewer, CheckRetention, CheckLater, CheckManual:
		return true
	}
	group, ok := strings.CutPrefix(check, certOwnPrefix)
	return ok && certGroups[group] != nil
}

// ListProfiles returns all built-in profiles ordered by id. A profile that
// names an unknown check is an error: it would silently read as unmet.
func ListProfiles() ([]Profile, error) {
	entries, err := profileFS.ReadDir("profiles")
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		b, err := profileFS.ReadFile("profiles/" + e.Name())
		if err != nil {
			return nil, err
		}
		var p Profile
		if err := yaml.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("profile %s: %w", e.Name(), err)
		}
		for _, it := range p.Items {
			if !knownCheck(it.Check) {
				return nil, fmt.Errorf("profile %s item %s: unknown check %q", p.ID, it.ID, it.Check)
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
