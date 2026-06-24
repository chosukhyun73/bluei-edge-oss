package learned_safety

import "time"

// LearnedRule is an in-memory representation of a mined safety rule.
// 저장소 행(storage.LearnedRule)과 1:1 대응하지만, 게이트 로직은 이 구조체를 사용.
type LearnedRule struct {
	RuleID        string
	ConditionJSON string
	Severity      string // "low" | "medium" | "high"
	Source        string // "operator_dispute" | "incident_log"
	Confidence    float64
	CreatedAt     time.Time
}

// Condition is the parsed form of ConditionJSON.
// { "metric": "water_temperature", "operator": "gt", "threshold": 28.0, "window_h": 24 }
type Condition struct {
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"` // "gt" | "lt" | "gte" | "lte"
	Threshold float64 `json:"threshold"`
	WindowH   int     `json:"window_h"`
}
