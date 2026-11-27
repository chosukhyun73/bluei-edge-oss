package feed_cycle

import (
	"math"
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
	// WP-F — onoff_feeder 마이크로펄스 override (0 = config 기본값). run초 가동 → stop초 정지 반복.
	RunSec  int
	StopSec int
}

// FixedParams holds parameters for a fixed (deterministic) feed cycle.
type FixedParams struct {
	PulseDurationMs int // required
	GapMs           int // required
	TotalPulses     int // required
	// 펄스별 공급시간 override (0-based index = 펄스 순번). 인덱스가 배열을 벗어나면
	// PulseDurationMs 로 폴백. 뒤 펄스 배출 끊김(정전 후유증 2026-07-08) 실험용 —
	// 뒤 펄스일수록 공급시간을 늘리는 램프 사이클을 수동 API 로 실행하기 위함.
	PulseDurationsMs []int
	// 회당 목표량(g). >0 이면 마지막 펄스 트림(잔여량 기준 공급시간 축소 — 펄스
	// 올림(ceil) 과급 방지). 0 = 트림 없음(기존 동작). 일 사이클 분할이 커지면 회당
	// 목표가 작아져 올림 과급 비율이 커지는 문제의 해소 (2026-07-07).
	TargetAmountG float64
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

	state          State
	pulsesExecuted int
	totalAmountG   float64
	// designAmountG — 설계값 기준 누적(초과 회전 제외). **제어 전용**이다.
	//
	// totalAmountG 는 급이량 **평가**용이라 초과 회전을 포함한다(2026-08-18). 그런데
	// 그 값으로 목표 도달을 판정하면 onoff_feeder API 응답 지연이 발수를 정해버린다 —
	// 8-19 실측: 정상 펄스도 매번 92~379ms 초과로 잡혀 8발 계획이 7발에서 끝났고
	// 실제 급이가 목표보다 17g 적었다. 네트워크가 느린 날 어체가 덜 먹을 이유가 없다.
	//
	// 그래서 "몇 g 나갔다고 볼 것인가"(totalAmountG)와 "몇 발 줄 것인가"(designAmountG)를
	// 분리한다. 중단 판단은 반응지수로 옮기는 중이라(운영자, 8-19) g 목표는 과도기
	// 제어이고, 거기에 지연 잡음이 섞이면 검증 중인 중단 규칙 데이터까지 오염된다.
	//
	// ⚠ 로드셀 실측(actualTotalAmountG)이 들어오면 그쪽이 둘 다 대체한다 — 실측이
	// 있으면 추정을 쓸 이유가 없다.
	designAmountG     float64
	terminationReason TerminationReason

	// 보상 펄스 (2026-07-08 운영자 지시: 2초 유지 + 무배출 감지 시 추가 펄스).
	// gap 끝 라이브 무배출 판정이 "no" 면 그 펄스의 허깨비 추정량을 차감하고
	// 펄스 허용치를 +1 늘린다. 상한은 config compensation_max_per_cycle.
	compensationsGranted int
	phantomDeductedG     float64
	// 배출이 확인된(verdict=yes) 펄스 수 — 게이트 만복 전제조건 (2026-08-02).
	confirmedDischarges int

	// 자율모드 (사이클 시작 시 캐시, 2026-07-21). 방향 A — "off" 만 반응형 보상 비활성
	// (스케줄 N펄스만). "" = 조회 실패(fail-open, 보상 유지 — 굶기지 않음).
	// 관찰/부분/전자동 = 보상 활성. 급이 축에선 관찰↑ 동일.
	autonomousMode string

	// Phase 5 (load cell). 0 이면 weight event 미수신 — 기존 totalAmountG (stub) 사용.
	actualTotalAmountG  float64
	recentPulseDeltas   []float64 // 최근 3 pulse delta (silo depletion 감지용)
	siloDepletionWarned bool
	lastWeightAfterG    float64 // 마지막 펄스 직후 통 무게 (StopAtRemainingG 평가용)
	weightSeen          bool    // RecordWeight 가 한 번이라도 호출됐는지

	// onoff 급이기 (feeder control run/stop). feederExtID>0 이면 ESP32 대신
	// onoff_feeder 경로로 펄스 구동. feederLive=false 면 dry-run(HTTP 미발행, 추정만 누적).
	feederExtID int
	feederLive  bool
	// 공급모터 리드타임 override(초) — 대시보드 그룹정책 입력(바인딩 시 policy 에서 주입). 0 = config/기본값.
	feederSpinupSec int

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

// DeriveOnoffFeederPulseParams — onoff_feeder(on/off) 사이클의 마이크로펄스 파라미터를 회당 목표량에서 도출.
// 한 번에 다 주면 물고기가 못 먹고 바닥 침전 → runSec 가동(≈rate×runSec g) → stopSec 정지(섭취)를
// 반복해 목표량을 채운다:
//
//	PulseDurationMs = runSec×1000, GapMs = stopSec×1000, MaxPulses = ceil(target/(rate×runSec))+1
//
// ⚠ PulseDurationMs 가 이미 설정(>0)돼 있으면(운영자 수동 지정) 도출하지 않는다.
// adaptive + target>0 일 때만. run/stop 초는 현장에서 조정(config).
// 반환: 도출 적용 여부.
func (c *Cycle) DeriveOnoffFeederPulseParams(runSec, stopSec int, rateGPS float64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Mode != ModeAdaptive || c.adaptive == nil {
		return false
	}
	if c.adaptive.TargetAmountG <= 0 || c.adaptive.PulseDurationMs != 0 {
		return false // 목표 없음 또는 운영자 수동 지정 → 존중
	}
	// 사이클별 override(그룹정책 입력값) 우선, 없으면 config 기본값.
	if c.adaptive.RunSec > 0 {
		runSec = c.adaptive.RunSec
	}
	if c.adaptive.StopSec > 0 {
		stopSec = c.adaptive.StopSec
	}
	if runSec <= 0 || rateGPS <= 0 {
		return false
	}
	c.adaptive.PulseDurationMs = runSec * 1000
	if c.adaptive.GapMs == 0 && stopSec > 0 {
		c.adaptive.GapMs = stopSec * 1000
	}
	burstG := rateGPS * float64(runSec) // 1 버스트 배출량
	c.adaptive.MaxPulses = int(math.Ceil(c.adaptive.TargetAmountG/burstG)) + 1
	// max_duration 도 예상 소요시간으로 확장(기본 30분이 목표 도달 전 조기 차단하는 것 방지).
	// 여유 20%. 하드캡(HardMaxSessionSec)은 별도 절대 상한으로 유지.
	estMin := float64(c.adaptive.MaxPulses) * float64(c.adaptive.PulseDurationMs+c.adaptive.GapMs) / 60000.0
	c.adaptive.MaxDurationMin = int(math.Ceil(estMin * 1.2))
	return true
}

// SetOnoffFeeder binds this cycle to a onoff feeder (feeder control run/stop).
// live=false → dry-run (추정만 누적, 실제 HTTP 미발행).
func (c *Cycle) SetOnoffFeeder(extID int, live bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.feederExtID = extID
	c.feederLive = live
}

// SetFeederSpinupSec — 대시보드 그룹정책의 리드타임 override 주입 (바인딩 시).
func (c *Cycle) SetFeederSpinupSec(sec int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.feederSpinupSec = sec
}

// FeederSpinupSec — 리드타임 override(초). 0 = 미설정(config/기본값 사용).
func (c *Cycle) FeederSpinupSec() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.feederSpinupSec
}

// IsOnoffFeeder reports whether this cycle drives a onoff feeder.
func (c *Cycle) IsOnoffFeeder() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.feederExtID > 0
}

// FeederExtID returns the onoff feeder setpoint id (0 if not onoff_feeder).
func (c *Cycle) FeederExtID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.feederExtID
}

// FeederLive reports whether real actuation is enabled (vs dry-run).
func (c *Cycle) FeederLive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.feederLive
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
// 부동소수 누적 오차(예: 18.8×10=188.00000000000003)를 소숫점 1자리로 반올림해
// 기록·표시 값을 깔끔하게 유지한다(안전 maxG 체크에도 무해).
func (c *Cycle) TotalAmountG() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return math.Round(c.totalAmountG*10) / 10
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
// estimatedG = 평가용(초과 회전 포함, 장부에 남는 값)
// designG    = 제어용(설계값만, 목표 도달·트림 계산의 기준)
func (c *Cycle) recordPulseResult(estimatedG, designG float64) {
	c.mu.Lock()
	c.pulsesExecuted++
	c.totalAmountG += estimatedG
	c.designAmountG += designG
	c.state = StatePulseComplete
	c.mu.Unlock()
}

// recordConfirmedDischarge — 배출이 확인된(verdict=yes) 펄스 1발을 센다.
// 만복 전제조건(게이트 적용 여부)의 근거 — 실제로 먹은 양이 있어야 배부를 수 있다.
func (c *Cycle) recordConfirmedDischarge() {
	c.mu.Lock()
	c.confirmedDischarges++
	c.mu.Unlock()
}

// ConfirmedDischarges — 이 사이클에서 배출이 확인된 펄스 수.
func (c *Cycle) ConfirmedDischarges() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.confirmedDischarges
}

// grantCompensation — 무배출 판정 펄스 1발의 보상 승인. 그 펄스의 추정 배출량을
// totalAmountG 에서 차감(허깨비 제거 — target 조기 도달 방지)하고 펄스 허용치를
// +1 늘린다 (maxPulses 참조).
func (c *Cycle) grantCompensation(phantomG float64) {
	c.mu.Lock()
	c.compensationsGranted++
	c.phantomDeductedG += phantomG
	c.deductLocked(phantomG)
	c.mu.Unlock()
}

// deductLocked — 허깨비 차감. 평가(totalAmountG)와 제어(designAmountG) 양쪽에서 뺀다.
// 제어값을 안 빼면 "target 조기 도달 방지"라는 차감의 목적 자체가 성립하지 않는다
// (2026-08-19 제어/평가 분리 이후). caller 가 c.mu 보유.
func (c *Cycle) deductLocked(phantomG float64) {
	c.totalAmountG -= phantomG
	if c.totalAmountG < 0 {
		c.totalAmountG = 0
	}
	c.designAmountG -= phantomG
	if c.designAmountG < 0 {
		c.designAmountG = 0
	}
}

// deductPhantomOnly — 무배출로 판정됐지만 보상은 못 나가는 펄스(보상 상한 소진·
// 자율모드 off)의 추정량을 **급이량에서만** 뺀다. 장부는 사실 기록이고 보상은
// 정책이라 분리한다 (2026-08-02 운영자 지시: "급이량만 정정, 앞으로만 정확하면 됨").
//
// 안 나간 사료를 먹인 것으로 기록하면 급이량·FCR·성장 분석이 전부 틀어진다.
// 실측(7-19~8-02): 판정 no 258건 중 51건이 보상 미발사 → 약 960g 과대계상.
// ⚠ 펄스 허용치(maxPulses = 기본 + 보상승인)는 늘리지 않으므로 발사 수는 불변.
// 재고 차감은 종전대로 둔다(운영자 결정) — 호퍼 원장 드리프트는 별도 과제.
func (c *Cycle) deductPhantomOnly(phantomG float64) {
	c.mu.Lock()
	c.phantomDeductedG += phantomG
	c.deductLocked(phantomG)
	c.mu.Unlock()
}

// compensationEnabled — 자율모드가 반응형 보상을 허용하는지 (방향 A).
// "off" 만 비활성. "" (조회 실패) 는 fail-open 으로 활성 유지.
func (c *Cycle) compensationEnabled() bool {
	return c.autonomousMode != "off"
}

// CompensationsGranted returns the number of compensation pulses granted.
func (c *Cycle) CompensationsGranted() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compensationsGranted
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
		c.mu.Lock()
		granted := c.compensationsGranted
		c.mu.Unlock()
		return c.fixed.TotalPulses + granted
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

// pulseDurationOverrideMs returns the per-pulse duration override for the
// pulse at 0-based index idx (fixed mode, PulseDurationsMs 설정 시).
// 0 = override 없음 → 호출부가 effectivePulseDurationMs 값을 유지.
func (c *Cycle) pulseDurationOverrideMs(idx int) int {
	if c.Mode != ModeFixed || c.fixed == nil {
		return 0
	}
	if idx < 0 || idx >= len(c.fixed.PulseDurationsMs) {
		return 0
	}
	return c.fixed.PulseDurationsMs[idx]
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
	var target float64
	switch {
	case c.Mode == ModeAdaptive && c.adaptive != nil:
		target = c.adaptive.TargetAmountG
	case c.Mode == ModeFixed && c.fixed != nil:
		// fixed 도 target 이 설정되면(스케줄 발화) 도달 시 종료 — 패턴(총 펄스 수)과
		// 실행 클램프(onoff_feeder 최소 공급시간)가 어긋나도 과급을 막는다 (2026-07-07).
		target = c.fixed.TargetAmountG
	}
	if target <= 0 {
		return false
	}
	// 제어는 설계값 기준 — API 지연이 발수를 정하지 않도록 (designAmountG 주석 참조).
	measured := c.designAmountG
	if c.actualTotalAmountG > 0 {
		measured = c.actualTotalAmountG // 로드셀 실측이 있으면 그쪽이 우선
	}
	return measured >= target
}

// remainingToTargetG returns target − dispensed-so-far. adaptive 는 항상,
// fixed 는 TargetAmountG 가 설정된 경우(스케줄 발화)만. 0 이면 target 없음 /
// 이미 도달. 마지막 펄스 트림용.
func (c *Cycle) remainingToTargetG() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var target float64
	switch {
	case c.Mode == ModeAdaptive && c.adaptive != nil:
		target = c.adaptive.TargetAmountG
	case c.Mode == ModeFixed && c.fixed != nil:
		target = c.fixed.TargetAmountG
	}
	if target <= 0 {
		return 0
	}
	// 트림 크기도 설계값 기준 — 초과 회전을 빼면 트림이 그만큼 짧아져 실제 급이가
	// 목표에 미달한다 (8-19 실측: 트림 p8 에 255ms=2.4g 가 얹혀 있었다).
	measured := c.designAmountG
	if c.actualTotalAmountG > 0 {
		measured = c.actualTotalAmountG
	}
	rem := target - measured
	if rem < 0 {
		return 0
	}
	return rem
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
