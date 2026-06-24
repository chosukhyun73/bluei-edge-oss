package arbiter

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
)

// Arbiter는 다중 소스의 사이클 요청을 우선순위에 따라 중재한다.
// Phase G: 환경 safety gate 가 모든 cycle source 의 단일 진입점으로 작동한다.
type Arbiter struct {
	fc            *feed_cycle.Worker
	store         storage.Store
	safetyGateCfg config.ArbiterSafetyGateConfig
	log           *slog.Logger
}

// New creates a new Arbiter wrapping the feed cycle Worker.
// safetyGateCfg 가 Enabled=false 이면 환경 게이트 검사를 skip 하고 기존 동작을 유지한다.
func New(fc *feed_cycle.Worker, store storage.Store, safetyGateCfg config.ArbiterSafetyGateConfig) *Arbiter {
	return &Arbiter{
		fc:            fc,
		store:         store,
		safetyGateCfg: safetyGateCfg,
		log:           slog.Default(),
	}
}

// Submit processes a CycleRequest and returns a Decision.
// Phase 6: manual_override 요청은 ai_advisory/ai_autonomous 활성 사이클을 선점할 수 있다.
// ai_advisory, ai_autonomous 는 어떤 우선순위의 활성 사이클도 선점할 수 없다.
func (a *Arbiter) Submit(ctx context.Context, req CycleRequest) (Decision, error) {
	// Priority 자동 설정 (호출자가 설정하지 않은 경우)
	if req.Priority == 0 {
		req.Priority = sourcePriority(req.Source)
	}
	if req.SubmittedAt.IsZero() {
		req.SubmittedAt = common.NowUTC()
	}

	decisionID := common.NewID("arb")

	// Phase G: 환경 safety gate — 모든 source (운영자 force 포함) bypass 불가.
	if ok, reason := a.checkEnvironmentGate(ctx, req.TankID); !ok {
		a.recordDecisionPreempt(ctx, decisionID, req, false, reason, "", "", "")
		return Decision{
			Accepted:        false,
			RejectionReason: reason,
			DecisionID:      decisionID,
		}, nil
	}

	// 활성 사이클 조회
	activeCycles, err := a.store.ListActiveFeedCycles(ctx)
	if err != nil {
		return Decision{}, err
	}

	// 동일 Tank에 활성 사이클 존재 여부 확인
	var existingCycle *storage.FeedCycle
	for _, c := range activeCycles {
		if c.TankID == req.TankID {
			existingCycle = c
			break
		}
	}

	if existingCycle != nil {
		existingPriority := parsePriorityLabel(existingCycle.Priority)

		// 선점 조건: 신규 요청이 manual_override이고 기존 사이클이 그보다 낮은 우선순위인 경우
		if req.Priority == PriorityManualOverride && existingPriority < PriorityManualOverride {
			// 선점 실행: 기존 사이클 중단 요청
			stopped := a.fc.StopCycle(existingCycle.CycleID)

			// StopCycle이 false를 반환하면 Worker 맵에 없는 상태 — 이미 완료됨으로 간주
			preempted := !stopped
			if stopped {
				// 최대 2초 대기 — run 고루틴이 finalizeCycle → store.CompleteFeedCycle 를 호출할 때까지
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					cycles, pollErr := a.store.ListActiveFeedCycles(ctx)
					if pollErr != nil {
						break
					}
					stillActive := false
					for _, c := range cycles {
						if c.CycleID == existingCycle.CycleID {
							stillActive = true
							break
						}
					}
					if !stillActive {
						preempted = true
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
			}

			if !preempted {
				// 2초 내 중단 실패 — 선점 실패 반환
				a.recordDecisionPreempt(ctx, decisionID, req, false, "preempt_failed", existingCycle.CycleID, "", "")
				return Decision{}, errors.New("PREEMPT_FAILED: existing cycle did not stop within 2s")
			}

			// 선점 이벤트 발행
			a.fc.EmitEvent(ctx, "feed.cycle.preempted", existingCycle.CycleID, req.TankID, map[string]any{
				"preempted_cycle_id":     existingCycle.CycleID,
				"preempting_decision_id": decisionID,
				"reason":                 "preempted_by_higher_priority",
			})

			// 새 사이클 시작
			cycleID, err := a.startCycle(ctx, req)
			if err != nil {
				return Decision{}, err
			}

			a.recordDecisionPreempt(ctx, decisionID, req, true, "", "", cycleID, existingCycle.CycleID)
			return Decision{
				Accepted:         true,
				ResultingCycleID: cycleID,
				DecisionID:       decisionID,
				PreemptedCycleID: existingCycle.CycleID,
			}, nil
		}

		// 선점 조건 미충족 — 거부
		reason := "active_cycle_exists"
		a.recordDecisionPreempt(ctx, decisionID, req, false, reason, existingCycle.CycleID, "", "")
		return Decision{
			Accepted:        false,
			RejectionReason: reason,
			ExistingCycleID: existingCycle.CycleID,
			DecisionID:      decisionID,
		}, nil
	}

	// 활성 사이클 없음 — 새 사이클 시작
	cycleID, err := a.startCycle(ctx, req)
	if err != nil {
		return Decision{}, err
	}

	a.recordDecisionPreempt(ctx, decisionID, req, true, "", "", cycleID, "")
	return Decision{
		Accepted:         true,
		ResultingCycleID: cycleID,
		DecisionID:       decisionID,
	}, nil
}

// startCycle은 req.Mode에 따라 적절한 사이클을 시작하고 cycle_id를 반환한다.
func (a *Arbiter) startCycle(ctx context.Context, req CycleRequest) (string, error) {
	priority := priorityLabel(req.Priority)
	switch req.Mode {
	case "adaptive":
		p, err := parseAdaptiveParams(req.Params)
		if err != nil {
			return "", err
		}
		c, err := a.fc.StartAdaptiveCycle(ctx, req.TankID, req.ControllerID, p, priority, req.IntentID)
		if err != nil {
			return "", err
		}
		return c.CycleID, nil
	case "fixed":
		p, err := parseFixedParams(req.Params)
		if err != nil {
			return "", err
		}
		c, err := a.fc.StartFixedCycle(ctx, req.TankID, req.ControllerID, p, priority, req.IntentID)
		if err != nil {
			return "", err
		}
		return c.CycleID, nil
	default:
		return "", &ErrInvalidMode{Mode: req.Mode}
	}
}

// recordDecisionPreempt은 arbiter_decisions 테이블에 감사 레코드를 기록한다.
// 오류 발생 시 무시 — audit 실패가 요청을 막지 않는다.
func (a *Arbiter) recordDecisionPreempt(
	ctx context.Context,
	decisionID string,
	req CycleRequest,
	accepted bool,
	rejectionReason string,
	existingCycleID string,
	resultingCycleID string,
	preemptedCycleID string,
) {
	if a.store == nil {
		return
	}
	d := &storage.ArbiterDecision{
		DecisionID:       decisionID,
		TankID:           req.TankID,
		Source:           string(req.Source),
		Priority:         priorityLabel(req.Priority),
		Accepted:         accepted,
		RejectionReason:  rejectionReason,
		ResultingCycleID: resultingCycleID,
		ExistingCycleID:  existingCycleID,
		IntentID:         req.IntentID,
		SubmittedAt:      req.SubmittedAt,
		DecidedAt:        time.Now().UTC(),
		PreemptedCycleID: preemptedCycleID,
	}
	_ = a.store.InsertArbiterDecision(ctx, d)
}

// parsePriorityLabel은 문자열 우선순위 레이블을 Priority 값으로 변환한다.
func parsePriorityLabel(label string) Priority {
	switch label {
	case "manual_override":
		return PriorityManualOverride
	case "ai_advisory":
		return PriorityAIAdvisory
	case "ai_autonomous":
		return PriorityAIAutonomous
	default:
		// 알 수 없는 레이블은 가장 높은 우선순위로 취급 — 안전 보수적
		return PriorityManualOverride
	}
}

func priorityLabel(p Priority) string {
	switch p {
	case PriorityManualOverride:
		return "manual_override"
	case PriorityAIAdvisory:
		return "ai_advisory"
	case PriorityAIAutonomous:
		return "ai_autonomous"
	default:
		return "unknown"
	}
}

// ErrInvalidMode is returned when Mode is neither "adaptive" nor "fixed".
type ErrInvalidMode struct{ Mode string }

func (e *ErrInvalidMode) Error() string { return "invalid mode: " + e.Mode }

// --- param parsers (동일 로직을 api 패키지에서 복사, 공용화 X — 단순하게 유지) ---

func parseAdaptiveParams(params map[string]any) (feed_cycle.AdaptiveParams, error) {
	p := feed_cycle.AdaptiveParams{}
	if params == nil {
		params = map[string]any{}
	}
	if v, ok := params["target_amount_g"]; ok {
		p.TargetAmountG = toFloat64(v)
	}
	if p.TargetAmountG <= 0 {
		return p, &ErrInvalidMode{Mode: "adaptive: target_amount_g must be > 0"}
	}
	if v, ok := params["max_pulses"]; ok {
		p.MaxPulses = int(toFloat64(v))
	}
	if v, ok := params["max_duration_min"]; ok {
		p.MaxDurationMin = int(toFloat64(v))
	}
	if v, ok := params["gap_ms"]; ok {
		p.GapMs = int(toFloat64(v))
	}
	if v, ok := params["pulse_duration_ms"]; ok {
		p.PulseDurationMs = int(toFloat64(v))
	}
	// Phase A — ESP32 모터 출력 (0 = 펌웨어 default)
	if v, ok := params["speed_rpm"]; ok {
		p.SpeedRpm = int(toFloat64(v))
		if err := validateSpeedRpm(p.SpeedRpm); err != nil {
			return p, err
		}
	}
	if v, ok := params["amount"]; ok {
		p.Amount = int(toFloat64(v))
		if err := validateAmount(p.Amount); err != nil {
			return p, err
		}
	}
	// Phase 5 — 통 잔량 임계 (load cell). 0 = 비활성.
	if v, ok := params["stop_at_remaining_g"]; ok {
		p.StopAtRemainingG = toFloat64(v)
		if p.StopAtRemainingG < 0 {
			return p, &ErrInvalidMode{Mode: "adaptive: stop_at_remaining_g must be >= 0"}
		}
	}
	return p, nil
}

func parseFixedParams(params map[string]any) (feed_cycle.FixedParams, error) {
	p := feed_cycle.FixedParams{}
	if params == nil {
		params = map[string]any{}
	}
	if v, ok := params["pulse_duration_ms"]; ok {
		p.PulseDurationMs = int(toFloat64(v))
	}
	if v, ok := params["gap_ms"]; ok {
		p.GapMs = int(toFloat64(v))
	}
	if v, ok := params["total_pulses"]; ok {
		p.TotalPulses = int(toFloat64(v))
	}
	if p.PulseDurationMs <= 0 {
		return p, &ErrInvalidMode{Mode: "fixed: pulse_duration_ms must be > 0"}
	}
	if p.GapMs < 0 {
		return p, &ErrInvalidMode{Mode: "fixed: gap_ms must be >= 0"}
	}
	if p.TotalPulses <= 0 {
		return p, &ErrInvalidMode{Mode: "fixed: total_pulses must be > 0"}
	}
	if v, ok := params["speed_rpm"]; ok {
		p.SpeedRpm = int(toFloat64(v))
		if err := validateSpeedRpm(p.SpeedRpm); err != nil {
			return p, err
		}
	}
	if v, ok := params["amount"]; ok {
		p.Amount = int(toFloat64(v))
		if err := validateAmount(p.Amount); err != nil {
			return p, err
		}
	}
	return p, nil
}

// validateSpeedRpm — ESP32 펌웨어 SPEED_MIN_RPM=14, SPEED_MAX_RPM=42. 0 = default.
func validateSpeedRpm(rpm int) error {
	if rpm == 0 {
		return nil
	}
	if rpm < 14 || rpm > 42 {
		return &ErrInvalidMode{Mode: "speed_rpm out of range (14~42 or 0 for default)"}
	}
	return nil
}

// validateAmount — ESP32 DAC 0~255.
func validateAmount(amount int) error {
	if amount < 0 || amount > 255 {
		return &ErrInvalidMode{Mode: "amount out of range (0~255)"}
	}
	return nil
}

// checkEnvironmentGate는 tank 의 최신 수온/DO 센서값을 확인한다.
// safetyGateCfg.Enabled=false 이면 항상 (true, "") 를 반환한다.
// 위반 시 (false, reason) 을 반환한다.
func (a *Arbiter) checkEnvironmentGate(ctx context.Context, tankID string) (bool, string) {
	cfg := a.safetyGateCfg
	if !cfg.Enabled {
		return true, ""
	}

	staleCutoff := time.Duration(cfg.SensorMaxStaleSec) * time.Second

	// 수온 검사
	if ok, reason := a.checkMetric(ctx, tankID, "water_temperature", staleCutoff, func(v float64) (bool, string) {
		if v < cfg.TempMinC {
			return false, "safety_gate:temp_critical_low"
		}
		if v > cfg.TempMaxC {
			return false, "safety_gate:temp_critical_high"
		}
		return true, ""
	}); !ok {
		return false, reason
	}

	// DO 검사
	if ok, reason := a.checkMetric(ctx, tankID, "dissolved_oxygen", staleCutoff, func(v float64) (bool, string) {
		if v < cfg.DOMinMgL {
			return false, "safety_gate:oxygen_critical_low"
		}
		return true, ""
	}); !ok {
		return false, reason
	}

	return true, ""
}

// checkMetric 은 주어진 metric 의 최신 값을 조회하고 stale/missing/threshold 검사를 수행한다.
func (a *Arbiter) checkMetric(
	ctx context.Context,
	tankID string,
	metric string,
	staleCutoff time.Duration,
	check func(float64) (bool, string),
) (bool, string) {
	readings, err := a.store.LatestSensorReadings(ctx, storage.LatestReadingFilter{
		TankID: tankID,
		Metric: metric,
		Limit:  1,
	})
	if err != nil || len(readings) == 0 {
		reason := "safety_gate:sensor_missing:" + metric
		a.log.Warn("arbiter: cycle rejected by safety gate", "tank_id", tankID, "reason", reason)
		return false, reason
	}

	r := readings[0]

	// observed_at 파싱 — 실패 시 안전 측(reject)
	observedAt, parseErr := time.Parse(time.RFC3339, r.ObservedAt)
	if parseErr != nil {
		reason := "safety_gate:sensor_observed_at_invalid"
		a.log.Warn("arbiter: cycle rejected by safety gate", "tank_id", tankID, "reason", reason, "metric", metric, "observed_at", r.ObservedAt)
		return false, reason
	}

	if time.Since(observedAt) > staleCutoff {
		reason := "safety_gate:sensor_stale:" + metric
		a.log.Warn("arbiter: cycle rejected by safety gate", "tank_id", tankID, "reason", reason, "metric", metric, "age_sec", int(time.Since(observedAt).Seconds()))
		return false, reason
	}

	if r.Value == nil {
		reason := "safety_gate:sensor_missing:" + metric
		a.log.Warn("arbiter: cycle rejected by safety gate", "tank_id", tankID, "reason", reason, "metric", metric)
		return false, reason
	}

	ok, reason := check(*r.Value)
	if !ok {
		a.log.Warn("arbiter: cycle rejected by safety gate", "tank_id", tankID, "reason", reason, "metric", metric, "value", *r.Value)
	}
	return ok, reason
}

func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
