package llm

import "fmt"

// ValidateAndEnforce — LLM 응답의 adjustment 를 백엔드가 다시 검증.
// LLM 단독 결정 금지. 위반 발견 시 a.CanApply=false 로 강제 override
// 하고 BlockedBy 에 위반 사유 append.
//
// 반환: validate 후의 violations 목록 (caller 가 로그/응답에 활용).
func ValidateAndEnforce(a *Analysis) []string {
	if a == nil {
		return nil
	}
	var violations []string

	if v, ok := numericField(a.Adjustment, "min_interval_min"); ok && v < 30 {
		violations = append(violations, fmt.Sprintf("min_interval_min %.0f < 30 (안전 임계)", v))
	}
	if v, ok := numericField(a.Adjustment, "max_daily_cycles_override"); ok {
		if v < 1 || v > 24 {
			violations = append(violations, fmt.Sprintf("max_daily_cycles_override %.0f out of [1,24]", v))
		}
	}
	if v, ok := numericField(a.Adjustment, "get_factor"); ok {
		if v < 0.3 || v > 1.5 {
			violations = append(violations, fmt.Sprintf("get_factor %.2f out of [0.3,1.5]", v))
		}
	}
	if v, ok := stringField(a.Adjustment, "bsf_mode_override"); ok {
		switch v {
		case "aggressive", "standard", "conservative":
			// ok
		default:
			violations = append(violations, fmt.Sprintf("bsf_mode_override %q not in {aggressive,standard,conservative}", v))
		}
	}

	if len(violations) > 0 {
		a.CanApply = false
		a.BlockedBy = append(a.BlockedBy, violations...)
	}
	return violations
}

// numericField — adjustment[key] 가 JSON number (float64) 또는 int 인 경우 추출.
func numericField(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func stringField(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	if s, ok := v.(string); ok && s != "" {
		return s, true
	}
	return "", false
}
