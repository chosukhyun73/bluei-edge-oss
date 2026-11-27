package feed_cycle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"bluei.kr/edge/internal/controller"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// --- mock storage -----------------------------------------------------------

type mockStore struct {
	storage.Store
	inserted  []*storage.FeedCycle
	completed []string
	alerts    []*storage.OpenAlert
}

func (m *mockStore) GetTankAutonomousMode(_ context.Context, tankID string) (*storage.TankAutonomousMode, error) {
	return &storage.TankAutonomousMode{TankID: tankID, Mode: "observation"}, nil
}

func (m *mockStore) InsertFeedCycle(_ context.Context, c *storage.FeedCycle) error {
	m.inserted = append(m.inserted, c)
	return nil
}

func (m *mockStore) UpdateFeedCycleProgress(_ context.Context, _ string, _ int, _ float64) error {
	return nil
}

func (m *mockStore) CompleteFeedCycle(_ context.Context, cycleID string, _ int, _ float64, _ string, _ time.Time) error {
	m.completed = append(m.completed, cycleID)
	return nil
}

func (m *mockStore) UpdateFeedCycleActualTotal(_ context.Context, _ string, _ float64) error {
	return nil
}

func (m *mockStore) SetFeedCycleSiloDepletionWarned(_ context.Context, _ string) error {
	return nil
}

// safety_block 에스컬레이션 조회 — 기본 빈 목록(streak 0 → 에스컬레이션 없음).
func (m *mockStore) ListRecentFeedCycles(_ context.Context, _ string, _ int) ([]*storage.FeedCycle, error) {
	return nil, nil
}

// onoff 급이기 해석용 — 컨트롤러 없음(기본 테스트: ESP32/추정 경로).
func (m *mockStore) GetController(_ context.Context, _ string) (*controller.Controller, error) {
	return nil, nil
}

func (m *mockStore) ListControllers(_ context.Context, _ string) ([]*controller.Controller, error) {
	return nil, nil
}

// #2 사료 기록/차감 — 재고 없음(기록만).
func (m *mockStore) ListInventoryItems(_ context.Context, _ string) ([]*storage.InventoryItem, error) {
	return nil, nil
}

func (m *mockStore) AdjustInventoryOnHand(_ context.Context, _ string, _ float64) (float64, error) {
	return 0, nil
}

// Phase A — silo depletion / stop 실패 alert raise 가 호출. 테스트는 raise 여부 확인.
func (m *mockStore) GetOpenAlert(_ context.Context, _ string) (*storage.OpenAlert, error) {
	return nil, nil
}

func (m *mockStore) UpsertAlert(_ context.Context, a *storage.OpenAlert) (bool, error) {
	m.alerts = append(m.alerts, a)
	return true, nil
}

func (m *mockStore) ClearAlert(_ context.Context, _ string) error {
	return nil
}

// --- helpers ----------------------------------------------------------------

func newTestWorker(t *testing.T, store *mockStore) *feed_cycle.Worker {
	t.Helper()
	app := &runtime.App{}
	return feed_cycle.NewWorker(app, store, nil, feed_cycle.TestConfig(), nil)
}

// --- tests ------------------------------------------------------------------

// TestAdaptiveCycle_MaxPulses verifies that the state machine terminates with
// reason "max_pulses" when MaxPulses is reached.
// estimatePerPulse = pulseDurationMs/1000 g = 1ms/1000 = 0.001g per pulse.
// 2 pulses * 0.001g = 0.002g << TargetAmountG=9999 → target 절대 미달 → max_pulses 종료.
func TestAdaptiveCycle_MaxPulses(t *testing.T) {
	p := feed_cycle.AdaptiveParams{
		TargetAmountG:   9999,
		MaxPulses:       2,
		MaxDurationMin:  60,
		GapMs:           1,
		PulseDurationMs: 1, // 1ms → 0.001g/pulse
	}

	c := feed_cycle.NewAdaptiveCycle("test_cycle_01", "tank_01", "", p)
	if c.State() != feed_cycle.StateIdle {
		t.Fatalf("expected idle, got %s", c.State())
	}

	store := &mockStore{}
	ctx := context.Background()

	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)

	// StartAdaptiveCycle을 직접 호출하면 goroutine이 뜨므로
	// 대신 내부 run 결과를 짧은 wait로 관찰
	go w.RunCycle(c)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete within deadline, state=%s", c.State())
	}
	if c.TerminationReason() != feed_cycle.ReasonMaxPulses {
		t.Fatalf("expected max_pulses termination, got %s", c.TerminationReason())
	}
	if c.PulsesExecuted() != 2 {
		t.Fatalf("expected 2 pulses, got %d", c.PulsesExecuted())
	}
	_ = ctx
}

// TestAdaptiveCycle_TargetReached verifies satiation_or_target termination.
// estimatePerPulse = 1000ms/1000 = 1g per pulse.
// TargetAmountG=0.5 < 1g → 첫 펄스 직후 target 초과 → satiation_or_target 종료.
func TestAdaptiveCycle_TargetReached(t *testing.T) {
	p := feed_cycle.AdaptiveParams{
		TargetAmountG:   0.5, // 1g/pulse 추정 → 첫 펄스 후 초과
		MaxPulses:       100,
		MaxDurationMin:  60,
		GapMs:           1,
		PulseDurationMs: 1000, // 1000ms → estimatePerPulse=1.0g
	}
	c := feed_cycle.NewAdaptiveCycle("test_cycle_02", "tank_01", "", p)
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)

	go w.RunCycle(c)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete within deadline, state=%s", c.State())
	}
	if c.TerminationReason() != feed_cycle.ReasonSatiationOrTarget {
		t.Fatalf("expected satiation_or_target termination, got %s", c.TerminationReason())
	}
}

// TestAdaptiveCycle_OperatorStop verifies operator_stop termination.
func TestAdaptiveCycle_OperatorStop(t *testing.T) {
	p := feed_cycle.AdaptiveParams{
		TargetAmountG:   9999,
		MaxPulses:       100,
		MaxDurationMin:  60,
		GapMs:           500, // 500ms gap → 충분히 길어서 중간에 stop 가능
		PulseDurationMs: 1,
	}
	c := feed_cycle.NewAdaptiveCycle("test_cycle_03", "tank_01", "", p)
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)

	go w.RunCycle(c)

	// 첫 펄스 완료 후 gap 중에 stop
	time.Sleep(50 * time.Millisecond)
	c.OperatorStop()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not stop within deadline, state=%s", c.State())
	}
	if c.TerminationReason() != feed_cycle.ReasonOperatorStop {
		t.Fatalf("expected operator_stop termination, got %s", c.TerminationReason())
	}
}

// TestAdaptiveCycle_SafetyBlock verifies safety_block termination via SafetyGate.
func TestAdaptiveCycle_SafetyBlock(t *testing.T) {
	p := feed_cycle.AdaptiveParams{
		TargetAmountG:   9999,
		MaxPulses:       100,
		MaxDurationMin:  60,
		GapMs:           1,
		PulseDurationMs: 1,
	}
	c := feed_cycle.NewAdaptiveCycle("test_cycle_04", "tank_01", "", p)
	store := &mockStore{}
	gate := &alwaysBlockGate{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, gate)

	go w.RunCycle(c)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not stop within deadline, state=%s", c.State())
	}
	if c.TerminationReason() != feed_cycle.ReasonSafetyBlock {
		t.Fatalf("expected safety_block termination, got %s", c.TerminationReason())
	}
}

// TestFixedCycle_HappyPath verifies that a fixed cycle runs total_pulses and terminates.
func TestFixedCycle_HappyPath(t *testing.T) {
	p := feed_cycle.FixedParams{
		PulseDurationMs: 1,
		GapMs:           1,
		TotalPulses:     3,
	}
	c := feed_cycle.NewFixedCycle("test_cycle_05", "tank_02", "", p)
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)

	go w.RunCycle(c)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete within deadline, state=%s", c.State())
	}
	if c.TerminationReason() != feed_cycle.ReasonMaxPulses {
		t.Fatalf("expected max_pulses termination, got %s", c.TerminationReason())
	}
	if c.PulsesExecuted() != 3 {
		t.Fatalf("expected 3 pulses, got %d", c.PulsesExecuted())
	}
}

// --- Phase 5 (load cell) tests ---------------------------------------------

// TestRecordWeight_DeltaAccumulates — 단일 weight reading 누적 + 음수 delta clamp.
func TestRecordWeight_DeltaAccumulates(t *testing.T) {
	p := feed_cycle.AdaptiveParams{TargetAmountG: 100, MaxPulses: 100, PulseDurationMs: 1000}
	c := feed_cycle.NewAdaptiveCycle("cycle_w1", "tank_01", "ctrl_w1", p)
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)
	w.RegisterCycleForTest(c)

	res, cyc := w.RecordWeight(context.Background(), "ctrl_w1", "p1", 100.0, 95.0, time.Now())
	if res == nil || cyc == nil {
		t.Fatalf("expected active cycle match")
	}
	if res.DeltaG != 5.0 {
		t.Errorf("delta: got %v want 5", res.DeltaG)
	}
	if res.ActualTotalAmountG != 5.0 {
		t.Errorf("total: got %v want 5", res.ActualTotalAmountG)
	}

	// 음수 delta (사료통 흔들림 등) — clamp to 0.
	res2, _ := w.RecordWeight(context.Background(), "ctrl_w1", "p2", 95.0, 96.0, time.Now())
	if res2.DeltaG != 0 {
		t.Errorf("clamp delta: got %v want 0", res2.DeltaG)
	}
	if res2.ActualTotalAmountG != 5.0 {
		t.Errorf("total unchanged: got %v want 5", res2.ActualTotalAmountG)
	}
}

// TestRecordWeight_SiloDepletion — 3 연속 pulse ≤1g 시 1회 감지 + idempotent.
func TestRecordWeight_SiloDepletion(t *testing.T) {
	p := feed_cycle.AdaptiveParams{TargetAmountG: 100, MaxPulses: 100, PulseDurationMs: 1000}
	c := feed_cycle.NewAdaptiveCycle("cycle_silo", "tank_01", "ctrl_silo", p)
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)
	w.RegisterCycleForTest(c)

	w.RecordWeight(context.Background(), "ctrl_silo", "p1", 100.0, 99.8, time.Now())           // 0.2g
	w.RecordWeight(context.Background(), "ctrl_silo", "p2", 99.8, 99.0, time.Now())            // 0.8g
	res3, _ := w.RecordWeight(context.Background(), "ctrl_silo", "p3", 99.0, 98.5, time.Now()) // 0.5g

	if !res3.SiloDepletionDetect {
		t.Errorf("expected silo depletion on 3rd ≤1g pulse")
	}
	if !c.SiloDepletionWarned() {
		t.Errorf("warned flag should be set after detection")
	}

	// idempotent — 다음 pulse 도 ≤1g 이지만 재발화 X.
	res4, _ := w.RecordWeight(context.Background(), "ctrl_silo", "p4", 98.5, 98.0, time.Now())
	if res4.SiloDepletionDetect {
		t.Errorf("subsequent detection must be false (idempotent)")
	}
}

// TestRecordWeight_Overflow — delta > expected×3 시 감지.
func TestRecordWeight_Overflow(t *testing.T) {
	p := feed_cycle.AdaptiveParams{TargetAmountG: 100, MaxPulses: 100, PulseDurationMs: 1000}
	c := feed_cycle.NewAdaptiveCycle("cycle_over", "tank_01", "ctrl_over", p)
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)
	w.RegisterCycleForTest(c)

	// pulseDuration=1000 → expected = 1.0g. delta=4g → > 3× → 감지.
	res, _ := w.RecordWeight(context.Background(), "ctrl_over", "p1", 100.0, 96.0, time.Now())
	if !res.OverflowDetect {
		t.Errorf("expected overflow on 4g vs 1g expected")
	}
	if res.ExpectedG != 1.0 {
		t.Errorf("expected expected=1.0, got %v", res.ExpectedG)
	}

	// delta=2g (2× expected) — 미감지.
	res2, _ := w.RecordWeight(context.Background(), "ctrl_over", "p2", 96.0, 94.0, time.Now())
	if res2.OverflowDetect {
		t.Errorf("delta 2g ≤ 3×expected should not overflow")
	}
}

// TestRecordWeight_NoActiveCycle — controller 에 active cycle 없으면 (nil, nil).
func TestRecordWeight_NoActiveCycle(t *testing.T) {
	store := &mockStore{}
	w := feed_cycle.NewWorkerForTest(nil, store, nil, nil)
	res, cyc := w.RecordWeight(context.Background(), "ctrl_orphan", "p1", 100.0, 95.0, time.Now())
	if res != nil || cyc != nil {
		t.Errorf("expected (nil, nil) for orphan weight, got res=%v cyc=%v", res, cyc)
	}
}

// --- helpers ----------------------------------------------------------------

type alwaysBlockGate struct{}

func (g *alwaysBlockGate) Check(_ string) (bool, string) { return true, "test_block" }

// TestOnoffFeederSignalDiet_OneStopPerPulse — onoff_feeder(live) 사이클 신호열(2026-07-06):
// 펄스마다 stop 1발(gap 재전송 없음), 마지막 펄스 뒤 confirm stop 1발로 마무리.
// 2펄스 → run 2 / stop 3 (펄스 stop 2 + confirm 1). trailing gap 없이 즉시 종료.
// TestFixedCycle_LastPulseTrim — fixed 사이클도 TargetAmountG 가 설정되면 마지막 펄스를
// 잔여량 기준으로 트림한다 (2026-07-07 일 사이클 분할 확대의 올림 과급 방지).
// target 50g, 펄스 28.2g(3s): p1 풀(3000ms) + p2 트림(잔여 21.8g ≈ 2319ms) → 총 ≈50g.
func TestFixedCycle_LastPulseTrim(t *testing.T) {
	store := &mockStore{}
	w := feed_cycle.NewWorker(nil, store, nil, feed_cycle.TestConfig(), nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, _ string) error { return nil })

	p := feed_cycle.FixedParams{
		PulseDurationMs: 3000,
		GapMs:           100,
		TotalPulses:     2,
		TargetAmountG:   50,
	}
	c := feed_cycle.NewFixedCycle("test_fixed_trim", "tank_01", "", p)
	c.SetOnoffFeeder(831, true)

	go w.RunCycle(c)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	// 트림 없으면 2×28.2=56.4g. 트림 시 목표 50g 에 근사 (onoff_feeder 1s 바닥 오차 허용).
	total := c.TotalAmountG()
	if total > 51 || total < 49 {
		t.Fatalf("expected trimmed total ≈50g, got %.1f", total)
	}
}

func TestOnoffFeederSignalDiet_OneStopPerPulse(t *testing.T) {
	var runs, stops int
	cfg := feed_cycle.TestConfig()
	cfg.FeederStopReinforceSec = 1
	store := &mockStore{}
	w := feed_cycle.NewWorker(nil, store, nil, cfg, nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, cmd string) error {
		switch cmd {
		case "feeder.run":
			runs++
		case "feeder.stop":
			stops++
		}
		return nil // 모두 성공
	})

	p := feed_cycle.AdaptiveParams{
		TargetAmountG:   28,  // p1 18.8g(2s) + p2 트림(9.2g→1s 바닥 9.4g) → 2펄스째 도달
		MaxPulses:       100, // target 이 먼저 종료
		MaxDurationMin:  60,
		GapMs:           1500,
		PulseDurationMs: 2000,
	}
	c := feed_cycle.NewAdaptiveCycle("test_signal_diet", "tank_02", "", p)
	c.SetOnoffFeeder(831, true) // live

	go w.RunCycle(c)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	if runs != 2 {
		t.Fatalf("expected 2 run pulses, got %d", runs)
	}
	// 펄스 stop 2 + 마무리 confirm stop 1 — 그 이상(gap 재전송)이 없어야 한다.
	if stops != 3 {
		t.Fatalf("expected exactly 3 stops (2 pulse + 1 confirm) for 2 pulses, got %d", stops)
	}
}

// TestOnoffFeederStop_ConsecutiveFailure_AbortsAndAlerts — stop 이 연속 실패하면
// 사이클을 안전정지(ReasonSafetyBlock)하고 critical 알림을 올린다.
func TestOnoffFeederStop_ConsecutiveFailure_AbortsAndAlerts(t *testing.T) {
	cfg := feed_cycle.TestConfig()
	cfg.FeederStopReinforceSec = 1
	store := &mockStore{}
	w := feed_cycle.NewWorker(nil, store, nil, cfg, nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, cmd string) error {
		if cmd == "feeder.stop" {
			return errors.New("onoff_feeder code 429: 1초에 1회만 허용됩니다")
		}
		return nil // run 은 성공
	})

	p := feed_cycle.AdaptiveParams{
		TargetAmountG:   9999, // target 으로 종료되지 않게
		MaxPulses:       100,
		MaxDurationMin:  60,
		GapMs:           1500, // 펄스 2개의 종료 stop 이 연속 실패 → 임계 도달
		PulseDurationMs: 1000,
	}
	c := feed_cycle.NewAdaptiveCycle("test_stopfail", "tank_02", "", p)
	c.SetOnoffFeeder(831, true)

	go w.RunCycle(c)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	if c.TerminationReason() != feed_cycle.ReasonSafetyBlock {
		t.Fatalf("expected safety_block termination, got %s", c.TerminationReason())
	}
	if len(store.alerts) == 0 {
		t.Fatalf("expected a stop-failure alert, got none")
	}
	if store.alerts[0].Severity != "critical" {
		t.Fatalf("expected critical alert, got %s", store.alerts[0].Severity)
	}
}

// --- 마지막 펄스 trailing gap 판정·즉시 보상 (2026-07-20 운영자 결정) -------

// lastPulseTestSeries — 판정용 라이브 요동 시리즈 (세션 시작 = 호출 시점−300s 앵커,
// 판정 r≈296). 기저 10(앞 8s) + 중간 평탄 + 판정창(pre/post) 값은 postV 로 제어 +
// 꼬리 20(settle ✓ / last5 > 기저×1.2 → 만복 아님).
func lastPulseTestSeries(postV float64) []float64 {
	// 길이 350 — post 창이 뒤로 이동(23~33)한 만큼 꼬리 구간도 확보해야
	// 보상 발사 직전 정착 판정(|last5−prev5|)이 성립한다.
	s := make([]float64, 350)
	for i := range s {
		switch {
		case i < 250:
			s[i] = 10
		case i < 300:
			s[i] = 10 // pre 창 (r−8..r−1 ≈ 288..295)
		case i < 330:
			s[i] = postV // post 창 (r+23..r+33 ≈ 319..329)
		case i < 332:
			s[i] = 10
		default:
			s[i] = 20 // 꼬리: 정착 + 비만복
		}
	}
	return s
}

// TestLastPulse_NoDischarge_NoCompensation — 마지막 펄스가 무배출(no)로 판정돼도
// 보상 펄스는 나가지 않고 장부도 깎이지 않는다 (2026-08-18 보상 절차 제외).
// 종전에는 총 3발(계획 2 + 보상 1)이었다.
func TestLastPulse_NoDischarge_NoCompensation(t *testing.T) {
	var runs int
	cfg := feed_cycle.TestConfig()
	cfg.CompensationMaxPerCycle = 1
	store := &mockStore{}
	w := feed_cycle.NewWorker(nil, store, nil, cfg, nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, cmd string) error {
		if cmd == "feeder.run" {
			runs++
		}
		return nil
	})
	calls := 0
	w.SetLiveSeriesProvider(func(string) ([]float64, time.Time, bool) {
		calls++
		if calls == 1 {
			return nil, time.Time{}, false // p1 중간 판정은 스킵 (마지막 펄스 경로만 검증)
		}
		// post 창 평탄(상승 0) → verdict no
		return lastPulseTestSeries(10), time.Now().Add(-300 * time.Second), true
	})

	p := feed_cycle.FixedParams{
		PulseDurationMs: 2000,
		GapMs:           1500,
		TotalPulses:     2,
		TargetAmountG:   37, // 2펄스(9.4g/s×2s×2=37.6g)로 도달
	}
	c := feed_cycle.NewFixedCycle("test_last_pulse_comp", "tank_01", "", p)
	c.SetOnoffFeeder(831, true)

	go w.RunCycle(c)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	if runs != 2 {
		t.Fatalf("보상 제외 — 계획 2발 그대로여야 한다, got %d", runs)
	}
	if got := c.CompensationsGranted(); got != 0 {
		t.Fatalf("보상 승인은 0 이어야 한다, got %d", got)
	}
	// 사료는 실제로 나갔으므로 장부에서 빼지 않는다 (차감 제외).
	if got := c.TotalAmountG(); got < 37 {
		t.Fatalf("차감 제외 — 장부 총량이 줄면 안 된다: %.1fg", got)
	}
}

// TestLastPulse_DischargeYes_NoCompensation — 마지막 펄스가 배출 확인(yes)이면
// trailing gap 판정 후 보상 없이 종전 사유로 종료한다 (계획 2발 그대로).
func TestLastPulse_DischargeYes_NoCompensation(t *testing.T) {
	var runs int
	cfg := feed_cycle.TestConfig()
	cfg.CompensationMaxPerCycle = 1
	store := &mockStore{}
	w := feed_cycle.NewWorker(nil, store, nil, cfg, nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, cmd string) error {
		if cmd == "feeder.run" {
			runs++
		}
		return nil
	})
	calls := 0
	w.SetLiveSeriesProvider(func(string) ([]float64, time.Time, bool) {
		calls++
		if calls == 1 {
			return nil, time.Time{}, false
		}
		// post 창 급상승(10→30) → verdict yes
		return lastPulseTestSeries(30), time.Now().Add(-300 * time.Second), true
	})

	p := feed_cycle.FixedParams{
		PulseDurationMs: 2000,
		GapMs:           1500,
		TotalPulses:     2,
		TargetAmountG:   37,
	}
	c := feed_cycle.NewFixedCycle("test_last_pulse_yes", "tank_01", "", p)
	c.SetOnoffFeeder(831, true)

	go w.RunCycle(c)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	if runs != 2 {
		t.Fatalf("expected 2 runs (보상 없음), got %d", runs)
	}
	if got := c.CompensationsGranted(); got != 0 {
		t.Fatalf("expected 0 compensation, got %d", got)
	}
}

// offModeStore — 자율모드 off (반응형 보상 차단) 탱크.
type offModeStore struct{ mockStore }

func (m *offModeStore) GetTankAutonomousMode(_ context.Context, tankID string) (*storage.TankAutonomousMode, error) {
	return &storage.TankAutonomousMode{TankID: tankID, Mode: "off"}, nil
}

// TestLastPulse_AutonomousOff_JudgesButNoExtraPulse — 보상이 차단돼 있어도 마지막
// 펄스는 판정한다(2026-08-14). 종전에는 off 인 동안 사이클마다 마지막 펄스 기록이
// 통째로 빠졌다.
//
// ⚠ 동시에 검증하는 것 — 판정 no 의 차감(deductPhantomOnly)이 totalAmountG 를 낮춰
// target 이 풀려도 **펄스가 한 발도 더 나가면 안 된다.** 보상 승인 없이 루프가 다시
// 열리면 보상을 꺼 둔 의미가 없어진다.
func TestLastPulse_AutonomousOff_JudgesButNoExtraPulse(t *testing.T) {
	var runs int
	cfg := feed_cycle.TestConfig()
	cfg.CompensationMaxPerCycle = 1
	w := feed_cycle.NewWorker(nil, &offModeStore{}, nil, cfg, nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, cmd string) error {
		if cmd == "feeder.run" {
			runs++
		}
		return nil
	})
	calls := 0
	w.SetLiveSeriesProvider(func(string) ([]float64, time.Time, bool) {
		calls++
		if calls == 1 {
			return nil, time.Time{}, false // p1 중간 판정은 스킵
		}
		return lastPulseTestSeries(10), time.Now().Add(-300 * time.Second), true // no
	})

	p := feed_cycle.FixedParams{
		PulseDurationMs: 2000,
		GapMs:           1500,
		TotalPulses:     2,
		TargetAmountG:   37,
	}
	c := feed_cycle.NewFixedCycle("test_last_pulse_off", "tank_01", "", p)
	c.SetOnoffFeeder(831, true)

	go w.RunCycle(c)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	if calls < 2 {
		t.Fatalf("마지막 펄스 판정이 수행되지 않았다 (라이브 조회 %d 회)", calls)
	}
	if runs != 2 {
		t.Fatalf("보상 off 인데 펄스가 더 나갔다: expected 2 runs, got %d", runs)
	}
	if got := c.CompensationsGranted(); got != 0 {
		t.Fatalf("expected 0 compensation, got %d", got)
	}
}
