package feed_cycle

import (
	"sync"
	"time"
)

// AdaptiveParams holds parameters for an adaptive feed cycle.
// 목표량에 도달하거나 안전 조건에 의해 조기 종료.
type AdaptiveParams struct {
	TargetAmountG   float64 // required, > 0
	MaxPulses       int     // default 10
	MaxDurationMin  int     // default 30
	GapMs           int     // 0 → use Worker.defaultGapMs
	PulseDurationMs int     // 0 → use Worker.defaultPulseDurationMs
	// Phase A — ESP32 모터 출력 (0 = ESP32 펌웨어 default)
	SpeedRpm int // 살포 모터 (DAC1) 14~42 rpm
	Amount   int // 공급 모터 (DAC2) 0~255
	// Phase 5 — 통 잔량 기반 종료. 마지막 weight_after_g 가 이 값 이하면 cycle 종료.
	// 0 = 비활성. weight 측정이 한 번이라도 도착해야 평가.
	StopAtRemainingG float64
}

// FixedParams holds parameters for a fixed (deterministic) feed cycle.
type FixedParams struct {
	PulseDurationMs int // required
	GapMs           int // required
	TotalPulses     int // required
	// Phase A — ESP32 모터 출력 (0 = ESP32 펌웨어 default)
	SpeedRpm int // 살포 모터 (DAC1) 14~42 rpm
	Amount   int // 공급 모터 (DAC2) 0~255
}

// Cycle is the in-memory state of one active feed cycle.
type Cycle struct {
	CycleID      string
	TankID       string
	ControllerID string
	Mode         Mode
	StartedAt    time.Time
	// IntentID 는 operator_intents.intent_id 와의 영구 링크. 빈 문자열이면 미연결.
	IntentID string

	// adaptive params (non-nil when Mode == ModeAdaptive)
	adaptive *AdaptiveParams
	// fixed params (non-nil when Mode == ModeFixed)
	fixed *FixedParams

	state             State
	pulsesExecuted    int
	totalAmountG      float64
	terminationReason TerminationReason

	// Phase 5 (load cell). 0 이면 weight event 미수신 — 기존 totalAmountG (stub) 사용.
	actualTotalAmountG  float64
	recentPulseDeltas   []float64 // 최근 3 pulse delta (silo depletion 감지용)
	siloDepletionWarned bool
	lastWeightAfterG    float64 // 마지막 펄스 직후 통 무게 (StopAtRemainingG 평가용)
	weightSeen          bool    // RecordWeight 가 한 번이라도 호출됐는지

	// stopCh is closed by operator stop.
	stopCh chan struct{}
	// siloThresholdCh is closed when UDP weight stream observes
	// remaining ≤ StopAtRemainingG. Triggers immediate cycle termination
	// with ReasonSiloThreshold (does NOT mark operator_stop).
	siloThresholdCh chan struct{}
	mu              sync.Mutex
}

// NewAdaptiveCycle creates a cycle in Idle state using adaptive params.
// Defaults are applied if MaxPulses or MaxDurationMin are zero.
func NewAdaptiveCycle(cycleID, tankID, controllerID string, p AdaptiveParams) *Cycle {
	if p.MaxPulses <= 0 {
		p.MaxPulses = 10
	}
	if p.MaxDurationMin <= 0 {
		p.MaxDurationMin = 30
	}
	return &Cycle{
		CycleID:         cycleID,
		TankID:          tankID,
		ControllerID:    controllerID,
		Mode:            ModeAdaptive,
		StartedAt:       time.Now().UTC(),
		adaptive:        &p,
		state:           StateIdle,
		stopCh:          make(chan struct{}),
		siloThresholdCh: make(chan struct{}),
	}
}

// NewFixedCycle creates a cycle in Idle state using fixed params.
func NewFixedCycle(cycleID, tankID, controllerID string, p FixedParams) *Cycle {
	return &Cycle{
		CycleID:         cycleID,
		TankID:          tankID,
		ControllerID:    controllerID,
		Mode:            ModeFixed,
		StartedAt:       time.Now().UTC(),
		fixed:           &p,
		state:           StateIdle,
		stopCh:          make(chan struct{}),
		siloThresholdCh: make(chan struct{}),
	}
}

// State returns the current state (safe for concurrent reads).
func (c *Cycle) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// PulsesExecuted returns the number of pulses completed so far.
func (c *Cycle) PulsesExecuted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pulsesExecuted
}

// TotalAmountG returns the estimated total feed dispensed so far.
func (c *Cycle) TotalAmountG() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalAmountG
}

// TerminationReason returns the reason set when the cycle completed (empty if not yet complete).
func (c *Cycle) TerminationReason() TerminationReason {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminationReason
}

// StopCh returns the channel closed on operator stop.
func (c *Cycle) StopCh() <-chan struct{} { return c.stopCh }

// SiloThresholdCh returns the channel closed when UDP weight stream
// observes remaining ≤ StopAtRemainingG.
func (c *Cycle) SiloThresholdCh() <-chan struct{} { return c.siloThresholdCh }

// TriggerSiloThreshold closes siloThresholdCh idempotently. Called from
// the UDP weight handler when the threshold is crossed mid-pulse.
func (c *Cycle) TriggerSiloThreshold() {
	select {
	case <-c.siloThresholdCh:
		// already closed
	default:
		close(c.siloThresholdCh)
	}
}

// IngestStreamWeight updates lastWeightAfterG from a UDP weight packet.
// Returns true if the silo threshold has just been crossed (caller should
// invoke TriggerSiloThreshold). Does NOT mutate actualTotalAmountG —
// stream weight is observational, not pulse-bounded.
func (c *Cycle) IngestStreamWeight(grams float64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastWeightAfterG = grams
	c.weightSeen = true
	if c.Mode != ModeAdaptive || c.adaptive == nil {
		return false
	}
	if c.adaptive.StopAtRemainingG <= 0 {
		return false
	}
	return grams <= c.adaptive.StopAtRemainingG
}

// OperatorStop marks the cycle for termination by an operator request.
// Safe to call multiple times.
func (c *Cycle) OperatorStop() {
	select {
	case <-c.stopCh:
		// already closed
	default:
		close(c.stopCh)
	}
}

// --- state transition helpers (called by Worker only, under c.mu) --------

func (c *Cycle) transitionTo(s State) {
	c.mu.Lock()
	c.state = s
	c.mu.Unlock()
}

// recordPulseResult records the outcome of a completed pulse.
func (c *Cycle) recordPulseResult(estimatedG float64) {
	c.mu.Lock()
	c.pulsesExecuted++
	c.totalAmountG += estimatedG
	c.state = StatePulseComplete
	c.mu.Unlock()
}

// complete transitions to cycle_complete with the given reason.
func (c *Cycle) complete(reason TerminationReason) {
	c.mu.Lock()
	c.state = StateCycleComplete
	c.terminationReason = reason
	c.mu.Unlock()
}

// maxPulsesForMode returns the effective pulse limit.
// Returns math.MaxInt32 for fixed mode (uses fixed.TotalPulses instead).
func (c *Cycle) maxPulses() int {
	if c.Mode == ModeAdaptive && c.adaptive != nil {
		return c.adaptive.MaxPulses
	}
	if c.Mode == ModeFixed && c.fixed != nil {
		return c.fixed.TotalPulses
	}
	return 10
}

// effectiveGapMs returns the gap to use for this cycle (param > default).
func (c *Cycle) effectiveGapMs(defaultGapMs int) int {
	if c.Mode == ModeAdaptive && c.adaptive != nil && c.adaptive.GapMs > 0 {
		return c.adaptive.GapMs
	}
	if c.Mode == ModeFixed && c.fixed != nil {
		return c.fixed.GapMs
	}
	return defaultGapMs
}

// effectivePulseDurationMs returns the pulse duration for this cycle.
func (c *Cycle) effectivePulseDurationMs(defaultPulseMs int) int {
	if c.Mode == ModeAdaptive && c.adaptive != nil && c.adaptive.PulseDurationMs > 0 {
		return c.adaptive.PulseDurationMs
	}
	if c.Mode == ModeFixed && c.fixed != nil {
		return c.fixed.PulseDurationMs
	}
	return defaultPulseMs
}

// SpeedRpm returns the 살포 모터 rpm for this cycle (0 = ESP32 default).
func (c *Cycle) SpeedRpm() int {
	if c.Mode == ModeAdaptive && c.adaptive != nil {
		return c.adaptive.SpeedRpm
	}
	if c.Mode == ModeFixed && c.fixed != nil {
		return c.fixed.SpeedRpm
	}
	return 0
}

// Amount returns the 공급 모터 amount for this cycle (0 = ESP32 default).
func (c *Cycle) Amount() int {
	if c.Mode == ModeAdaptive && c.adaptive != nil {
		return c.adaptive.Amount
	}
	if c.Mode == ModeFixed && c.fixed != nil {
		return c.fixed.Amount
	}
	return 0
}

// isTargetReached returns true when adaptive target has been hit or exceeded.
// Phase 5: actualTotalAmountG > 0 이면 실측 누적 우선, 미수신이면 stub totalAmountG.
func (c *Cycle) isTargetReached() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Mode != ModeAdaptive || c.adaptive == nil {
		return false
	}
	measured := c.totalAmountG
	if c.actualTotalAmountG > 0 {
		measured = c.actualTotalAmountG
	}
	return measured >= c.adaptive.TargetAmountG
}

// isStopAtRemainingReached returns true when adaptive cycle should stop
// because the silo weight has fallen below StopAtRemainingG.
// 첫 펄스 weight 도착 전에는 false (false positive 방지).
func (c *Cycle) isStopAtRemainingReached() (bool, float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Mode != ModeAdaptive || c.adaptive == nil {
		return false, 0
	}
	if c.adaptive.StopAtRemainingG <= 0 || !c.weightSeen {
		return false, 0
	}
	return c.lastWeightAfterG <= c.adaptive.StopAtRemainingG, c.lastWeightAfterG
}

// ActualTotalAmountG returns the cumulative weight-measured feed dispensed
// (Phase 5). 0 means no weight events received yet — caller should fall back
// to TotalAmountG (stub estimate).
func (c *Cycle) ActualTotalAmountG() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.actualTotalAmountG
}

// SiloDepletionWarned reports whether the silo-empty warning has already
// been emitted for this cycle (idempotent — emit only once per cycle).
func (c *Cycle) SiloDepletionWarned() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.siloDepletionWarned
}

// WeightResult is the outcome of RecordWeight.
type WeightResult struct {
	DeltaG              float64 // before - after, clamped to >= 0
	ActualTotalAmountG  float64 // cumulative measured weight after this pulse
	Last3SumG           float64 // sum of last (up to 3) pulse deltas — included for silo payload
	SiloDepletionDetect bool    // true on first detection (3 consecutive ≤ 1g pulses)
	OverflowDetect      bool    // true if delta > expectedG × 3
	ExpectedG           float64 // expected estimate (caller-provided)
}

// RecordWeight ingests an ESP32 HX711 reading for one completed pulse.
// expectedG is the stub estimate (estimatePerPulse) — used for the
// overflow detector and as a baseline for "3× expected".
// Negative delta (sensor noise / sloshing) is clamped to 0.
// SiloDepletionDetect fires only once per cycle (idempotent).
func (c *Cycle) RecordWeight(beforeG, afterG, expectedG float64) WeightResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	delta := beforeG - afterG
	if delta < 0 {
		delta = 0
	}
	c.actualTotalAmountG += delta
	c.lastWeightAfterG = afterG
	c.weightSeen = true

	c.recentPulseDeltas = append(c.recentPulseDeltas, delta)
	if len(c.recentPulseDeltas) > 3 {
		c.recentPulseDeltas = c.recentPulseDeltas[len(c.recentPulseDeltas)-3:]
	}

	sum3 := 0.0
	for _, d := range c.recentPulseDeltas {
		sum3 += d
	}

	res := WeightResult{
		DeltaG:             delta,
		ActualTotalAmountG: c.actualTotalAmountG,
		Last3SumG:          sum3,
		ExpectedG:          expectedG,
	}

	if !c.siloDepletionWarned && len(c.recentPulseDeltas) >= 3 {
		all := true
		for _, d := range c.recentPulseDeltas {
			if d > 1.0 {
				all = false
				break
			}
		}
		if all {
			c.siloDepletionWarned = true
			res.SiloDepletionDetect = true
		}
	}

	if expectedG > 0 && delta > expectedG*3.0 {
		res.OverflowDetect = true
	}

	return res
}

// isMaxDurationExceeded returns true when the cycle has exceeded max_duration_min.
func (c *Cycle) isMaxDurationExceeded() bool {
	if c.Mode != ModeAdaptive || c.adaptive == nil {
		return false
	}
	return time.Since(c.StartedAt) >= time.Duration(c.adaptive.MaxDurationMin)*time.Minute
}
