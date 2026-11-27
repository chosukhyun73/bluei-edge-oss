package haccp

import "testing"

func hasIssue(issues []Issue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// completeTemplate fills in what a plant has to supply before publishing:
// product name, monitors, outage rule, sensor binding, missing limit values,
// and the per-item confirmation.
func completeTemplate(t *testing.T, id string) *PlanContent {
	t.Helper()
	tpl, err := LoadTemplate(id)
	if err != nil || tpl == nil {
		t.Fatalf("load template %s: %v", id, err)
	}
	c := tpl.Content
	c.Product.Name = "시험 제품"
	for i := range c.Hazards {
		c.Hazards[i].Confirmed = true
	}
	for i := range c.CCPs {
		k := &c.CCPs[i]
		k.MonitorPrimary = "user_monitor"
		k.OutageAfterSec = 600
		for j := range k.Limits {
			l := &k.Limits[j]
			if l.SourceKind == SourceSensor {
				l.SensorID = "mock_" + l.LimitID
			}
			if l.RuleKind == RuleInstant && l.Min == nil && l.Max == nil {
				v := 30.0
				l.Max = &v
			}
			if l.BasisRef == "" {
				l.BasisRef = "자사 검증 기록 2026-11"
			}
			l.Confirmed = true
		}
	}
	return &c
}

func TestTemplatesLoadAndAreStructurallyValid(t *testing.T) {
	all, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 templates, got %d", len(all))
	}
	for _, tpl := range all {
		if tpl.Source == "" {
			t.Errorf("%s: source 가 비어 있음", tpl.ID)
		}
		if issues := ValidateStructure(&tpl.Content); len(issues) != 0 {
			t.Errorf("%s: 구조 오류 %+v", tpl.ID, issues)
		}
	}
}

// 템플릿을 그대로는 게시할 수 없다 — 사람이 값을 정하고 확인해야 한다.
func TestTemplateAsIsCannotBePublished(t *testing.T) {
	for _, id := range []string{"frozen_fish", "fresh_histamine_fish"} {
		tpl, _ := LoadTemplate(id)
		issues := ValidateForPublish(&tpl.Content)
		for _, code := range []string{"NOT_CONFIRMED", "MISSING_PRODUCT", "MISSING_MONITOR", "MISSING_SENSOR", "MISSING_OUTAGE_RULE"} {
			if !hasIssue(issues, code) {
				t.Errorf("%s: %s 가 나와야 함", id, code)
			}
		}
	}
	// 히스타민 템플릿의 이송 대기 CCP 는 값과 근거를 비워 두었다.
	tpl, _ := LoadTemplate("fresh_histamine_fish")
	issues := ValidateForPublish(&tpl.Content)
	if !hasIssue(issues, "MISSING_BASIS") || !hasIssue(issues, "MISSING_LIMIT_VALUE") {
		t.Errorf("이송 대기 한계기준의 값·근거 누락이 잡혀야 함: %+v", issues)
	}
}

func TestCompletedTemplatePassesPublishRules(t *testing.T) {
	for _, id := range []string{"frozen_fish", "fresh_histamine_fish"} {
		c := completeTemplate(t, id)
		if issues := ValidateForPublish(c); len(issues) != 0 {
			t.Errorf("%s: 게시 가능해야 함, got %+v", id, issues)
		}
	}
}

func TestPublishRules(t *testing.T) {
	t.Run("근거가 빈 한계기준", func(t *testing.T) {
		c := completeTemplate(t, "frozen_fish")
		c.CCPs[0].Limits[0].BasisRef = ""
		if !hasIssue(ValidateForPublish(c), "MISSING_BASIS") {
			t.Fatal("MISSING_BASIS 가 나와야 함")
		}
	})
	t.Run("고립된 단계", func(t *testing.T) {
		c := completeTemplate(t, "frozen_fish")
		c.Steps = append(c.Steps, Step{StepID: "alone", Name: "외딴 단계"})
		if !hasIssue(ValidateForPublish(c), "ISOLATED_STEP") {
			t.Fatal("ISOLATED_STEP 가 나와야 함")
		}
	})
	t.Run("연결·구간 CCP 의 서류 표기 이름", func(t *testing.T) {
		c := completeTemplate(t, "fresh_histamine_fish")
		for i := range c.CCPs {
			c.CCPs[i].ExportStepName = ""
		}
		issues := ValidateForPublish(c)
		n := 0
		for _, i := range issues {
			if i.Code == "MISSING_EXPORT_STEP_NAME" {
				n++
			}
		}
		if n != 2 { // 연결 CCP 1 + 구간 CCP 1
			t.Fatalf("MISSING_EXPORT_STEP_NAME 2건이어야 함, got %d (%+v)", n, issues)
		}
	})
	t.Run("누적 노출 규칙의 필수 값", func(t *testing.T) {
		c := completeTemplate(t, "fresh_histamine_fish")
		for i := range c.CCPs {
			for j := range c.CCPs[i].Limits {
				if c.CCPs[i].Limits[j].RuleKind == RuleCumulativeExposure {
					delete(c.CCPs[i].Limits[j].Params, "max_minutes")
				}
			}
		}
		if !hasIssue(ValidateForPublish(c), "MISSING_RULE_PARAM") {
			t.Fatal("MISSING_RULE_PARAM 가 나와야 함")
		}
	})
}

func TestStructureRules(t *testing.T) {
	c := completeTemplate(t, "frozen_fish")
	c.Links = append(c.Links, Link{LinkID: "bad", From: "wash", To: "nowhere", FlowKind: FlowProduct})
	c.CCPs[0].ScopeKind = ScopeSpan // 단계 하나뿐인데 구간
	c.Hazards[0].LinkID = "l01"     // 단계와 연결 둘 다
	issues := ValidateStructure(c)
	for _, code := range []string{"UNKNOWN_STEP", "CCP_SCOPE", "HAZARD_TARGET"} {
		if !hasIssue(issues, code) {
			t.Errorf("%s 가 나와야 함: %+v", code, issues)
		}
	}
}

func TestContentHashIsStableAndSensitive(t *testing.T) {
	a := completeTemplate(t, "fresh_histamine_fish")
	b := completeTemplate(t, "fresh_histamine_fish")
	ha, _ := ContentHash(a)
	hb, _ := ContentHash(b)
	if ha == "" || ha != hb {
		t.Fatalf("같은 내용은 같은 해시여야 함: %s vs %s", ha, hb)
	}
	v := 5.0
	b.CCPs[1].Limits[0].Max = &v
	hc, _ := ContentHash(b)
	if hc == ha {
		t.Fatal("내용이 바뀌면 해시가 달라야 함")
	}
	if !CCPsChanged(a, b) {
		t.Fatal("한계기준이 바뀌면 CCP 변경으로 봐야 함")
	}
	c := completeTemplate(t, "fresh_histamine_fish")
	c.Product.Name = "다른 제품"
	if CCPsChanged(a, c) {
		t.Fatal("제품명만 바뀐 것은 CCP 변경이 아님")
	}
}
