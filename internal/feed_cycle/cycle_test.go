package feed_cycle_test

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// --- mock storage -----------------------------------------------------------

type mockStore struct {
	storage.Store
	inserted  []*storage.FeedCycle
	completed []string
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

// Phase A — silo depletion alert raise 가 호출하는 alert 메서드 no-op.
func (m *mockStore) GetOpenAlert(_ context.Context, _ string) (*storage.OpenAlert, error) {
	return nil, nil
}

func (m *mockStore) UpsertAlert(_ context.Context, _ *storage.OpenAlert) (bool, error) {
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
