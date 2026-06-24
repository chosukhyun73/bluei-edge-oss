package arbiter

import "time"

// Source는 사이클 요청의 출처를 나타낸다.
type Source string

const (
	SourceOperatorManual   Source = "operator_manual"
	SourceOperatorSchedule Source = "operator_schedule"
	SourceAIAdvisory       Source = "ai_advisory"
	SourceAIAutonomous     Source = "ai_autonomous"
)

// Priority는 요청 우선순위 수준이다.
type Priority int

const (
	// 값이 클수록 우선순위가 높다.
	PriorityAIAutonomous   Priority = 10
	PriorityAIAdvisory     Priority = 20
	PriorityManualOverride Priority = 30
)

// sourcePriority는 Source → Priority 매핑이다.
func sourcePriority(s Source) Priority {
	switch s {
	case SourceOperatorManual, SourceOperatorSchedule:
		return PriorityManualOverride
	case SourceAIAdvisory:
		return PriorityAIAdvisory
	case SourceAIAutonomous:
		return PriorityAIAutonomous
	default:
		return PriorityAIAutonomous
	}
}

// CycleRequest는 피드 사이클 시작 요청이다.
type CycleRequest struct {
	TankID       string
	ControllerID string
	Source       Source
	Priority     Priority // sourcePriority(Source) 로 자동 설정
	Mode         string   // "adaptive" | "fixed"
	Params       map[string]any
	SubmittedAt  time.Time
	IntentID     string // 운영자 의도 메모 연결 (선택)
}

// Decision은 arbiter 결정 결과다.
type Decision struct {
	Accepted         bool
	RejectionReason  string // accepted=false 일 때만 설정
	ExistingCycleID  string // 충돌 사이클 ID (accepted=false 일 때)
	ResultingCycleID string // 생성된 사이클 ID (accepted=true 일 때)
	DecisionID       string // audit 레코드 ID
	PreemptedCycleID string // 선점된 사이클 ID (accepted=true + 선점 발생 시)
}
