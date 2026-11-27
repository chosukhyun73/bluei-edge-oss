package feed_cycle

// State represents the current phase of a feed cycle.
type State string

const (
	StateIdle           State = "idle"
	StatePulseActive    State = "pulse_active"
	StatePulseComplete  State = "pulse_complete"
	StateGapObservation State = "gap_observation"
	StateCycleComplete  State = "cycle_complete"
)

// TerminationReason explains why a cycle ended.
type TerminationReason string

const (
	ReasonSatiationOrTarget TerminationReason = "satiation_or_target"
	ReasonMaxPulses         TerminationReason = "max_pulses"
	ReasonMaxDuration       TerminationReason = "max_duration"
	ReasonSafetyBlock       TerminationReason = "safety_block"
	ReasonOperatorStop      TerminationReason = "operator_stop"
	// ReasonVisionBlackout — 판정 카메라가 프레임을 못 주는 동안의 급이 차단
	// (2026-08-25 운영자 지시: "카메라가 끊어진다는 것은 눈이 사라지는 것").
	// ⚠ safety_block 과 반드시 구분한다 — safety_block 은 연속 2회면 수조 급이를
	// 일시정지시키는데(safety_escalation.go), 그건 원격 급이기 API 장애용 대응이다.
	// 카메라 단절로 그 정지가 걸리면 카메라 복구 후에도 운영자가 손으로 풀어야 한다.
	ReasonVisionBlackout TerminationReason = "vision_blackout"
	ReasonSiloThreshold  TerminationReason = "silo_threshold" // 통 잔량이 stop_at_remaining_g 이하
)

// Mode identifies what drove a cycle.
type Mode string

const (
	ModeAdaptive Mode = "adaptive"
	ModeFixed    Mode = "fixed"
)

// SafetyGate is checked before each new pulse. It returns a non-empty block reason
// if the pulse should not proceed. The default no-op implementation always allows.
//
// Phase 4 C-3p: wire in predictive water-quality block logic here.
type SafetyGate interface {
	Check(tankID string) (blocked bool, reason string)
}

// noopSafetyGate always allows — used when no SafetyGate is provided.
type noopSafetyGate struct{}

func (noopSafetyGate) Check(_ string) (bool, string) { return false, "" }
