package feed_cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/control"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
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

	// visionGate — 판정 카메라 생존 확인 (nil 가능, 미설정이면 종전 동작).
	// vision_gate.go 참고.
	visionGate VisionGate

	mu     sync.Mutex
	cycles map[string]*Cycle // cycle_id → Cycle

	// Phase 1c — AI 자율 스케줄러 (nil 가능, 설정되지 않으면 hook 호출 안 함).
	aiScheduler *AIScheduler

	// G-2 — capture worker (nil 가능, 설정되지 않거나 disabled 면 hook 호출 안 함).
	captureWorker *capture.Worker

	// 사이클 완료 자동 보고 (nil 가능, 설정되지 않으면 hook 호출 안 함).
	reporter *Reporter

	cancel context.CancelFunc
	done   chan struct{}

	// 테스트 전용 — nil 이면 실제 onoff feeder control HTTP. 설정 시 그 함수로 대체.
	feederActuateFn func(c *Cycle, cmd string) error

	// 보상 펄스 — 라이브 요동 series 조회 (nil 이면 판정/보상 비활성).
	// main 이 vision_pipeline.LiveAgitation 세션을 감싸 주입한다.
	liveSeriesFn func(cycleID string) (series []float64, startedAt time.Time, ok bool)

	// r3 섀도 판정 — LRCN 세그먼트 채점기 (nil 이면 섀도 비활성). reporter 와 같은 객체.
	segScorer segmentScorer
}

// SetSegmentScorer wires the LRCN segment scorer used by the r3 shadow discharge
// judgment (feed.pulse.r3_shadow). nil = 섀도 비활성. reporter 와 동일 객체 주입.
func (w *Worker) SetSegmentScorer(s segmentScorer) { w.segScorer = s }

// SetLiveSeriesProvider wires the live agitation series lookup used by the
// gap-end discharge judgment (compensation pulse). nil = 비활성.
func (w *Worker) SetLiveSeriesProvider(fn func(cycleID string) ([]float64, time.Time, bool)) {
	w.liveSeriesFn = fn
}

// SetFeederActuateForTest overrides onoff feeder actuation (feeder control).
// 테스트에서 run/stop 성공·실패를 주입해 stop 신호열·연속실패 경로를 검증한다.
func (w *Worker) SetFeederActuateForTest(fn func(c *Cycle, cmd string) error) {
	w.feederActuateFn = fn
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

// SetReporter — 사이클 완료 자동 보고 등록. nil 허용 (테스트). main 부팅 sequence 에서 호출.
func (w *Worker) SetReporter(r *Reporter) {
	w.reporter = r
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
	// 비전 게이트 — 눈이 없으면 시작하지 않는다 (2026-08-25 운영자 지시).
	// 사이클 행을 만들기 전에 막으므로 "0발 사이클" 기록이 남지 않는다. 대신
	// 알림으로 남겨 급이가 왜 안 나갔는지 운영자가 알 수 있게 한다.
	if blind, detail := w.visionBlocked(tankID); blind {
		w.raiseVisionBlackoutAlert(ctx, tankID, "", detail, 0)
		return nil, fmt.Errorf("vision blackout: %s", detail)
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

	expectedG := estimatePerPulse(c, c.effectivePulseDurationMs(w.defaultPulseDurationMs()), w.feederRateGPS())
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
		FeederSpinupSec:         -1, // 리드 없음 — 실클럭 테스트가 펄스당 +10s 되는 것 방지
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

// EmergencyStopAll — 프로세스 종료 직전 호출: 진행 중인 모든 onoff_feeder(live) 사이클의
// 급이기에 stop 을 직접 발행하고 사이클에 operator stop 신호를 보낸다.
// 2026-07-07 실사고: 재시작이 펄스 ON 직후에 걸리면 stop 미발행 → 급이기 57초 방치.
// feederStop 은 fresh context 로 발행되므로 shutdown 중에도 전송된다(멱등).
func (w *Worker) EmergencyStopAll() {
	w.mu.Lock()
	cycles := make([]*Cycle, 0, len(w.cycles))
	for _, c := range w.cycles {
		cycles = append(cycles, c)
	}
	w.mu.Unlock()
	for _, c := range cycles {
		if c.IsOnoffFeeder() {
			if err := w.feederStop(c); err != nil {
				w.log.Error("shutdown feeder stop FAILED — 급이기 수동 확인 필요",
					"cycle_id", c.CycleID, "tank_id", c.TankID, "error", err)
			} else {
				w.log.Info("shutdown feeder stop sent", "cycle_id", c.CycleID, "tank_id", c.TankID)
			}
		}
		c.OperatorStop() // 사이클 고루틴 종료 신호 (finalize 경로에서 confirm stop 추가 발행)
	}
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

	// 자율모드 → 반응형 보상 게이트 (방향 A: off=스케줄만, 관찰↑=보상). 사이클 시작 시 1회 캐시.
	// 조회 실패 시 필드 "" 유지 → compensationEnabled()=true (fail-open, 굶기지 않음).
	if m, err := w.store.GetTankAutonomousMode(ctx, c.TankID); err != nil {
		w.log.Warn("autonomous mode lookup failed — compensation kept ON (fail-open)",
			"tank_id", c.TankID, "error", err)
	} else if m != nil {
		c.autonomousMode = m.Mode
	} else {
		c.autonomousMode = "off" // 미설정 탱크 = off 기본 (반응형 보상 없음)
	}

	now := common.NowUTC()
	w.emitEvent(ctx, "feed.cycle.started", c.CycleID, c.TankID, map[string]any{
		"cycle_id":        c.CycleID,
		"tank_id":         c.TankID,
		"mode":            string(c.Mode),
		"autonomous_mode": c.autonomousMode,
		"started_at":      common.FormatTime(now),
	})

	c.transitionTo(StatePulseActive)

	// onoff 급이기 바인딩 (경연 경로). 급이기면 펄스를 feeder control run/stop 으로 구동.
	// controller_id 미전달(대시보드 Start)이면 tank_id 로 급이기 컨트롤러 해석. 실작동은 allowlist(tank_02).
	w.resolveOnoffFeeder(ctx, c)

	// WP-F — onoff_feeder(on/off) 사이클은 회당 목표량에서 마이크로펄스(run초 가동 → stop초 정지 반복) 도출.
	// 도출 없으면 ESP32 default(펄스 23.5g×max10=235g)에서 멈춤. 한 번에 다 주면 바닥 침전 → 짧게 나눠 급이.
	if c.IsOnoffFeeder() {
		if c.DeriveOnoffFeederPulseParams(w.feederRunSec(), w.feederStopSec(), w.feederRateGPS()) {
			log.Info("onoff_feeder pulse params derived from target",
				"run_sec", w.feederRunSec(), "stop_sec", w.feederStopSec(),
				"pulse_ms", c.effectivePulseDurationMs(w.defaultPulseDurationMs()),
				"max_pulses", c.maxPulses())
		}
	}

	// 비전 D-3 — 신호게이팅 급이 캡처 시작(비블로킹). 수조 전 카메라에서 첫 ON~마지막 OFF
	// 구간을 1개 긴 clip 으로 녹화. finalizeCycle 의 StopCycleRecording 이 종료.
	// 캡처 실패는 cycle 진행을 막지 않는다 (warn 만 로깅).
	if w.captureWorker != nil && w.captureWorker.Enabled() {
		if err := w.captureWorker.StartCycleRecording(context.Background(), c.CycleID, c.TankID); err != nil {
			w.log.Warn("gated capture start failed", "cycle_id", c.CycleID, "tank_id", c.TankID, "error", err)
		}
	}

	// onoff_feeder stop 신뢰성 — 펄스 종료마다 stop 1발(신호 다이어트, 2026-07-06. gap 재전송 제거 —
	// 다운링크 큐잉 시 stop 이 다음 run 뒤에 배달돼 펄스를 사살). 드롭된 stop 은 다음 펄스의
	// stop 또는 사이클 마무리 confirm stop 이 흡수한다.
	// 연속 실패가 임계를 넘으면 abort=true 반환 → 호출부가 critical 알림 + 안전정지.
	consecutiveStopFailures := 0
	// sendFeederStop — stop 1발 + 실패 시 즉시 재전송. 실패해도 다음 정기 stop
	// (gap +5초 확인 stop)까지 기다리면 그동안 모터가 계속 돈다 — 2026-08-18 실사고에서
	// 그 5초가 초과 회전 16.3초 중 5초를 차지했다. 멱등이라 중복 전송은 무해하고,
	// onoff_feeder 1req/s 제한은 재전송 간격으로 지킨다.
	sendFeederStop := func() (abort bool) {
		err := w.feederStop(c)
		for i := 0; err != nil && i < feederStopQuickRetries; i++ {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(feederStopQuickRetryGap):
			}
			w.log.Warn("feeder stop 즉시 재전송", "cycle_id", c.CycleID, "tank_id", c.TankID,
				"attempt", i+2, "prev_error", err)
			err = w.feederStop(c)
		}
		if err != nil {
			consecutiveStopFailures++
			if consecutiveStopFailures >= feederStopFailureThreshold {
				w.raiseFeederStopFailureAlert(ctx, c.TankID, c.CycleID, consecutiveStopFailures, common.NowUTC())
				return true
			}
		} else {
			consecutiveStopFailures = 0
		}
		return false
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

		// 비전 게이트 — 사이클 도중 판정 카메라가 끊기면 그 순간부터 멈춘다
		// (2026-08-25 운영자 지시). 펄스와 펄스 사이라 급이기는 이미 정지 상태다.
		if blind, detail := w.visionBlocked(c.TankID); blind {
			c.complete(ReasonVisionBlackout)
			w.finalizeCycle(ctx, c)
			w.raiseVisionBlackoutAlert(ctx, c.TankID, c.CycleID, detail, c.PulsesExecuted())
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

		// 하드 안전 상한 (params 무관 강제 종료) — onoff_feeder 실작동 폭주 방지
		if w.hardCapExceeded(c) {
			c.complete(ReasonMaxDuration)
			w.finalizeCycle(ctx, c)
			log.Warn("cycle complete: HARD safety cap (max session/total grams)")
			return
		}

		// 보상 펄스 만복 게이트 — 원 계획을 넘어서는 펄스(보상) 발사 직전, 어체
		// 활동이 기저로 복귀(만복 신호)했으면 남은 보상을 폐기하고 종료 (2026-07-09
		// 운영자 확정 "부족하면 추가, 배부르면 그만").
		// 평가창 지연: 직전 펄스 요동이 정착한 뒤에 평가한다 — 감쇠 꼬리 위의 last5
		// 는 부풀어 만복을 배고픔으로 오판 (7-09 17:00 경계 오발사 근본 대책).
		settleWaitMs, settled := 0, false
		if w.compensationGateApplies(c) {
			var res gapResult
			res, settleWaitMs, settled = w.waitCompensationSettle(c)
			switch res {
			case gapOperatorStopped:
				c.complete(ReasonOperatorStop)
				w.finalizeCycle(ctx, c)
				log.Info("cycle stopped by operator during compensation settle wait")
				return
			case gapSiloCrossed:
				c.complete(ReasonSiloThreshold)
				w.finalizeCycle(ctx, c)
				log.Info("cycle complete: silo threshold during compensation settle wait")
				return
			case gapElapsed:
			}
		}
		if sat := w.compensationSatiated(c); sat.satiated {
			payload := map[string]any{
				"cycle_id":       c.CycleID,
				"tank_id":        c.TankID,
				"reason":         "satiation_gap_end_recovery",
				"gate_mode":      sat.mode,
				"settle_wait_ms": settleWaitMs,
				"settled":        settled,
				"skipped_at":     common.FormatTime(common.NowUTC()),
			}
			if sat.mode == "peak" {
				payload["peak_norm"] = sat.peakNorm
				payload["peak_threshold"] = sat.peakThreshold
				payload["rise_p90"] = sat.riseP90
			} else {
				payload["last5_mean"] = sat.last5Mean
				payload["baseline"] = sat.baseline
			}
			w.emitEvent(ctx, "feed.compensation.skipped", c.CycleID, c.TankID, payload)
			c.complete(ReasonSatiationOrTarget)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: compensation skipped — satiation",
				"gate_mode", sat.mode, "peak_norm", sat.peakNorm, "peak_threshold", sat.peakThreshold,
				"last5_mean", sat.last5Mean, "baseline", sat.baseline,
				"settle_wait_ms", settleWaitMs, "settled", settled)
			return
		}
		if settleWaitMs > 0 {
			log.Info("compensation gate: settle wait before pass",
				"settle_wait_ms", settleWaitMs, "settled", settled)
		}

		// --- pulse_active ---
		pulseID := common.NewID("pulse")
		pulseDurationMs := c.effectivePulseDurationMs(w.defaultPulseDurationMs())
		// 펄스별 공급시간 override (fixed 모드 램프 실험 — 뒤 펄스 배출 끊김 대응).
		if d := c.pulseDurationOverrideMs(c.PulsesExecuted()); d > 0 {
			pulseDurationMs = d
		}
		// 마지막 펄스 트림 — 남은 목표량이 한 펄스 배출량보다 적으면 공급시간을 잔여량 기준으로
		// 축소. 구조적 과급(펄스 올림 +17%) 방지 → 오차를 펄스 1개(≈28g)에서 잔여량 근사로 축소.
		// (onoff_feeder 1req/s 로 최소 1000ms 바닥은 아래 clamp 가 유지 — 이 경우만 소폭 초과.)
		if rateGPS := w.feederRateGPS(); rateGPS > 0 {
			if rem := c.remainingToTargetG(); rem > 0 {
				fullBurstG := float64(pulseDurationMs) / 1000.0 * rateGPS
				if rem < fullBurstG {
					trimmedMs := int(math.Round(rem / rateGPS * 1000.0))
					if trimmedMs > 0 && trimmedMs < pulseDurationMs {
						pulseDurationMs = trimmedMs
					}
				}
			}
		}
		// onoff_feeder: run/stop 이 ≥1s 간격이어야 함(1req/s). 펄스 지속 최소 1000ms clamp.
		// ⚠ 2026-07-07 13:00 실사고: 1s 공급에서 기기가 stop 을 무시하고 모터 4분 연속
		// 회전(폭주). 일시적으로 하한을 2000ms 로 올렸다가, 펄스당 확인 stop(+5s) 안전망
		// 가동 후 운영자 지시로 1s 재실험을 위해 1000ms 로 복원 (폭주 시 ~5s 내 차단).
		if c.IsOnoffFeeder() && pulseDurationMs < 1000 {
			pulseDurationMs = 1000
		}
		// onoff_feeder 공급모터 리드타임 — 살포모터가 먼저 돌고 공급모터는 spinup초 후에 걸린다
		// (실측 2026-07-04: run 후 +11~13s 낙하 시작). ON 시간 = spinup + 공급시간.
		// 배출량 추정(estimatePerPulse)은 공급시간(pulseDurationMs)만 사용.
		// 리드값: 대시보드 그룹정책(cycle 에 주입) > config > 기본 10s.
		onDurationMs := pulseDurationMs
		if c.IsOnoffFeeder() {
			spinup := c.FeederSpinupSec()
			if spinup <= 0 {
				spinup = w.feederSpinupSec()
			}
			onDurationMs += spinup * 1000
		}
		c.transitionTo(StatePulseActive)

		pulseStartedAt := common.NowUTC()
		w.emitEvent(ctx, "feed.pulse.started", c.CycleID, c.TankID, map[string]any{
			"cycle_id":   c.CycleID,
			"pulse_id":   pulseID,
			"pulse_no":   c.PulsesExecuted() + 1,
			"tank_id":    c.TankID,
			"started_at": common.FormatTime(pulseStartedAt),
		})

		// 펄스 발행: onoff 급이기면 feeder control run, 아니면 ESP32 feed.dispense(폴링).
		runAckAt := pulseStartedAt
		if c.IsOnoffFeeder() {
			if err := w.feederRun(c); err != nil {
				// run 실패 → 즉시 stop 시도 + 사이클 안전 종료 (fail-safe)
				w.feederStop(c)
				c.complete(ReasonSafetyBlock)
				w.finalizeCycle(ctx, c)
				log.Error("onoff feeder run failed — cycle aborted (fail-safe)", "error", err)
				return
			}
			runAckAt = common.NowUTC() // 모터 회전 구간의 시작 — 여기서부터 stop ACK 까지 잰다
		} else {
			w.dispatchPulse(ctx, c, pulseDurationMs)
		}

		// 펄스 지속 시간 대기 (interruptible — stop/silo 시 즉시 각성).
		select {
		case <-c.StopCh():
			// 펄스 도중 중단 — pulse_complete 처리 후 operator_stop 으로 마감
		case <-c.SiloThresholdCh():
			// 펄스 도중 silo 임계 — pulse_complete 처리 후 다음 iter 에서 종료
		case <-time.After(time.Duration(onDurationMs) * time.Millisecond):
		}

		// onoff_feeder: 펄스 종료 → ⚠ 반드시 feeder stop (중단/타임아웃/에러 무관, 급이기 off 보장).
		abortAfterStop := false
		if c.IsOnoffFeeder() {
			abortAfterStop = sendFeederStop()
		}
		stopAckAt := common.NowUTC()

		// 초과 회전 — run ACK ~ stop ACK 실측 구간이 설계 ON 시간을 넘긴 몫.
		//
		// 2026-08-18 운영자 지시: "피드백까지 확인해서 급이량을 평가해야 한다."
		// 종전에는 설정된 공급시간(pulseDurationMs)만으로 g 을 적어서, 같은 날 29.5초
		// 돈 펄스와 13.2초 돈 펄스가 장부에 똑같이 28.2g 으로 남았다.
		//
		// 초과분을 **전부 배출 시간으로** 더한다. 초과는 펄스 끝(stop 지연)에서 생기는데
		// 그 시점엔 리드가 이미 끝나 사료가 나가는 중이기 때문이다. 이렇게 하면 리드가
		// 상태변수(콜드 25초/프라임 10초)라 값을 모르는 문제를 우회한다 — 정상 펄스는
		// 초과 0 이라 종전과 같은 값이 나오고, 이상 펄스만 실제에 가깝게 잡힌다.
		//
		// ⚠ stop 이 끝내 실패하면 stopAckAt 은 "포기한 시각"이라 실제 회전은 더 길 수 있다.
		// 이 값은 하한이다.
		overrunMs := 0
		if c.IsOnoffFeeder() {
			if d := int(stopAckAt.Sub(runAckAt).Milliseconds()) - onDurationMs; d > 0 {
				overrunMs = d
			}
		}
		if abortAfterStop {
			c.recordPulseResult(estimatePerPulse(c, pulseDurationMs+overrunMs, w.feederRateGPS()),
				estimatePerPulse(c, pulseDurationMs, w.feederRateGPS()))
			c.complete(ReasonSafetyBlock)
			w.finalizeCycle(ctx, c)
			log.Error("onoff feeder stop 연속 실패 — 사이클 안전정지 + critical 알림", "consecutive_failures", consecutiveStopFailures)
			return
		}

		// estimatedG: 펄스 1회당 추정. onoff_feeder=배출률×(공급시간+초과회전), ESP32=1g/s stub.
		// 평가는 초과 포함(장부·보고), 제어는 설계값(목표 도달·트림) — 2026-08-19 운영자.
		estimatedG := estimatePerPulse(c, pulseDurationMs+overrunMs, w.feederRateGPS())
		c.recordPulseResult(estimatedG, estimatePerPulse(c, pulseDurationMs, w.feederRateGPS()))
		if overrunMs >= feederOverrunWarnMs {
			log.Warn("급이기 초과 회전 — 설계보다 오래 돌았다",
				"pulse_no", c.PulsesExecuted(), "overrun_ms", overrunMs,
				"design_on_ms", onDurationMs, "estimated_g", estimatedG)
		}

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
			// 모터 회전 실측 (2026-08-18) — 종전엔 로그에만 있어 사후 추적이 어려웠다.
			// design_on_ms 는 설계 ON(리드+공급), measured_on_ms 는 run ACK~stop ACK.
			"design_on_ms":   onDurationMs,
			"measured_on_ms": int(stopAckAt.Sub(runAckAt).Milliseconds()),
			"overrun_ms":     overrunMs,
			"run_ack_at":     common.FormatTime(runAckAt),
			"stop_ack_at":    common.FormatTime(stopAckAt),
		})

		w.store.UpdateFeedCycleProgress(ctx, c.CycleID, c.PulsesExecuted(), c.TotalAmountG())

		// r3 섀도 배출판정 (2026-07-22) — 이 펄스의 반응창을 담는 탑뷰 판정클립을 별도
		// 캡처해 LRCN 으로 채점, feed.pulse.r3_shadow 로 적재만 한다 (급이 보상 결정 무변).
		// 비블로킹: gap 길이만큼 녹화 후 스스로 종료. 요동 판정과 r3 의 일치를 라이브 관찰.
		w.launchR3Shadow(c, c.PulsesExecuted(), c.effectiveGapMs(w.defaultGapMs()))

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

		// 마지막 펄스(target/max 도달) — 판정·보상 가능한 사이클이면 즉시 마무리 대신
		// 아래 trailing gap 으로 진행해 gap 끝 라이브 판정까지 수행한다 (2026-07-20
		// 운영자 결정: 마지막 펄스 무배출도 그 자리에서 즉시 보상 — 급이 타이밍 단축·
		// 반응형 급이 방향). 무배출(no) 확정 시 grantCompensation 의 총량 차감으로
		// 루프가 자연히 이어져 만복 게이트 → 보상 펄스의 기존 검증 경로를 그대로 탄다.
		// 종료는 루프 상단 target/max_pulses 체크가 담당 (종료 사유 종전과 동일).
		//
		// 판정 불가 조건(비fixed·보상 상한 소진 등)이면 종전대로 trailing gap 없이
		// 즉시 마무리 (신호 다이어트, 2026-07-06). 정지 확인(confirm stop)은 2초 뒤
		// finalizeCycle 의 feederStopFinal 1발이 담당: 신호 열이 run→stop, …,
		// run→stop, (+2s) confirm stop 으로 끝난다.
		//
		// 보상이 차단된 상태(자율모드 off)에서는 trailing gap 을 돌되 판정만 하고
		// 그 자리에서 끝낸다 — 판정 no 의 차감(deductPhantomOnly)이 totalAmountG 를
		// 낮춰 isTargetReached() 가 풀리면, 보상 승인 없이도 루프가 다시 열려 펄스가
		// 한 발 더 나갈 수 있다 (2026-08-14). 종료 사유는 gap 前 값으로 고정한다.
		finalJudgeOnly := false
		finalReason := ReasonMaxPulses
		if c.IsOnoffFeeder() && (c.isTargetReached() || c.PulsesExecuted() >= c.maxPulses()) {
			if c.isTargetReached() {
				finalReason = ReasonSatiationOrTarget
			}
			if !w.lastPulseJudgeEligible(c) {
				if c.FeederLive() {
					select {
					case <-c.StopCh():
					case <-time.After(2 * time.Second):
					}
				}
				c.complete(finalReason)
				w.finalizeCycle(ctx, c)
				log.Info("cycle complete: final pulse + confirm stop", "reason", string(finalReason))
				return
			}
			finalJudgeOnly = !c.compensationEnabled()
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

		// 펄스 확인 stop — gap 앞 5초 뒤 1발 재전송(멱등). 기기가 공급모터 체결 직후
		// 도착한 stop 을 무시하는 결함 대응 (2026-07-07 13:00 실사고: stop 3발 전부
		// ACK 됐는데 모터 4분 연속 회전 — 늦게 보낸 정지는 수용됨). gap 90s 기준 다음
		// run 까지 85s 여유라 지연 배달 사살(구 15s gap 문제) 우려 없음. 짧은 gap(<20s)
		// 은 기존 단발 stop 유지.
		reinforceMs := 0
		if c.IsOnoffFeeder() && gapMs >= 20000 {
			reinforceMs = 5000
		}
		if reinforceMs > 0 {
			switch w.observeGap(c, reinforceMs) {
			case gapOperatorStopped:
				c.complete(ReasonOperatorStop)
				w.finalizeCycle(ctx, c)
				log.Info("cycle stopped by operator during gap")
				return
			case gapSiloCrossed:
				c.complete(ReasonSiloThreshold)
				w.finalizeCycle(ctx, c)
				log.Info("cycle complete: silo threshold during gap")
				return
			case gapElapsed:
			}
			if sendFeederStop() {
				c.complete(ReasonSafetyBlock)
				w.finalizeCycle(ctx, c)
				log.Error("onoff feeder 확인 stop 연속 실패 — 사이클 안전정지 + critical 알림")
				return
			}
		}

		switch w.observeGap(c, gapMs-reinforceMs) {
		case gapOperatorStopped:
			c.complete(ReasonOperatorStop)
			w.finalizeCycle(ctx, c)
			log.Info("cycle stopped by operator during gap")
			return
		case gapSiloCrossed:
			c.complete(ReasonSiloThreshold)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: silo threshold during gap")
			return
		case gapElapsed:
		}

		w.emitEvent(ctx, "feed.gap.completed", c.CycleID, c.TankID, map[string]any{
			"cycle_id":     c.CycleID,
			"tank_id":      c.TankID,
			"gap_ms":       gapMs,
			"completed_at": common.FormatTime(common.NowUTC()),
		})

		// 보상 펄스 판정 — gap 끝에서 직전 펄스의 라이브 무배출 판정. "no" 확정 시
		// 허깨비 추정량 차감 + 펄스 허용치 +1 (상한 config compensation_max_per_cycle).
		w.maybeCompensatePulse(ctx, c, pulseStartedAt, estimatedG)

		// 판정 전용 trailing gap (보상 off) — 기록만 남기고 종료. 추가 펄스 금지.
		if finalJudgeOnly {
			c.complete(finalReason)
			w.finalizeCycle(ctx, c)
			log.Info("cycle complete: 마지막 펄스 판정만 수행 (보상 off)",
				"reason", string(finalReason))
			return
		}

		// 다음 반복 (pulse_active 로 돌아감)
		c.transitionTo(StatePulseActive)
	}
}

// gapResult is the outcome of observeGap.
type gapResult int

const (
	gapElapsed         gapResult = iota // gap 정상 경과
	gapOperatorStopped                  // operator stop
	gapSiloCrossed                      // silo 임계
)

// observeGap waits out the inter-pulse gap, interruptible by operator stop /
// silo threshold. gap 중 stop 재전송은 하지 않는다(신호 다이어트, 2026-07-06) —
// 종전 재전송은 외부 장비 다운링크 큐잉 시 stop 이 다음 run 뒤에 배달돼 펄스를
// 사살하는 경로였다(2026-07-05 tank_02 "첫 펄스만 배출" + 외부 장비 "요청 과다" 회신).
// 드롭된 stop 은 다음 펄스의 stop 또는 사이클 마무리 confirm stop 이 흡수한다.
func (w *Worker) observeGap(c *Cycle, gapMs int) gapResult {
	gapTimer := time.NewTimer(time.Duration(gapMs) * time.Millisecond)
	defer gapTimer.Stop()

	for {
		select {
		case <-c.StopCh():
			return gapOperatorStopped
		case <-c.SiloThresholdCh():
			return gapSiloCrossed
		case <-gapTimer.C:
			return gapElapsed
		}
	}
}

// compensationGateApplies — 만복 게이트 적용 대상인지: fixed 사이클에서 보상이
// 승인돼 다음 펄스가 원 계획을 넘어서는(보상) 펄스일 때.
func (w *Worker) compensationGateApplies(c *Cycle) bool {
	return w.liveSeriesFn != nil && c.Mode == ModeFixed && c.fixed != nil &&
		c.CompensationsGranted() > 0 && c.PulsesExecuted() >= c.fixed.TotalPulses
}

// waitCompensationSettle — 만복 게이트 평가창 지연: 직전 펄스 요동이 정착할 때까지
// compensationSettlePollMs 단위로 폴링 (상한 compensationSettleMaxWaitMs, operator
// stop/silo 임계에 즉시 각성). 반환: (중단 사유, 대기 ms, 정착 여부). 시리즈 부족
// 등 판정 불가면 대기 없이 진행한다 (게이트도 어차피 작동 못 함).
func (w *Worker) waitCompensationSettle(c *Cycle) (gapResult, int, bool) {
	waited := 0
	for {
		settled, ok := w.compensationAgitationSettled(c)
		if !ok {
			return gapElapsed, waited, false
		}
		if settled {
			return gapElapsed, waited, true
		}
		if waited >= compensationSettleMaxWaitMs {
			return gapElapsed, waited, false
		}
		if res := w.observeGap(c, compensationSettlePollMs); res != gapElapsed {
			return res, waited, false
		}
		waited += compensationSettlePollMs
	}
}

// compensationAgitationSettled — 직전 펄스 요동 정착 여부: 최근 5s 평균(last5)과
// 그 직전 5s 평균의 차가 기저×compensationSettleToleranceFactor 이내면 정착
// (감쇠/상승 전이 구간이 아님). ok=false 는 시리즈 부족으로 판정 불가.
func (w *Worker) compensationAgitationSettled(c *Cycle) (settled, ok bool) {
	series, _, ok := w.liveSeriesFn(c.CycleID)
	if !ok {
		return false, false
	}
	m := vision_pipeline.ComputeAgitationMetrics(series, 8)
	if m == nil || m.Baseline <= 0 || len(m.Series) < 10 {
		return false, false
	}
	sm := m.Series
	prev5 := meanOf(sm[len(sm)-10 : len(sm)-5])
	return math.Abs(m.Last5Mean-prev5) <= m.Baseline*compensationSettleToleranceFactor, true
}

// satiationResult — 보상 만복 게이트 판정 + 로그용 상세.
type satiationResult struct {
	satiated bool
	mode     string // "peak"(사이클 피크) | "gap_end"(legacy 복귀도) | ""(판정 불가)
	// peak 모드
	peakNorm      float64
	peakThreshold float64
	riseP90       float64
	// gap_end 모드
	last5Mean float64
	baseline  float64
}

// compensationSatiated — 다음 펄스가 보상 펄스(원 계획 초과)일 때 만복 판정.
//
// CompensationSatiationPeakThreshold>0 이고 카메라 P90 확보 시: 사이클 요동 피크를
// 카메라 자기 진폭(P90)으로 정규화한 값이 임계 미만이면 저활동=만복 (2026-07-20,
// 저진폭 카메라 gap끝 판별 불가 대응). 미설정/P90 미확보 시: 종전 gap끝 복귀도 게이트.
func (w *Worker) compensationSatiated(c *Cycle) satiationResult {
	if !w.compensationGateApplies(c) {
		return satiationResult{}
	}
	// 만복 전제조건 — 맥락 게이트(위 maybeCompensatePulse)와 같은 조건·같은 설정값.
	//
	// 이 게이트는 "요동이 낮다 = 어체가 배부르다"로 읽는다. 그런데 급이기가 안 돌면
	// 사료가 없어서 요동이 낮은 것이라 전제가 깨진다. 8-02 에 맥락 게이트에만
	// K 조건을 넣고 이쪽을 빠뜨려, 같은 사이클에서 두 게이트가 모순된 결론을 냈다 —
	// 8-03 10:00 tank_02(6발 중 배출 1발)에서 맥락 게이트는
	// gate_skipped=satiation_not_possible 로 "만복일 리 없다"고 판단했는데, 이 게이트는
	// peak_norm 0.14 < 0.30 을 보고 만복으로 단정해 **이미 승인된 보상 2발을 취소하고
	// 사이클을 끝냈다**(37.6g, 목표의 절반). 급이기 고장이 만복으로 기록된 것이다.
	//
	// 판정 불가(satiationResult{})로 반환 = 게이트 미적용 → 보상 진행. 노출은 사이클당
	// 보상 상한(compensation_max_per_cycle)으로 유계다. 7-25 이후 실측 스킵 1건이
	// 이 조건으로 뒤집힌다 — 바로 위 8-03 사례.
	if c.ConfirmedDischarges() < w.cfg.CompensationGateMinConfirmed {
		return satiationResult{}
	}
	series, _, ok := w.liveSeriesFn(c.CycleID)
	if !ok {
		return satiationResult{}
	}
	m := vision_pipeline.ComputeAgitationMetrics(series, 8)
	if m == nil || m.Baseline <= 0 {
		return satiationResult{}
	}
	if th := w.cfg.CompensationSatiationPeakThreshold; th > 0 {
		if p90, okP := w.tankTopviewRiseP90(c.TankID); okP {
			norm := vision_pipeline.PeakRiseFrac(m.Baseline, m.Peak) / p90
			if norm > 1 {
				norm = 1
			}
			return satiationResult{
				satiated: norm < th, mode: "peak",
				peakNorm: norm, peakThreshold: th, riseP90: p90,
			}
		}
	}
	return satiationResult{
		satiated: m.Last5Mean <= m.Baseline*compensationSatiationFactor, mode: "gap_end",
		last5Mean: m.Last5Mean, baseline: m.Baseline,
	}
}

// lastPulseJudgeEligible — 마지막 펄스 뒤 trailing gap 판정이 가능한 조건.
//
// ⚠ 2026-08-01: 종전에는 "보상 상한 잔여"를 조건에 넣어, 보상을 다 쓴 사이클은
// trailing gap 을 건너뛰고 마지막 펄스를 **판정조차 하지 않았다**. 그런데 실측에서
// 그렇게 판정을 건너뛴 마지막 펄스 2건이 **둘 다 실제 미배출**이었다(운영자 육안,
// 게이트 가동 후 94펄스 표본). 즉 가장 놓치면 안 되는 자리에 기록이 아예 없었다.
//
// 판정과 보상을 분리한다 — 보상을 못 주더라도 판정은 남긴다. 그 기록이
// ① 운영자 라벨의 대상이 되고 ② 결식 집계에 잡히며 ③ 보상 상한을 푸는 판단의
// 입력이 된다. 대가는 해당 사이클에 trailing gap(≈1분) 지연 하나뿐이다.
//
// ⚠ 2026-08-14: 자율모드 off(보상 차단) 도 같은 이유로 조건에서 뺐다. 종전에는
// off 인 동안 사이클마다 마지막 펄스의 판정 기록이 통째로 빠져, 판정 창 검증의
// 표본과 운영자 정답지에서 매번 한 발씩 사라졌다. 보상 발사 여부는 판정 이후
// maybeCompensatePulse 안에서 따로 걸린다 — 여기는 "판정할 수 있는가"만 본다.
// off 일 때의 trailing gap 은 판정 전용이며, 판정 직후 그 자리에서 사이클을
// 끝낸다 (runPulseLoop 의 finalJudgeOnly — 차감으로 target 이 풀려 추가 펄스가
// 나가는 것을 막는다).
func (w *Worker) lastPulseJudgeEligible(c *Cycle) bool {
	return w.liveSeriesFn != nil && c.Mode == ModeFixed && c.fixed != nil &&
		c.IsOnoffFeeder() && w.cfg.CompensationMaxPerCycle > 0
}

// maybeCompensatePulse — gap 끝에서 직전 펄스의 라이브 무배출 판정 + 보상 승인.
// 판정 결과는 feed.pulse.discharge_judged 이벤트로 항상 남긴다 (unknown 제외).
// 보상은 onoff_feeder fixed 사이클 + config compensation_max_per_cycle > 0 일 때만.
func (w *Worker) maybeCompensatePulse(ctx context.Context, c *Cycle, pulseRunAt time.Time, estimatedG float64) {
	if w.liveSeriesFn == nil || c.Mode != ModeFixed || !c.IsOnoffFeeder() {
		return
	}
	maxComp := w.cfg.CompensationMaxPerCycle
	if maxComp <= 0 {
		return
	}
	series, startedAt, ok := w.liveSeriesFn(c.CycleID)
	if !ok {
		return
	}
	// 1차 — 국소 상승폭 (종전 판정). 사이클 후반 만복 구간에서 정상 배출도 no 로
	// 떨어뜨리는 한계가 있다 (2026-07-29 육안 대조: no 비율 p1 10% → p7 90%).
	rawVerdict, evidence := judgeLivePulseDischarge(series, startedAt, pulseRunAt)
	if rawVerdict == "unknown" {
		return
	}
	// 2차 — 차트 전체 맥락 (2026-07-30 운영자 지시: "차트의 값을 가지고 각각의 펄스를
	// 판단"). 상승폭이 no 여도 사이클 초반 대비 수준이 유지되거나 반응창에 변동이
	// 남아 있으면 배출된 것으로 보고 unclear 로 낮춘다. 판정 불가면 no 유지(fail-open).
	verdict := rawVerdict
	gate := compGateResult{}
	// 만복 전제조건 (2026-08-02) — 이 사이클에서 배출이 아직 충분히 확인되지 않았으면
	// 어체가 배부를 수 없으므로 게이트를 걸지 않는다. 급이기 무배출이 잦아지면서
	// "후반 no = 만복" 전제가 깨졌다 (8-02 tank_02: 배출 3발뿐인 사이클에서 게이트가
	// 3건을 잘못 완화 → 결식 + 급이량 47g 과대계상).
	satiationPossible := c.ConfirmedDischarges() >= w.cfg.CompensationGateMinConfirmed
	// rise 음수는 만복 완화 대상에서 제외한다 (2026-08-16 운영자 결정).
	// 음수 = 펄스 후 반응창의 **최댓값**이 펄스 직전 평균보다도 낮다는 뜻이라, 사료가
	// 물에 닿았다면 나오기 어려운 모양이다. 실측으로 확인된 미전달 3건(8-14 t01 p4
	// −0.42 · t02 p5 −0.15 운영자 라벨 feed_not_delivered, 8-15 t02 p5 −0.38 직후 펄스
	// 4.20 즉시 복귀)이 모두 음수였고, 반대로 양수 저반응은 만복으로 확인된 사례가 있다.
	// σ 비례 임계와 달리 부호는 자가 없어 사이클 σ 오염에 흔들리지 않는다.
	//
	// ⚠ 이 규칙은 게이트보다 "덜 깎는" 쪽이 아니라 **더 깎는** 쪽이다 — 8-16 14:40 t02 p8
	// (rise −0.17, 마지막 트림 펄스)은 종전 규칙에서 만복으로 완화됐으나 이제 no 로 남는다.
	// ⚠ 스핀업창 극단 포화 가드(judgeLivePulseDischarge 의 ×2.0)는 그대로 둔다 — 그쪽은
	// "배부르다"가 아니라 "측정을 믿을 수 없다"는 판단이라 부호로 뒤집을 근거가 없다.
	negativeRise := evidence.Valid && evidence.Rise < 0
	if shouldApplySatiationGate(rawVerdict, w.cfg.CompensationGateEnabled, satiationPossible, negativeRise) {
		gate = compensationGate(series, startedAt, pulseRunAt, c.PulsesExecuted(),
			w.cfg.CompensationGateRelMax, w.cfg.CompensationGateVarMax)
		if gate.Checked && !gate.Pass {
			verdict = "unclear"
			w.log.Info("discharge verdict softened by context — 만복 추정(거짓 no)",
				"cycle_id", c.CycleID, "tank_id", c.TankID,
				"pulse_no", c.PulsesExecuted(), "rel", gate.Rel, "var_ratio", gate.VarRatio,
				"confirmed_discharges", c.ConfirmedDischarges())
		}
	}
	if verdict == "yes" {
		c.recordConfirmedDischarge()
	}
	// 보상·차감 제외 (2026-08-18 운영자 결정). 펄스 기동을 3초로 올린 뒤로 사료
	// 미전달은 실질적으로 막혔다고 본다. 그러면 저반응의 뜻이 뒤집힌다:
	//
	//	종전   저반응 = 사료가 안 나갔다  → 보상 펄스로 더 준다 · 장부에서 차감
	//	이제   저반응 = 사료는 나갔고 안 먹는다 → 더 주면 안 된다 · 장부도 사실 그대로
	//
	// 종전 로직은 방향이 정반대였다 — 식욕이 떨어진 수조에 사료를 더 주고, 실제로
	// 나간 사료를 장부에서 뺐다. 판정(verdict)과 그 근거는 그대로 남긴다. 반응이
	// 떨어질 때의 조치는 보상이 아니라 **중단**이고, 그 규칙은 섀도 검증 중이다
	// (shadow_stop.go 의 absolute_level_v1).
	//
	// 되돌리려면 이 커밋을 revert 한다. compensation_max_per_cycle 은 건드리지 않았다
	// — 0 으로 두면 maybeCompensatePulse 가 조기 반환해 **판정 이벤트까지 사라진다**.
	granted := false
	deductedG := 0.0
	payload := map[string]any{
		"cycle_id":             c.CycleID,
		"tank_id":              c.TankID,
		"pulse_no":             c.PulsesExecuted(),
		"verdict":              verdict, // yes | unclear | no (맥락 반영 최종 판정)
		"compensation_granted": granted,
		"deducted_g":           deductedG,
		"autonomous_mode":      c.autonomousMode, // off 면 verdict=no 여도 미발사 (방향 A)
		"judged_at":            common.FormatTime(common.NowUTC()),
	}
	if evidence.Valid {
		// 판정 근거 (2026-08-14) — 이 수들이 없어 8-13 에 원인을 정황으로 추측했다.
		payload["rise"] = evidence.Rise
		payload["noise"] = evidence.Noise
		payload["threshold_yes"] = dischargeRiseYes * evidence.Noise
		payload["threshold_unclear"] = dischargeRiseUnsure * evidence.Noise
		payload["pre_mean"] = evidence.PreMean
		payload["baseline"] = evidence.Baseline
		payload["post_window"] = fmt.Sprintf("%d-%ds", dischargePostStartS, dischargePostEndS)
	}
	if gate.Checked {
		// 사후 분석 연속성 — 상승폭 원본과 맥락 지표를 함께 남긴다.
		payload["raw_verdict"] = rawVerdict
		payload["gate_pass"] = gate.Pass
		payload["gate_rel"] = gate.Rel
		payload["gate_var_ratio"] = gate.VarRatio
	}
	if rawVerdict == "no" && w.cfg.CompensationGateEnabled && !satiationPossible {
		// 만복 전제 미충족으로 게이트를 건너뛴 건 — 왜 완화되지 않았는지 추적용.
		payload["gate_skipped"] = "satiation_not_possible"
		payload["confirmed_discharges"] = c.ConfirmedDischarges()
	} else if rawVerdict == "no" && w.cfg.CompensationGateEnabled && negativeRise {
		// 음수 우선으로 게이트를 건너뛴 건 — 같은 이유로 추적 가능하게 남긴다.
		payload["gate_skipped"] = "negative_rise"
	}
	w.emitEvent(ctx, "feed.pulse.discharge_judged", c.CycleID, c.TankID, payload)
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
	// ⚠ onoff 급이기: 모든 종료 사유에 대해 방어적으로 마무리 stop (idempotent).
	// 급이기가 켜진 채 사이클이 끝나는 일을 원천 차단. 실패 시 재시도 후 critical 알림.
	if c.IsOnoffFeeder() {
		w.feederStopFinal(ctx, c)
	}

	// 비전 D-3 — 신호게이팅 급이 캡처 종료. 사이클 deregister 전에 호출해야
	// clip evidence 보강(feed_target/total)이 active 사이클을 찾는다. clip 이 clean EOS 될
	// 때까지 잠깐(~수초) 블록.
	// completedAt 은 캡처 종료 전에 확정 — 관찰 꼬리를 급이 duration 에 포함하지 않는다.
	completedAt := common.NowUTC()
	if w.captureWorker != nil && w.captureWorker.Enabled() {
		// 관찰 꼬리 (2026-07-06 운영자 결정) — 급이 신호는 이미 끝났고 녹화만 유지.
		// 마지막 공급 후 먹이활동 지속(침전 사료 섭취·탐색)과 진정 도달을 클립에 담는다
		// (calm_reached 실측화). operator stop 은 StopCh 로 즉시 중단된다.
		if tail := w.captureWorker.ObservationTailSec(); tail > 0 {
			select {
			case <-c.StopCh():
			case <-time.After(time.Duration(tail) * time.Second):
			}
		}
		w.captureWorker.StopCycleRecording(ctx, c.CycleID)
	}

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

	// safety_block 연속 에스컬레이션 (2026-07-16) — 2회 연속이면 급이 일시정지 + critical.
	// CompleteFeedCycle 이후여야 이번 사이클이 streak 집계에 포함된다. goroutine — 비차단.
	if c.TerminationReason() == ReasonSafetyBlock {
		go w.escalateConsecutiveSafetyBlock(context.Background(), c.TankID, c.CycleID)
	}

	// #2 — 사료투입 기록(feeding.recorded) + 재고 차감. 총 공급량>0 일 때만.
	w.recordFeedingAndConsume(ctx, c)

	w.deregister(c.CycleID)

	// Phase 1c — AI scheduler hook. operating_mode='auto' 면 다음 cycle 자동 등록.
	// 별도 goroutine — finalize 차단 X. arbiter / DB 조회 비용 격리.
	if w.aiScheduler != nil {
		go w.aiScheduler.OnCycleComplete(context.Background(), c.TankID, completedAt)
	}

	// 사이클 완료 자동 보고 — 비전 결과(LRCN·요동곡선, 비동기 적재)를 폴링 대기 후
	// feed.cycle.report 1건 적재. 별도 goroutine — finalize 차단 X.
	if w.reporter != nil {
		go w.reporter.OnCycleComplete(context.Background(), c.CycleID)
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
func estimatePerPulse(c *Cycle, pulseDurationMs int, rateGPS float64) float64 {
	if c.IsOnoffFeeder() {
		// onoff_feeder: 배출률(g/s) × 운전시간
		return rateGPS * float64(pulseDurationMs) / 1000.0
	}
	// ESP32: 1g/sec 단순 모델 (Phase 3 stub)
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
