// Package haccp holds the processing-plant HACCP plan model: the process flow
// (steps + links), hazards, CCPs and their critical limits, plus the rules a
// plan version must satisfy before it can be published.
//
// The package is pure logic — no storage, no HTTP. A plan version's content is
// one PlanContent value, stored as JSON and hashed at publish time.
// References: docs/54-haccp-process-flow-ccp-design.md.
package haccp

// PlanContent is everything a plan version defines.
type PlanContent struct {
	Product Product      `json:"product" yaml:"product"`
	Team    []TeamMember `json:"team" yaml:"team"`
	Steps   []Step       `json:"steps" yaml:"steps"`
	Links   []Link       `json:"links" yaml:"links"`
	Hazards []Hazard     `json:"hazards" yaml:"hazards"`
	CCPs    []CCP        `json:"ccps" yaml:"ccps"`
}

// Product is the 제품설명서 + 용도 (식약처 고시 제6조제3항 나·다목).
type Product struct {
	Name        string `json:"name" yaml:"name"`
	FoodType    string `json:"food_type" yaml:"food_type"`       // 식품유형
	Form        string `json:"form" yaml:"form"`                 // 성상
	Composition string `json:"composition" yaml:"composition"`   // 배합비율
	BatchUnit   string `json:"batch_unit" yaml:"batch_unit"`     // 제조단위
	Spec        string `json:"spec" yaml:"spec"`                 // 완제품 규격
	Storage     string `json:"storage" yaml:"storage"`           // 보관·유통 주의사항
	ShelfLife   string `json:"shelf_life" yaml:"shelf_life"`     // 소비기한
	Packaging   string `json:"packaging" yaml:"packaging"`       // 포장방법·재질
	IntendedUse string `json:"intended_use" yaml:"intended_use"` // 가열·섭취 방법
	Consumers   string `json:"consumers" yaml:"consumers"`       // 소비 대상
}

// TeamMember is one HACCP team entry.
type TeamMember struct {
	Name     string `json:"name" yaml:"name"`
	Position string `json:"position" yaml:"position"`
	Role     string `json:"role" yaml:"role"`
}

// Step is one node of the process flow.
type Step struct {
	StepID      string `json:"step_id" yaml:"step_id"`
	Name        string `json:"name" yaml:"name"`
	Method      string `json:"method" yaml:"method"` // 공정별 가공방법·조건
	ZoneID      string `json:"zone_id" yaml:"zone_id"`
	EquipmentID string `json:"equipment_id" yaml:"equipment_id"`
}

// Flow kinds a link can carry.
const (
	FlowProduct    = "product"
	FlowIngredient = "ingredient"
	FlowPackaging  = "packaging"
	FlowWater      = "water"
	FlowWaste      = "waste"
	FlowRework     = "rework"
)

// Link is one directed edge between two steps. A link can carry its own
// conditions (waiting time, ambient temperature) and can be a CCP scope.
type Link struct {
	LinkID      string   `json:"link_id" yaml:"link_id"`
	From        string   `json:"from" yaml:"from"`
	To          string   `json:"to" yaml:"to"`
	FlowKind    string   `json:"flow_kind" yaml:"flow_kind"`
	MaxWaitMin  *float64 `json:"max_wait_min,omitempty" yaml:"max_wait_min,omitempty"`
	EnvTempMaxC *float64 `json:"env_temp_max_c,omitempty" yaml:"env_temp_max_c,omitempty"`
	Handover    string   `json:"handover" yaml:"handover"` // 인수인계 주체
}

// Hazard categories.
const (
	HazardBiological = "B"
	HazardChemical   = "C"
	HazardPhysical   = "P"
)

// Hazard is one identified hazard, attached to exactly one step or one link.
type Hazard struct {
	HazardID       string `json:"hazard_id" yaml:"hazard_id"`
	StepID         string `json:"step_id,omitempty" yaml:"step_id,omitempty"`
	LinkID         string `json:"link_id,omitempty" yaml:"link_id,omitempty"`
	Category       string `json:"category" yaml:"category"`
	Name           string `json:"name" yaml:"name"`
	Cause          string `json:"cause" yaml:"cause"`
	Severity       int    `json:"severity" yaml:"severity"`
	Likelihood     int    `json:"likelihood" yaml:"likelihood"`
	Significant    bool   `json:"significant" yaml:"significant"`
	ControlMeasure string `json:"control_measure" yaml:"control_measure"`
	SourceRef      string `json:"source_ref" yaml:"source_ref"`
	// Confirmed marks that a person reviewed this entry. Template entries
	// start unconfirmed; a version cannot be published until all are confirmed.
	Confirmed bool `json:"confirmed" yaml:"confirmed"`
}

// CCP scope kinds.
const (
	ScopeStep = "step" // one step
	ScopeLink = "link" // one link (transfer / waiting / handover)
	ScopeSpan = "span" // several steps treated as one control (cumulative exposure)
)

// CCP is one critical control point.
type CCP struct {
	CCPID            string   `json:"ccp_id" yaml:"ccp_id"`
	Code             string   `json:"code" yaml:"code"` // 예: CCP-1B
	Name             string   `json:"name" yaml:"name"`
	ScopeKind        string   `json:"scope_kind" yaml:"scope_kind"`
	StepIDs          []string `json:"step_ids,omitempty" yaml:"step_ids,omitempty"`
	LinkIDs          []string `json:"link_ids,omitempty" yaml:"link_ids,omitempty"`
	HazardIDs        []string `json:"hazard_ids" yaml:"hazard_ids"`
	MonitorPrimary   string   `json:"monitor_primary" yaml:"monitor_primary"`
	MonitorBackup    string   `json:"monitor_backup" yaml:"monitor_backup"`
	CorrectiveAction string   `json:"corrective_action" yaml:"corrective_action"`
	Verification     string   `json:"verification" yaml:"verification"`
	// OutageAfterSec: 이 시간 동안 값이 들어오지 않으면 모니터링 불능으로 본다(비상계획). 0 = 미설정.
	OutageAfterSec int `json:"outage_after_sec" yaml:"outage_after_sec"`
	// ExportStepName: 한국 인증 서류에서 연결·구간 CCP 를 표기할 단계 이름.
	ExportStepName string  `json:"export_step_name" yaml:"export_step_name"`
	Limits         []Limit `json:"limits" yaml:"limits"`
}

// Rule kinds a critical limit can use.
const (
	RuleInstant            = "instant"             // 순간값이 범위 안
	RuleDuration           = "duration"            // 조건을 만족한 연속 시간
	RuleCumulativeExposure = "cumulative_exposure" // 기준 온도 초과 누적 시간
	RuleCount              = "count"               // 검출·통과 수량
	RulePeriodicCheck      = "periodic_check"      // 주기 점검
)

// Value sources.
const (
	SourceSensor   = "sensor"
	SourceOperator = "operator"
	SourceDerived  = "derived"
)

// Automation levels (인증원 스마트해썹 CCP별 적용 수준).
const (
	AutoMeasure = "auto_measure" // 자동측정
	AutoObserve = "auto_observe" // 자동관찰
	AutoRecord  = "auto_record"  // 자동기록
	AutoManual  = "manual"       // 수기
)

// Limit is one critical-limit item of a CCP. A CCP has one or more.
type Limit struct {
	LimitID  string   `json:"limit_id" yaml:"limit_id"`
	Item     string   `json:"item" yaml:"item"` // 예: temperature, time, core_temperature
	RuleKind string   `json:"rule_kind" yaml:"rule_kind"`
	Min      *float64 `json:"min,omitempty" yaml:"min,omitempty"`
	Max      *float64 `json:"max,omitempty" yaml:"max,omitempty"`
	Unit     string   `json:"unit" yaml:"unit"`
	// Params — rule_kind 별 추가 값.
	//   duration:            min_duration_sec
	//   cumulative_exposure: threshold, max_minutes (선택: high_threshold, max_minutes_high)
	//   periodic_check:      interval_min
	Params            map[string]float64 `json:"params,omitempty" yaml:"params,omitempty"`
	SourceKind        string             `json:"source_kind" yaml:"source_kind"`
	SensorID          string             `json:"sensor_id" yaml:"sensor_id"`
	SampleIntervalSec int                `json:"sample_interval_sec" yaml:"sample_interval_sec"`
	AutomationLevel   string             `json:"automation_level" yaml:"automation_level"`
	BasisRef          string             `json:"basis_ref" yaml:"basis_ref"` // 한계기준 근거
	Confirmed         bool               `json:"confirmed" yaml:"confirmed"`
}
