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
	ReasonSiloThreshold     TerminationReason = "silo_threshold" // 통 잔량이 stop_at_remaining_g 이하
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
