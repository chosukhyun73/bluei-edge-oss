package haccp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Issue is one validation finding. Path points at the offending entry.
type Issue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func issue(code, path, format string, a ...any) Issue {
	return Issue{Code: code, Path: path, Message: fmt.Sprintf(format, a...)}
}

// ValidateStructure checks that a draft is internally consistent: ids are
// present and unique, references resolve, enums are known. A draft that fails
// this cannot be saved.
func ValidateStructure(c *PlanContent) []Issue {
	var out []Issue

	steps := map[string]bool{}
	for i, s := range c.Steps {
		p := fmt.Sprintf("steps[%d]", i)
		if s.StepID == "" {
			out = append(out, issue("MISSING_ID", p, "step_id 가 비어 있습니다"))
			continue
		}
		if steps[s.StepID] {
			out = append(out, issue("DUPLICATE_ID", p, "step_id 중복: %s", s.StepID))
		}
		steps[s.StepID] = true
	}

	links := map[string]bool{}
	for i, l := range c.Links {
		p := fmt.Sprintf("links[%d]", i)
		if l.LinkID == "" {
			out = append(out, issue("MISSING_ID", p, "link_id 가 비어 있습니다"))
			continue
		}
		if links[l.LinkID] {
			out = append(out, issue("DUPLICATE_ID", p, "link_id 중복: %s", l.LinkID))
		}
		links[l.LinkID] = true
		if !steps[l.From] || !steps[l.To] {
			out = append(out, issue("UNKNOWN_STEP", p, "연결의 앞·뒤 단계가 없습니다: %s → %s", l.From, l.To))
		}
		if l.From == l.To {
			out = append(out, issue("SELF_LINK", p, "같은 단계로 이어지는 연결입니다: %s", l.From))
		}
		if !validFlowKind(l.FlowKind) {
			out = append(out, issue("INVALID_FLOW_KIND", p, "flow_kind 값이 올바르지 않습니다: %q", l.FlowKind))
		}
	}

	hazards := map[string]bool{}
	for i, h := range c.Hazards {
		p := fmt.Sprintf("hazards[%d]", i)
		if h.HazardID == "" {
			out = append(out, issue("MISSING_ID", p, "hazard_id 가 비어 있습니다"))
			continue
		}
		if hazards[h.HazardID] {
			out = append(out, issue("DUPLICATE_ID", p, "hazard_id 중복: %s", h.HazardID))
		}
		hazards[h.HazardID] = true
		switch {
		case h.StepID != "" && h.LinkID != "":
			out = append(out, issue("HAZARD_TARGET", p, "위해요소는 단계와 연결 중 하나에만 붙습니다"))
		case h.StepID != "" && !steps[h.StepID]:
			out = append(out, issue("UNKNOWN_STEP", p, "위해요소의 단계가 없습니다: %s", h.StepID))
		case h.LinkID != "" && !links[h.LinkID]:
			out = append(out, issue("UNKNOWN_LINK", p, "위해요소의 연결이 없습니다: %s", h.LinkID))
		case h.StepID == "" && h.LinkID == "":
			out = append(out, issue("HAZARD_TARGET", p, "위해요소가 붙을 단계 또는 연결이 없습니다"))
		}
		if h.Category != HazardBiological && h.Category != HazardChemical && h.Category != HazardPhysical {
			out = append(out, issue("INVALID_CATEGORY", p, "category 는 B·C·P 중 하나입니다: %q", h.Category))
		}
	}

	ccpIDs := map[string]bool{}
	limitIDs := map[string]bool{}
	for i, k := range c.CCPs {
		p := fmt.Sprintf("ccps[%d]", i)
		if k.CCPID == "" {
			out = append(out, issue("MISSING_ID", p, "ccp_id 가 비어 있습니다"))
			continue
		}
		if ccpIDs[k.CCPID] {
			out = append(out, issue("DUPLICATE_ID", p, "ccp_id 중복: %s", k.CCPID))
		}
		ccpIDs[k.CCPID] = true
		for _, id := range k.StepIDs {
			if !steps[id] {
				out = append(out, issue("UNKNOWN_STEP", p, "CCP 범위의 단계가 없습니다: %s", id))
			}
		}
		for _, id := range k.LinkIDs {
			if !links[id] {
				out = append(out, issue("UNKNOWN_LINK", p, "CCP 범위의 연결이 없습니다: %s", id))
			}
		}
		switch k.ScopeKind {
		case ScopeStep:
			if len(k.StepIDs) != 1 || len(k.LinkIDs) != 0 {
				out = append(out, issue("CCP_SCOPE", p, "단계 CCP 는 단계 하나에만 걸립니다"))
			}
		case ScopeLink:
			if len(k.LinkIDs) != 1 || len(k.StepIDs) != 0 {
				out = append(out, issue("CCP_SCOPE", p, "연결 CCP 는 연결 하나에만 걸립니다"))
			}
		case ScopeSpan:
			if len(k.StepIDs) < 2 {
				out = append(out, issue("CCP_SCOPE", p, "구간 CCP 는 단계 둘 이상을 묶습니다"))
			}
		default:
			out = append(out, issue("CCP_SCOPE", p, "scope_kind 값이 올바르지 않습니다: %q", k.ScopeKind))
		}
		for _, id := range k.HazardIDs {
			if !hazards[id] {
				out = append(out, issue("UNKNOWN_HAZARD", p, "CCP 가 가리키는 위해요소가 없습니다: %s", id))
			}
		}
		for j, l := range k.Limits {
			lp := fmt.Sprintf("%s.limits[%d]", p, j)
			if l.LimitID == "" {
				out = append(out, issue("MISSING_ID", lp, "limit_id 가 비어 있습니다"))
				continue
			}
			if limitIDs[l.LimitID] {
				out = append(out, issue("DUPLICATE_ID", lp, "limit_id 중복: %s", l.LimitID))
			}
			limitIDs[l.LimitID] = true
			if !validRuleKind(l.RuleKind) {
				out = append(out, issue("INVALID_RULE_KIND", lp, "rule_kind 값이 올바르지 않습니다: %q", l.RuleKind))
			}
			if l.SourceKind != "" && !validSourceKind(l.SourceKind) {
				out = append(out, issue("INVALID_SOURCE_KIND", lp, "source_kind 값이 올바르지 않습니다: %q", l.SourceKind))
			}
			if l.AutomationLevel != "" && !validAutomationLevel(l.AutomationLevel) {
				out = append(out, issue("INVALID_AUTOMATION_LEVEL", lp, "automation_level 값이 올바르지 않습니다: %q", l.AutomationLevel))
			}
		}
	}
	return out
}

// ValidateForPublish returns everything that blocks publishing: structural
// problems plus completeness rules. An empty result means the version may be
// published.
func ValidateForPublish(c *PlanContent) []Issue {
	out := ValidateStructure(c)

	if c.Product.Name == "" {
		out = append(out, issue("MISSING_PRODUCT", "product.name", "제품명이 없습니다"))
	}
	if len(c.Steps) == 0 {
		out = append(out, issue("NO_STEPS", "steps", "공정 단계가 없습니다"))
	}
	if len(c.CCPs) == 0 {
		out = append(out, issue("NO_CCPS", "ccps", "CCP 가 없습니다"))
	}

	// 고립된 단계: 단계가 둘 이상이면 모든 단계가 연결을 하나 이상 가져야 한다.
	if len(c.Steps) > 1 {
		linked := map[string]bool{}
		for _, l := range c.Links {
			linked[l.From] = true
			linked[l.To] = true
		}
		for i, s := range c.Steps {
			if s.StepID != "" && !linked[s.StepID] {
				out = append(out, issue("ISOLATED_STEP", fmt.Sprintf("steps[%d]", i), "다른 단계와 이어지지 않은 단계입니다: %s", s.Name))
			}
		}
	}
	for i, s := range c.Steps {
		if s.Name == "" {
			out = append(out, issue("MISSING_NAME", fmt.Sprintf("steps[%d]", i), "단계 이름이 없습니다"))
		}
	}

	for i, h := range c.Hazards {
		p := fmt.Sprintf("hazards[%d]", i)
		if h.Name == "" {
			out = append(out, issue("MISSING_NAME", p, "위해요소 이름이 없습니다"))
		}
		if h.SourceRef == "" {
			out = append(out, issue("MISSING_BASIS", p, "위해요소의 근거가 없습니다"))
		}
		if !h.Confirmed {
			out = append(out, issue("NOT_CONFIRMED", p, "사람이 확인하지 않은 위해요소입니다: %s", h.Name))
		}
	}

	codes := map[string]bool{}
	for i, k := range c.CCPs {
		p := fmt.Sprintf("ccps[%d]", i)
		if k.Code == "" {
			out = append(out, issue("MISSING_CODE", p, "CCP 번호가 없습니다"))
		} else if codes[k.Code] {
			out = append(out, issue("DUPLICATE_CODE", p, "CCP 번호 중복: %s", k.Code))
		}
		codes[k.Code] = true
		if len(k.HazardIDs) == 0 {
			out = append(out, issue("CCP_NO_HAZARD", p, "CCP 가 관리하는 위해요소가 없습니다"))
		}
		if k.MonitorPrimary == "" {
			out = append(out, issue("MISSING_MONITOR", p, "모니터링 담당자가 없습니다"))
		}
		if k.CorrectiveAction == "" {
			out = append(out, issue("MISSING_CORRECTIVE_ACTION", p, "개선조치 방법이 없습니다"))
		}
		if k.Verification == "" {
			out = append(out, issue("MISSING_VERIFICATION", p, "검증 방법이 없습니다"))
		}
		if (k.ScopeKind == ScopeLink || k.ScopeKind == ScopeSpan) && k.ExportStepName == "" {
			out = append(out, issue("MISSING_EXPORT_STEP_NAME", p, "연결·구간 CCP 는 서류에 표기할 단계 이름이 필요합니다"))
		}
		if len(k.Limits) == 0 {
			out = append(out, issue("CCP_NO_LIMIT", p, "한계기준 항목이 없습니다"))
		}
		usesSensor := false
		for j, l := range k.Limits {
			lp := fmt.Sprintf("%s.limits[%d]", p, j)
			if l.Item == "" {
				out = append(out, issue("MISSING_ITEM", lp, "한계기준 항목 이름이 없습니다"))
			}
			if l.BasisRef == "" {
				out = append(out, issue("MISSING_BASIS", lp, "한계기준의 근거가 없습니다"))
			}
			if l.SourceKind == "" {
				out = append(out, issue("MISSING_SOURCE", lp, "값의 출처가 없습니다"))
			}
			if l.SourceKind == SourceSensor {
				usesSensor = true
				if l.SensorID == "" {
					out = append(out, issue("MISSING_SENSOR", lp, "연결된 센서가 없습니다"))
				}
			}
			if l.AutomationLevel == "" {
				out = append(out, issue("MISSING_AUTOMATION_LEVEL", lp, "자동화 수준이 없습니다"))
			}
			if !l.Confirmed {
				out = append(out, issue("NOT_CONFIRMED", lp, "사람이 확인하지 않은 한계기준입니다: %s", l.Item))
			}
			out = append(out, limitRuleIssues(lp, l)...)
		}
		if usesSensor && k.OutageAfterSec <= 0 {
			out = append(out, issue("MISSING_OUTAGE_RULE", p, "센서 불능 판정 조건(비상계획)이 없습니다"))
		}
	}
	return out
}

// limitRuleIssues checks that a limit carries the numbers its rule kind needs.
func limitRuleIssues(path string, l Limit) []Issue {
	var out []Issue
	need := func(key string) {
		if v, ok := l.Params[key]; !ok || v <= 0 {
			out = append(out, issue("MISSING_RULE_PARAM", path, "%s 규칙에 %s 값이 필요합니다", l.RuleKind, key))
		}
	}
	switch l.RuleKind {
	case RuleInstant:
		if l.Min == nil && l.Max == nil {
			out = append(out, issue("MISSING_LIMIT_VALUE", path, "하한 또는 상한이 필요합니다"))
		}
	case RuleDuration:
		if l.Min == nil && l.Max == nil {
			out = append(out, issue("MISSING_LIMIT_VALUE", path, "하한 또는 상한이 필요합니다"))
		}
		need("min_duration_sec")
	case RuleCumulativeExposure:
		need("threshold")
		need("max_minutes")
	case RuleCount:
		if l.Max == nil {
			out = append(out, issue("MISSING_LIMIT_VALUE", path, "허용 수량 상한이 필요합니다"))
		}
	case RulePeriodicCheck:
		need("interval_min")
	}
	if l.Min != nil && l.Max != nil && *l.Min > *l.Max {
		out = append(out, issue("LIMIT_RANGE", path, "하한이 상한보다 큽니다"))
	}
	return out
}

func validFlowKind(v string) bool {
	switch v {
	case FlowProduct, FlowIngredient, FlowPackaging, FlowWater, FlowWaste, FlowRework:
		return true
	}
	return false
}

func validRuleKind(v string) bool {
	switch v {
	case RuleInstant, RuleDuration, RuleCumulativeExposure, RuleCount, RulePeriodicCheck:
		return true
	}
	return false
}

func validSourceKind(v string) bool {
	switch v {
	case SourceSensor, SourceOperator, SourceDerived:
		return true
	}
	return false
}

func validAutomationLevel(v string) bool {
	switch v {
	case AutoMeasure, AutoObserve, AutoRecord, AutoManual:
		return true
	}
	return false
}

// ContentHash returns the sha256 of the canonical JSON form of c. The same
// content always yields the same hash: struct fields marshal in declaration
// order and map keys are sorted by encoding/json.
func ContentHash(c *PlanContent) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// CCPsChanged reports whether the CCP definitions differ between two versions
// (added, removed, or any field changed). A change here means the plant has to
// file a certification change request (식약처 고시 제12조, 제11조의2제7항).
func CCPsChanged(prev, next *PlanContent) bool {
	a, _ := json.Marshal(prev.CCPs)
	b, _ := json.Marshal(next.CCPs)
	return string(a) != string(b)
}

// SiteRefs is what a plant has registered: the ids a plan may point at.
type SiteRefs struct {
	Zones     map[string]bool
	Equipment map[string]bool
	Sensors   map[string]bool
}

// ValidateRefs checks that the zones, equipment and sensors a plan names are
// actually registered at the plant. Empty references are allowed here — whether
// one is required is decided by ValidateForPublish.
func ValidateRefs(c *PlanContent, refs SiteRefs) []Issue {
	var out []Issue
	for i, s := range c.Steps {
		p := fmt.Sprintf("steps[%d]", i)
		if s.ZoneID != "" && !refs.Zones[s.ZoneID] {
			out = append(out, issue("UNKNOWN_ZONE", p, "등록되지 않은 구역입니다: %s", s.ZoneID))
		}
		if s.EquipmentID != "" && !refs.Equipment[s.EquipmentID] {
			out = append(out, issue("UNKNOWN_EQUIPMENT", p, "등록되지 않은 설비입니다: %s", s.EquipmentID))
		}
	}
	for i, k := range c.CCPs {
		for j, l := range k.Limits {
			if l.SensorID != "" && !refs.Sensors[l.SensorID] {
				out = append(out, issue("UNKNOWN_SENSOR", fmt.Sprintf("ccps[%d].limits[%d]", i, j),
					"등록되지 않은 센서입니다: %s", l.SensorID))
			}
		}
	}
	return out
}
