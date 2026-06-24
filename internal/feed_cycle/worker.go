package feed_cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

const (
	defaultGapMs           = 60_000 // 1분
	defaultPulseDurationMs = 5_000  // 5초
	defaultDispatchTimeout = 30 * time.Second
)

// Worker manages all active feed cycles and drives their state machines.
type Worker struct {
	cfg   config.FeedCycleConfig
	app   *runtime.App
	store storage.Store
	ctrl  *control.Service
	gate  SafetyGate
	log   *slog.Logger

	mu     sync.Mutex
	cycles map[string]*Cycle // cycle_id → Cycle

	// Phase 1c — AI 자율 스케줄러 (nil 가능, 설정되지 않으면 hook 호출 안 함).
	aiScheduler *AIScheduler

	// G-2 — capture worker (nil 가능, 설정되지 않거나 disabled 면 hook 호출 안 함).
	captureWorker *capture.Worker

	cancel context.CancelFunc
	done   chan struct{}
}

// NewWorker creates a Worker. gate may be nil (defaults to no-op).
func NewWorker(
	app *runtime.App,
	store storage.Store,
	ctrl *control.Service,
	cfg config.FeedCycleConfig,
	gate SafetyGate,
) *Worker {
	if gate == nil {
		gate = noopSafetyGate{}
	}
	return &Worker{
		cfg:    cfg,
		app:    app,
		store:  store,
		ctrl:   ctrl,
		gate:   gate,
		log:    slog.With("service", "feed_cycle_worker"),
		cycles: make(map[string]*Cycle),
		done:   make(chan struct{}),
	}
}

func (w *Worker) Name() string { return "feed_cycle" }

// SetAIScheduler — Phase 1c. cycle 종료 hook 으로 사용할 AI 스케줄러 등록.
// nil 허용 (테스트). main 부팅 sequence 에서 호출.
func (w *Worker) SetAIScheduler(a *AIScheduler) {
	w.aiScheduler = a
}

// SetCaptureWorker — G-2. cycle 시작 hook 으로 사용할 영상 캡처 워커 등록.
// nil 또는 disabled 면 hook skip. main 부팅 sequence 에서 호출.
func (w *Worker) SetCaptureWorker(cw *capture.Worker) {
	w.captureWorker = cw
}

func (w *Worker) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.log.Info("feed cycle worker disabled by config")
		close(w.done)
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go func() {
		defer close(w.done)
		<-runCtx.Done()
	}()
	w.app.Health.Set("feed_cycle", "ok", "")
	w.log.Info("feed cycle worker started")
	return nil
}

func (w *Worker) Stop(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	select {
	case <-w.done:
	case <-ctx.Done():
	}
	return nil
}

// StartAdaptiveCycle creates and launches a new adaptive cycle.
// priority는 "manual_override" | "ai_advisory" | "ai_autonomous" 중 하나. 빈 문자열이면 "manual_override" 사용.
// intentID는 operator_intents.intent_id (선택, 빈 문자열이면 미연결).
func (w *Worker) StartAdaptiveCycle(ctx context.Context, tankID, controllerID string, p AdaptiveParams, priority, intentID string) (*Cycle, error) {
	if priority == "" {
		priority = "manual_override"
	}
	cycleID := common.NewID("cycle")
	c := NewAdaptiveCycle(cycleID, tankID, controllerID, p)
	c.IntentID = intentID
	w.register(c)

	row := cycleToRow(c)
	row.Priority = priority
	if err := w.store.InsertFeedCycle(ctx, row); err != nil {
		w.deregister(cycleID)
		return nil, err
	}

	go w.run(c)
	return c, nil
}

// StartFixedCycle creates and launches a new fixed cycle.
// priority는 "manual_override" | "ai_advisory" | "ai_autonomous" 중 하나. 빈 문자열이면 "manual_override" 사용.
// intentID는 operator_intents.intent_id (선택, 빈 문자열이면 미연결).
func (w *Worker) StartFixedCycle(ctx context.Context, tankID, controllerID string, p FixedParams, priority, intentID string) (*Cycle, error) {
	if priority == "" {
		priority = "manual_override"
	}
	cycleID := common.NewID("cycle")
	c := NewFixedCycle(cycleID, tankID, controllerID, p)
	c.IntentID = intentID
	w.register(c)

	row := cycleToRow(c)
	row.Priority = priority
	if err := w.store.InsertFeedCycle(ctx, row); err != nil {
		w.deregister(cycleID)
		return nil, err
	}

	go w.run(c)
	return c, nil
}

// StopCycle requests an operator stop for the given cycle.
// Returns false if the cycle is not found or already complete.
func (w *Worker) StopCycle(cycleID string) bool {
	w.mu.Lock()
	c, ok := w.cycles[cycleID]
	w.mu.Unlock()
	if !ok {
		return false
	}
	if c.State() == StateCycleComplete {
		return false
	}
	c.OperatorStop()
	return true
}

// GetCycle returns a snapshot of the in-memory cycle (nil if not found).
func (w *Worker) GetCycle(cycleID string) *Cycle {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cycles[cycleID]
}

// RecordWeight ingests an ESP32 HX711 reading for the active cycle on the
// given controller. Called by the HTTP weight handler.
//
// If no active cycle exists for the controller, returns (nil, nil) — the
// reading is silently dropped (e.g. pulse arrived after cycle finalize).
//
// Emits feed.pulse.weight always, plus feed.silo.depletion / feed.overflow.detected
// when detected. Persists actual_total_amount_g to storage.
func (w *Worker) RecordWeight(ctx context.Context, controllerID, pulseID string, beforeG, afterG float64, measuredAt time.Time) (*WeightResult, *Cycle) {
	w.mu.Lock()
	var c *Cycle
	for _, cyc := range w.cycles {
		if cyc.ControllerID == controllerID {
			c = cyc
			break
		}
	}
	w.mu.Unlock()
	if c == nil {
		return nil, nil
	}

	expectedG := estimatePerPulse(c, c.effectivePulseDurationMs(w.defaultPulseDurationMs()))
	res := c.RecordWeight(beforeG, afterG, expectedG)

	w.emitEvent(ctx, "feed.pulse.weight", c.CycleID, c.TankID, map[string]any{
		"cycle_id":        c.CycleID,
		"tank_id":         c.TankID,
		"controller_id":   controllerID,
		"pulse_id":        pulseID,
		"weight_before_g": beforeG,
		"weight_after_g":  afterG,
		"delta_g":         res.DeltaG,
		"expected_g":      expectedG,
		"actual_total_g":  res.ActualTotalAmountG,
		"measured_at":     common.FormatTime(measuredAt),
	})

	if err := w.store.UpdateFeedCycleActualTotal(ctx, c.CycleID, res.ActualTotalAmountG); err != nil {
		w.log.Warn("persist actual total failed", "cycle_id", c.CycleID, "error", err)
	}

	if res.SiloDepletionDetect {
		w.emitEvent(ctx, "feed.silo.depletion", c.CycleID, c.TankID, map[string]any{
			"cycle_id":      c.CycleID,
			"tank_id":       c.TankID,
			"controller_id": controllerID,
			"last_3_sum_g":  res.Last3SumG,
			"threshold_g":   3.0,
			"detected_at":   common.FormatTime(measuredAt),
		})
		if err := w.store.SetFeedCycleSiloDepletionWarned(ctx, c.CycleID); err != nil {
			w.log.Warn("mark silo depletion failed", "cycle_id", c.CycleID, "error", err)
		}
		// 운영자 인지용 전역 알림 — open_alerts 로 승격. dashboard 헤더 banner 노출.
		// 정책 (사용자 명시 2026-05-22): close 는 *운영자 명시적 액션* 으로만. 자동 close X.
		// 센서 정상화만으로 dismiss 하면 운영자가 "처리 안 했는데 알림 사라짐" → 잊을 위험.
		if err := w.raiseSiloDepletionAlert(ctx, c.TankID, res.Last3SumG, measuredAt); err != nil {
			w.log.Warn("silo depletion alert failed", "tank_id", c.TankID, "error", err)
		}
	}

	if res.OverflowDetect {
		w.emitEvent(ctx, "feed.overflow.detected", c.CycleID, c.TankID, map[string]any{
			"cycle_id":      c.CycleID,
			"tank_id":       c.TankID,
			"controller_id": controllerID,
			"pulse_id":      pulseID,
			"delta_g":       res.DeltaG,
			"expected_g":    expectedG,
			"detected_at":   common.FormatTime(measuredAt),
		})
	}

	return &res, c
}

// HandleStreamWeight ingests a UDP weight broadcast (1Hz) from ESP32.
// If a cycle is active on this controller and its remaining grams have
// crossed StopAtRemainingG, signals immediate termination via
// siloThresholdCh (mid-pulse or in-gap). No-op when no active cycle.
// Returns the cycle that was matched (or nil) for tracing/logging.
func (w *Worker) HandleStreamWeight(ctx context.Context, controllerID string, grams float64, measuredAt time.Time) *Cycle {
	w.mu.Lock()
	var c *Cycle
	for _, cyc := range w.cycles {
		if cyc.ControllerID == controllerID {
			c = cyc
			break
		}
	}
	w.mu.Unlock()
	if c == nil {
		return nil
	}
	crossed := c.IngestStreamWeight(grams)
	if crossed {
		c.TriggerSiloThreshold()
		w.emitEvent(ctx, "feed.silo.threshold", c.CycleID, c.TankID, map[string]any{
			"cycle_id":      c.CycleID,
			"tank_id":       c.TankID,
			"controller_id": controllerID,
			"remaining_g":   grams,
			"threshold_g":   c.adaptive.StopAtRemainingG,
			"detected_at":   common.FormatTime(measuredAt),
		})
	}
	return c
}

// --- internal ---------------------------------------------------------------

// --- test helpers -----------------------------------------------------------

// TestConfig returns a minimal FeedCycleConfig for unit tests.
func TestConfig() config.FeedCycleConfig {
	return config.FeedCycleConfig{
		Enabled:                 true,
		DefaultGapMs:            1,
		DefaultPulseDurationMs:  1,
		PulseDispatchTimeoutSec: 1,
	}
}

// NewWorkerForTest creates a Worker with explicit runtime/gate for testing.
// app may be nil (events are skipped when nil).
// ctrl may be nil (dispatch is a no-op).
func NewWorkerForTest(app *runtime.App, store storage.Store, ctrl *control.Service, gate SafetyGate) *Worker {
	return NewWorker(app, store, ctrl, TestConfig(), gate)
}

// RunCycle exposes the private run method for unit tests.
func (w *Worker) RunCycle(c *Cycle) { w.run(c) }

// EmitEvent는 arbiter 등 외부에서 feed_cycle 이벤트를 발행할 때 사용한다.
func (w *Worker) EmitEvent(ctx context.Context, eventType, correlationID, tankID string, payload map[string]any) {
	w.emitEvent(ctx, eventType, correlationID, tankID, payload)
}

// RegisterCycleForTest는 테스트 전용 — 외부에서 만든 Cycle을 Worker 내부 맵에 등록한다.
// 실제 run 고루틴은 시작하지 않는다. StopCycle 호출 시 OperatorStop 신호만 발생.
func (w *Worker) RegisterCycleForTest(c *Cycle) {
	w.register(c)
}

// --- internal ---------------------------------------------------------------

func (w *Worker) register(c *Cycle) {
	w.mu.Lock()
	w.cycles[c.CycleID] = c
	w.mu.Unlock()
}

func (w *Worker) deregister(cycleID string) {
	w.mu.Lock()
	delete(w.cycles, cycleID)
	w.mu.Unlock()
}

// run drives a single cycle's state machine until cycle_complete.
func (w *Worker) run(c *Cycle) {
	ctx := context.Background()
	log := w.log.With("cycle_id", c.CycleID, "tank_id", c.TankID, "mode", c.Mode)

	now := common.NowUTC()
	w.emitEvent(ctx, "feed.cycle.started", c.CycleID, c.TankID, map[string]any{
		"cycle_id":   c.CycleID,
		"tank_id":    c.TankID,
		"mode":       string(c.Mode),
		"started_at": common.FormatTime(now),
	})

	c.transitionTo(StatePulseActive)

	// G-2 — cycle 시작 hook. 7초 영상 캡처를 background 로 발사.
	// 캡처 실패는 cycle 진행을 막지 않는다 (warn 만 로깅).
	if w.captureWorker != nil && w.captureWorker.Enabled() {
		go func(cycleID, tankID string) {
			if _, err := w.captureWorker.OnCycleStart(context.Background(), cycleID, tankID); err != nil {
				w.log.Warn("capture failed", "cycle_id", cycleID, "tank_id", tankID, "error", err)
			}
		}(c.CycleID, c.TankID)
	}

	for {
		// operator stop / silo threshold (UDP stream) 체크
		select {
		case <-c.StopCh():
			c.complete(ReasonOperatorStop)
			w.finalizeCycle(ctx, c)
			log.Info("cycle stopped by operator")
			return
		case <-c.SiloThresholdCh():
			c.complete(ReasonSiloThreshold)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: silo threshold (stream)")
			return
		default:
		}

		// target 체크 (adaptive only) — max_pulses 전에 확인해야 함
		if c.isTargetReached() {
			c.complete(ReasonSatiationOrTarget)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: target reached")
			return
		}

		// Phase 5 — silo 잔량 임계 체크 (adaptive + StopAtRemainingG>0 + weight 수신 후)
		if hit, remainingG := c.isStopAtRemainingReached(); hit {
			c.complete(ReasonSiloThreshold)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: silo remaining below threshold",
				"remaining_g", remainingG,
				"threshold_g", c.adaptive.StopAtRemainingG)
			return
		}

		// safety gate 체크 (Phase 4 C-3p hook)
		if blocked, reason := w.gate.Check(c.TankID); blocked {
			log.Info("cycle blocked by safety gate", "reason", reason)
			c.complete(ReasonSafetyBlock)
			w.finalizeCycle(ctx, c)
			return
		}

		// max_pulses 체크
		if c.PulsesExecuted() >= c.maxPulses() {
			c.complete(ReasonMaxPulses)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: max_pulses reached")
			return
		}

		// max_duration 체크
		if c.isMaxDurationExceeded() {
			c.complete(ReasonMaxDuration)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: max_duration exceeded")
			return
		}

		// --- pulse_active ---
		pulseID := common.NewID("pulse")
		pulseDurationMs := c.effectivePulseDurationMs(w.defaultPulseDurationMs())
		c.transitionTo(StatePulseActive)

		pulseStartedAt := common.NowUTC()
		w.emitEvent(ctx, "feed.pulse.started", c.CycleID, c.TankID, map[string]any{
			"cycle_id":   c.CycleID,
			"pulse_id":   pulseID,
			"pulse_no":   c.PulsesExecuted() + 1,
			"tank_id":    c.TankID,
			"started_at": common.FormatTime(pulseStartedAt),
		})

		// feed.dispense 명령 발행 (controller polling 방식 — ESP32가 가져감)
		w.dispatchPulse(ctx, c, pulseDurationMs)

		// 펄스 지속 시간 대기 (Phase 4: 실제 controller ACK 으로 교체)
		// silo threshold 가 펄스 도중 트리거되면 즉시 깨어남 (다음 iter 에서 종료 처리).
		select {
		case <-c.StopCh():
			// 펄스 도중 중단 — pulse_complete 처리 후 operator_stop 으로 마감
		case <-c.SiloThresholdCh():
			// 펄스 도중 silo 임계 — pulse_complete 처리 후 다음 iter 에서 종료
		case <-time.After(time.Duration(pulseDurationMs) * time.Millisecond):
		}

		// estimatedG: 펄스 1회당 단순 추정 (Phase 4: 실제 무게 센서로 교체)
		estimatedG := estimatePerPulse(c, pulseDurationMs)
		c.recordPulseResult(estimatedG)

		pulseEndedAt := common.NowUTC()
		actualDurationMs := int(pulseEndedAt.Sub(pulseStartedAt).Milliseconds())
		w.emitEvent(ctx, "feed.pulse.completed", c.CycleID, c.TankID, map[string]any{
			"cycle_id":           c.CycleID,
			"pulse_id":           pulseID,
			"pulse_no":           c.PulsesExecuted(),
			"tank_id":            c.TankID,
			"actual_duration_ms": actualDurationMs,
			"estimated_amount_g": estimatedG,
			"total_amount_g":     c.TotalAmountG(),
			"completed_at":       common.FormatTime(pulseEndedAt),
		})

		w.store.UpdateFeedCycleProgress(ctx, c.CycleID, c.PulsesExecuted(), c.TotalAmountG())

		// operator stop / silo threshold 재확인 (펄스 완료 후)
		select {
		case <-c.StopCh():
			c.complete(ReasonOperatorStop)
			w.finalizeCycle(ctx, c)
			log.Info("cycle stopped by operator after pulse")
			return
		case <-c.SiloThresholdCh():
			c.complete(ReasonSiloThreshold)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: silo threshold after pulse")
			return
		default:
		}

		// --- gap_observation ---
		gapMs := c.effectiveGapMs(w.defaultGapMs())
		c.transitionTo(StateGapObservation)

		gapStartedAt := common.NowUTC()
		w.emitEvent(ctx, "feed.gap.started", c.CycleID, c.TankID, map[string]any{
			"cycle_id":   c.CycleID,
			"tank_id":    c.TankID,
			"gap_ms":     gapMs,
			"started_at": common.FormatTime(gapStartedAt),
		})

		gapTimer := time.NewTimer(time.Duration(gapMs) * time.Millisecond)
		select {
		case <-c.StopCh():
			gapTimer.Stop()
			c.complete(ReasonOperatorStop)
			w.finalizeCycle(ctx, c)
			log.Info("cycle stopped by operator during gap")
			return
		case <-c.SiloThresholdCh():
			gapTimer.Stop()
			c.complete(ReasonSiloThreshold)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: silo threshold during gap")
			return
		case <-gapTimer.C:
		}

		w.emitEvent(ctx, "feed.gap.completed", c.CycleID, c.TankID, map[string]any{
			"cycle_id":     c.CycleID,
			"tank_id":      c.TankID,
			"gap_ms":       gapMs,
			"completed_at": common.FormatTime(common.NowUTC()),
		})

		// 다음 반복 (pulse_active 로 돌아감)
		c.transitionTo(StatePulseActive)
	}
}

// dispatchPulse queues a feed.dispense command for the ESP32 controller.
func (w *Worker) dispatchPulse(ctx context.Context, c *Cycle, pulseDurationMs int) {
	if w.ctrl == nil || c.ControllerID == "" {
		return
	}
	timeout := w.dispatchTimeout()
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Phase A — ESP32 펌웨어가 받는 모터 출력 (0/미설정이면 펌웨어 default).
	params := map[string]any{
		"duration_ms": pulseDurationMs,
		"cycle_id":    c.CycleID,
	}
	if speedRpm := c.SpeedRpm(); speedRpm > 0 {
		params["speed_rpm"] = speedRpm
	}
	if amount := c.Amount(); amount > 0 {
		params["amount"] = amount
	}

	// controller_id 와 device_id 가 다른 경우 (대부분의 yaml seed) device 매핑 필요.
	// feeder_tank_02 (controller) → feeder_esp32_02 (device) 같은 변환.
	targetDeviceID := c.ControllerID
	if devs := w.ctrl.DeviceIDsForController(c.ControllerID); len(devs) > 0 {
		targetDeviceID = devs[0]
	}
	_, err := w.ctrl.Submit(dctx, control.CommandRequest{
		IdempotencyKey: common.NewID("idem"),
		RequestedBy:    map[string]any{"source": "feed_cycle", "cycle_id": c.CycleID},
		Target:         map[string]any{"device_id": targetDeviceID},
		Command: map[string]any{
			"type":   "feed.dispense",
			"params": params,
		},
		ExpiresInSec: int(timeout.Seconds()) + int(time.Duration(pulseDurationMs)*time.Millisecond/time.Second) + 10,
	})
	if err != nil {
		w.log.Warn("feed pulse dispatch failed", "cycle_id", c.CycleID, "error", err)
	}
}

func (w *Worker) finalizeCycle(ctx context.Context, c *Cycle) {
	completedAt := common.NowUTC()
	w.emitEvent(ctx, "feed.cycle.completed", c.CycleID, c.TankID, map[string]any{
		"cycle_id":           c.CycleID,
		"tank_id":            c.TankID,
		"mode":               string(c.Mode),
		"pulses_executed":    c.PulsesExecuted(),
		"total_amount_g":     c.TotalAmountG(),
		"termination_reason": string(c.TerminationReason()),
		"completed_at":       common.FormatTime(completedAt),
	})

	w.store.CompleteFeedCycle(ctx, c.CycleID, c.PulsesExecuted(), c.TotalAmountG(), string(c.TerminationReason()), completedAt)
	w.deregister(c.CycleID)

	// Phase 1c — AI scheduler hook. operating_mode='auto' 면 다음 cycle 자동 등록.
	// 별도 goroutine — finalize 차단 X. arbiter / DB 조회 비용 격리.
	if w.aiScheduler != nil {
		go w.aiScheduler.OnCycleComplete(context.Background(), c.TankID, completedAt)
	}
}

func (w *Worker) emitEvent(ctx context.Context, eventType, correlationID, tankID string, payload map[string]any) {
	if w.app == nil {
		return
	}
	if _, err := w.app.AppendEvent(ctx, "feed_cycle", "", tankID, eventType, correlationID, payload); err != nil {
		w.log.Warn("failed to emit event", "event_type", eventType, "error", err)
	}
}

func (w *Worker) defaultGapMs() int {
	if w.cfg.DefaultGapMs > 0 {
		return w.cfg.DefaultGapMs
	}
	return defaultGapMs
}

func (w *Worker) defaultPulseDurationMs() int {
	if w.cfg.DefaultPulseDurationMs > 0 {
		return w.cfg.DefaultPulseDurationMs
	}
	return defaultPulseDurationMs
}

func (w *Worker) dispatchTimeout() time.Duration {
	if w.cfg.PulseDispatchTimeoutSec > 0 {
		return time.Duration(w.cfg.PulseDispatchTimeoutSec) * time.Second
	}
	return defaultDispatchTimeout
}

// raiseSiloDepletionAlert — silo 빈 감지 시 운영자 알림으로 승격.
// dedupeKey 로 Cage/Tank 당 1건만 유지 (idempotent). 운영자가 사료 보충 후
// 명시적으로 close 하거나, Phase B 의 잔량 추적 로직에서 자동 close.
// 실패는 silent — feed cycle 진행에는 영향 없음 (alert 누락이 cycle 중단 사유 아님).
func (w *Worker) raiseSiloDepletionAlert(ctx context.Context, tankID string, last3SumG float64, detectedAt time.Time) error {
	key := "tank.silo.depletion." + tankID
	existing, err := w.store.GetOpenAlert(ctx, key)
	if err != nil {
		return fmt.Errorf("get open alert: %w", err)
	}
	if existing != nil {
		return nil // 이미 열려있음 — idempotent
	}
	alertID := common.NewAlertID()
	msg := fmt.Sprintf("Cage/Tank %s 사료통 보충 필요 — 3 연속 펄스 중량 감소 합계 %.2fg ≤ 임계 3.0g", tankID, last3SumG)
	payload := map[string]any{
		"alert_id":   alertID,
		"alert_type": "tank.silo.depletion",
		"severity":   "warning",
		"status":     "open",
		"subject":    map[string]any{"kind": "tank", "id": tankID},
		"message":    msg,
		"evidence": map[string]any{
			"tank_id":      tankID,
			"last_3_sum_g": last3SumG,
			"threshold_g":  3.0,
			"detected_at":  common.FormatTime(detectedAt),
		},
		"raised_at":  common.FormatTime(detectedAt),
		"updated_at": common.FormatTime(detectedAt),
	}
	body, _ := json.Marshal(payload)
	row := &storage.OpenAlert{
		AlertID:        alertID,
		AlertDedupeKey: key,
		AlertType:      "tank.silo.depletion",
		Severity:       "warning",
		SubjectKind:    "tank",
		SubjectID:      tankID,
		Status:         "open",
		RaisedAt:       detectedAt,
		UpdatedAt:      detectedAt,
		PayloadJSON:    string(body),
	}
	if _, err := w.store.UpsertAlert(ctx, row); err != nil {
		return fmt.Errorf("upsert alert: %w", err)
	}
	if w.app != nil {
		_, _ = w.app.AppendEvent(ctx, "feed_cycle", "", tankID, "alert.raised", alertID, payload)
	}
	return nil
}

// estimatePerPulse returns a simple per-pulse feed estimate.
// Phase 4: replace with load-cell reading.
//
// 추정 전략: 시간 기반 1g/sec 단순 모델 (Phase 3 stub).
// adaptive.TargetAmountG / MaxPulses 는 사용하지 않음 — 그렇게 하면
// maxPulses 실행 후 항상 정확히 target 에 도달해 두 종료 조건이 동시에 발생.
func estimatePerPulse(_ *Cycle, pulseDurationMs int) float64 {
	// 1g/sec 단순 모델
	return float64(pulseDurationMs) / 1000.0
}

// cycleToRow converts a Cycle to a storage FeedCycle row for initial insert.
func cycleToRow(c *Cycle) *storage.FeedCycle {
	row := &storage.FeedCycle{
		CycleID:      c.CycleID,
		TankID:       c.TankID,
		ControllerID: c.ControllerID,
		Mode:         string(c.Mode),
		StartedAt:    c.StartedAt,
		IntentID:     c.IntentID,
		SpeedRpm:     c.SpeedRpm(),
		Amount:       c.Amount(),
	}
	if c.Mode == ModeAdaptive && c.adaptive != nil {
		row.TargetAmountG = c.adaptive.TargetAmountG
		params, _ := json.Marshal(c.adaptive)
		row.ParamsJSON = string(params)
	}
	if c.Mode == ModeFixed && c.fixed != nil {
		params, _ := json.Marshal(c.fixed)
		row.ParamsJSON = string(params)
	}
	return row
}
