package arbiter_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"bluei.kr/edge/internal/arbiter"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
)

// --- mock store --- 최소 인터페이스 구현

type mockStore struct {
	storage.Store
	mu             sync.Mutex
	activeCycles   []*storage.FeedCycle
	insertedCycles []*storage.FeedCycle
	decisions      []*storage.ArbiterDecision
	sensorReadings map[sensorReadingKey]*storage.LatestSensorReading
}

func (m *mockStore) ListActiveFeedCycles(_ context.Context) ([]*storage.FeedCycle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*storage.FeedCycle, len(m.activeCycles))
	copy(out, m.activeCycles)
	return out, nil
}

func (m *mockStore) InsertFeedCycle(_ context.Context, c *storage.FeedCycle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertedCycles = append(m.insertedCycles, c)
	m.activeCycles = append(m.activeCycles, c)
	return nil
}

func (m *mockStore) UpdateFeedCycleProgress(_ context.Context, _ string, _ int, _ float64) error {
	return nil
}

func (m *mockStore) CompleteFeedCycle(_ context.Context, cycleID string, _ int, _ float64, _ string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 완료된 사이클을 active 목록에서 제거
	filtered := m.activeCycles[:0]
	for _, c := range m.activeCycles {
		if c.CycleID != cycleID {
			filtered = append(filtered, c)
		}
	}
	m.activeCycles = filtered
	return nil
}

// seedActiveCycle은 테스트용으로 주어진 priority의 활성 사이클을 직접 주입한다.
func (m *mockStore) seedActiveCycle(cycleID, tankID, priority string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeCycles = append(m.activeCycles, &storage.FeedCycle{
		CycleID:  cycleID,
		TankID:   tankID,
		Priority: priority,
		Mode:     "fixed",
	})
}

func (m *mockStore) InsertArbiterDecision(_ context.Context, d *storage.ArbiterDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions = append(m.decisions, d)
	return nil
}

func (m *mockStore) ListArbiterDecisions(_ context.Context, _ string, _ int) ([]*storage.ArbiterDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*storage.ArbiterDecision, len(m.decisions))
	copy(out, m.decisions)
	return out, nil
}

// sensorReadings — mock 센서 데이터. metric → reading.
type sensorReadingKey struct {
	tankID string
	metric string
}

func (m *mockStore) setSensorReading(tankID, metric string, value float64, observedAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sensorReadings == nil {
		m.sensorReadings = make(map[sensorReadingKey]*storage.LatestSensorReading)
	}
	v := value
	m.sensorReadings[sensorReadingKey{tankID: tankID, metric: metric}] = &storage.LatestSensorReading{
		SensorID:   "sensor_mock",
		Metric:     metric,
		Value:      &v,
		ObservedAt: observedAt.UTC().Format(time.RFC3339),
		Location:   map[string]any{"tank_id": tankID},
	}
}

func (m *mockStore) LatestSensorReadings(_ context.Context, f storage.LatestReadingFilter) ([]*storage.LatestSensorReading, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sensorReadings == nil {
		return nil, nil
	}
	key := sensorReadingKey{tankID: f.TankID, metric: f.Metric}
	r, ok := m.sensorReadings[key]
	if !ok {
		return nil, nil
	}
	return []*storage.LatestSensorReading{r}, nil
}

// newTestArbiter creates an Arbiter backed by mock storage and a minimal feed_cycle.Worker.
// safety gate 는 기본적으로 disabled — 기존 priority 로직 테스트에 영향 없음.
func newTestArbiter(t *testing.T, store *mockStore) *arbiter.Arbiter {
	t.Helper()
	// app=nil: feed_cycle.Worker.emitEvent 는 app==nil 일 때 이벤트를 skip 한다.
	fc := feed_cycle.NewWorkerForTest(nil, store, nil, nil)
	return arbiter.New(fc, store, config.ArbiterSafetyGateConfig{Enabled: false})
}

// newTestArbiterWithGate creates an Arbiter with safety gate enabled and custom thresholds.
func newTestArbiterWithGate(t *testing.T, store *mockStore, gateCfg config.ArbiterSafetyGateConfig) *arbiter.Arbiter {
	t.Helper()
	fc := feed_cycle.NewWorkerForTest(nil, store, nil, nil)
	return arbiter.New(fc, store, gateCfg)
}

// fixedParams returns minimal valid fixed params.
func fixedParams() map[string]any {
	return map[string]any{
		"pulse_duration_ms": float64(1),
		"gap_ms":            float64(0),
		"total_pulses":      float64(1),
	}
}

// --- tests -------------------------------------------------------------------

// TestArbiter_Accept verifies a single request for an idle tank is accepted.
func TestArbiter_Accept(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID:      "tank_01",
		Source:      arbiter.SourceOperatorManual,
		Mode:        "fixed",
		Params:      fixedParams(),
		SubmittedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("expected accepted, got rejected: %s", dec.RejectionReason)
	}
	if dec.DecisionID == "" {
		t.Error("DecisionID should be set")
	}

	// audit 레코드 확인
	store.mu.Lock()
	dLen := len(store.decisions)
	store.mu.Unlock()
	if dLen != 1 {
		t.Errorf("expected 1 decision record, got %d", dLen)
	}
}

// TestArbiter_RejectActiveCycle verifies a second request for the same tank is rejected
// when a cycle is already active.
func TestArbiter_RejectActiveCycle(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	// 첫 번째 요청 (manual_override) 허용
	dec1, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_01",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if !dec1.Accepted {
		t.Fatalf("expected first to be accepted")
	}

	// 두 번째 요청 (ai_advisory) — 동일 Tank, active 사이클 존재 → 거부
	dec2, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_01",
		Source: arbiter.SourceAIAdvisory,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if dec2.Accepted {
		t.Fatal("expected second request to be rejected")
	}
	if dec2.RejectionReason != "active_cycle_exists" {
		t.Errorf("expected rejection_reason=active_cycle_exists, got %s", dec2.RejectionReason)
	}
	if dec2.ExistingCycleID == "" {
		t.Error("ExistingCycleID should be set on rejection")
	}

	// audit: 2개 레코드 (1 accepted + 1 rejected)
	store.mu.Lock()
	dLen := len(store.decisions)
	store.mu.Unlock()
	if dLen != 2 {
		t.Errorf("expected 2 decision records, got %d", dLen)
	}
}

// TestArbiter_PriorityResolution은 다중 소스의 순차 충돌 시나리오를 검증한다.
//
// 시나리오 (docs/39 §5 기준):
// - 12:00 operator_schedule (manual_override) 먼저 도착 → 수락
// - 12:00 ai_advisory 나중 도착 → active 사이클 존재로 거부
//
// 실제 운영에서 동시 도착은 arbiter 내부 serialized store query 로 자연 직렬화된다.
// 이 테스트는 순서가 보장된 순차 호출로 동작 명세를 검증한다.
func TestArbiter_PriorityResolution(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	now := time.Now()

	// 1. operator_schedule (manual_override) 먼저 제출
	decSched, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID:      "ras_tank_01",
		Source:      arbiter.SourceOperatorSchedule,
		Mode:        "fixed",
		Params:      fixedParams(),
		SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("operator_schedule submit: %v", err)
	}
	if !decSched.Accepted {
		t.Fatalf("operator_schedule: expected accepted, got rejected: %s", decSched.RejectionReason)
	}

	// 2. ai_advisory 동일 시각 제출 — active 사이클 존재 → 거부
	decAI, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID:      "ras_tank_01",
		Source:      arbiter.SourceAIAdvisory,
		Mode:        "fixed",
		Params:      fixedParams(),
		SubmittedAt: now,
	})
	if err != nil {
		t.Fatalf("ai_advisory submit: %v", err)
	}
	if decAI.Accepted {
		t.Fatal("ai_advisory: expected rejected (active cycle exists)")
	}
	if decAI.RejectionReason != "active_cycle_exists" {
		t.Errorf("ai_advisory rejection_reason: want active_cycle_exists, got %s", decAI.RejectionReason)
	}

	// audit 레코드: 2개 (1 수락 + 1 거부)
	store.mu.Lock()
	decisions := make([]*storage.ArbiterDecision, len(store.decisions))
	copy(decisions, store.decisions)
	store.mu.Unlock()

	if len(decisions) != 2 {
		t.Fatalf("expected 2 arbiter_decisions records, got %d", len(decisions))
	}
	// 첫 번째: accepted=true, source=operator_schedule
	if !decisions[0].Accepted || decisions[0].Source != "operator_schedule" {
		t.Errorf("decision[0]: want accepted operator_schedule, got accepted=%v source=%s",
			decisions[0].Accepted, decisions[0].Source)
	}
	// 두 번째: accepted=false, source=ai_advisory
	if decisions[1].Accepted || decisions[1].Source != "ai_advisory" {
		t.Errorf("decision[1]: want rejected ai_advisory, got accepted=%v source=%s",
			decisions[1].Accepted, decisions[1].Source)
	}
}

// TestPreemptManualOverridesAdvisory — ai_advisory 활성 사이클이 있을 때
// manual_override 요청은 선점하고 새 사이클을 시작해야 한다.
func TestPreemptManualOverridesAdvisory(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	// ai_advisory 사이클을 직접 시드 (feed_cycle.Worker 없이 mock 주입)
	store.seedActiveCycle("cycle_advisory_001", "tank_01", "ai_advisory")

	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID:      "tank_01",
		Source:      arbiter.SourceOperatorManual,
		Mode:        "fixed",
		Params:      fixedParams(),
		SubmittedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("expected accepted (preempt), got rejected: %s", dec.RejectionReason)
	}
	if dec.PreemptedCycleID != "cycle_advisory_001" {
		t.Errorf("expected preempted_cycle_id=cycle_advisory_001, got %q", dec.PreemptedCycleID)
	}
	if dec.ResultingCycleID == "" {
		t.Error("expected resulting_cycle_id to be set")
	}

	// arbiter_decisions 레코드에 preempted_cycle_id 확인
	store.mu.Lock()
	decisions := make([]*storage.ArbiterDecision, len(store.decisions))
	copy(decisions, store.decisions)
	store.mu.Unlock()

	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision record, got %d", len(decisions))
	}
	if decisions[0].PreemptedCycleID != "cycle_advisory_001" {
		t.Errorf("arbiter_decisions.preempted_cycle_id: want cycle_advisory_001, got %q",
			decisions[0].PreemptedCycleID)
	}
}

// TestNoPreemptSamePriority — 동일 우선순위(manual_override)끼리는 선점 없이 거부한다.
func TestNoPreemptSamePriority(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	// 첫 번째 manual_override 사이클 허용
	dec1, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_02",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if !dec1.Accepted {
		t.Fatalf("expected first to be accepted")
	}

	// 두 번째 manual_override 요청 — 동일 우선순위, 선점 없이 거부
	dec2, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_02",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if dec2.Accepted {
		t.Fatal("expected second manual_override to be rejected (same priority)")
	}
	if dec2.RejectionReason != "active_cycle_exists" {
		t.Errorf("want rejection_reason=active_cycle_exists, got %s", dec2.RejectionReason)
	}
	if dec2.PreemptedCycleID != "" {
		t.Errorf("expected no preempted_cycle_id, got %q", dec2.PreemptedCycleID)
	}
}

// TestAdvisoryDoesNotPreemptManual — manual_override 활성 사이클에 ai_advisory 요청은 선점 불가.
func TestAdvisoryDoesNotPreemptManual(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	// manual_override 사이클 시드
	store.seedActiveCycle("cycle_manual_001", "tank_03", "manual_override")

	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_03",
		Source: arbiter.SourceAIAdvisory,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Accepted {
		t.Fatal("expected ai_advisory to be rejected (cannot preempt manual_override)")
	}
	if dec.RejectionReason != "active_cycle_exists" {
		t.Errorf("want active_cycle_exists, got %s", dec.RejectionReason)
	}
}

// TestArbiter_DifferentTanks verifies concurrent requests for different tanks are both accepted.
func TestArbiter_DifferentTanks(t *testing.T) {
	store := &mockStore{}
	a := newTestArbiter(t, store)

	var wg sync.WaitGroup
	results := make([]arbiter.Decision, 2)

	for idx, tankID := range []string{"tank_A", "tank_B"} {
		wg.Add(1)
		go func(i int, tid string) {
			defer wg.Done()
			dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
				TankID: tid,
				Source: arbiter.SourceOperatorManual,
				Mode:   "fixed",
				Params: fixedParams(),
			})
			if err != nil {
				t.Errorf("submit %s: %v", tid, err)
				return
			}
			results[i] = dec
		}(idx, tankID)
	}
	wg.Wait()

	for i, dec := range results {
		if !dec.Accepted {
			t.Errorf("tank[%d] expected accepted, got rejected: %s", i, dec.RejectionReason)
		}
	}
}

// defaultGateCfg returns a standard safety gate config for gate tests.
func defaultGateCfg() config.ArbiterSafetyGateConfig {
	return config.ArbiterSafetyGateConfig{
		Enabled:           true,
		SensorMaxStaleSec: 300,
		TempMinC:          4.0,
		TempMaxC:          20.0,
		DOMinMgL:          5.0,
	}
}

// TestSafetyGate_TemperatureCritical — 수온 2.0°C 는 TempMinC(4.0) 미만 → reject.
func TestSafetyGate_TemperatureCritical(t *testing.T) {
	store := &mockStore{}
	now := time.Now()
	store.setSensorReading("tank_g1", "water_temperature", 2.0, now)
	store.setSensorReading("tank_g1", "dissolved_oxygen", 8.0, now)

	a := newTestArbiterWithGate(t, store, defaultGateCfg())
	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_g1",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Accepted {
		t.Fatal("expected rejection by safety gate (temp too low)")
	}
	if dec.RejectionReason != "safety_gate:temp_critical_low" {
		t.Errorf("want safety_gate:temp_critical_low, got %s", dec.RejectionReason)
	}
}

// TestSafetyGate_OxygenCritical — DO 3.0 mg/L 는 DOMinMgL(5.0) 미만 → reject.
func TestSafetyGate_OxygenCritical(t *testing.T) {
	store := &mockStore{}
	now := time.Now()
	store.setSensorReading("tank_g2", "water_temperature", 12.0, now)
	store.setSensorReading("tank_g2", "dissolved_oxygen", 3.0, now)

	a := newTestArbiterWithGate(t, store, defaultGateCfg())
	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_g2",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Accepted {
		t.Fatal("expected rejection by safety gate (DO too low)")
	}
	if dec.RejectionReason != "safety_gate:oxygen_critical_low" {
		t.Errorf("want safety_gate:oxygen_critical_low, got %s", dec.RejectionReason)
	}
}

// TestSafetyGate_SensorStale — observed_at 10분 전 → stale reject.
func TestSafetyGate_SensorStale(t *testing.T) {
	store := &mockStore{}
	staleTime := time.Now().Add(-10 * time.Minute)
	store.setSensorReading("tank_g3", "water_temperature", 12.0, staleTime)
	store.setSensorReading("tank_g3", "dissolved_oxygen", 8.0, staleTime)

	a := newTestArbiterWithGate(t, store, defaultGateCfg())
	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_g3",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Accepted {
		t.Fatal("expected rejection by safety gate (sensor stale)")
	}
	if dec.RejectionReason != "safety_gate:sensor_stale:water_temperature" {
		t.Errorf("want safety_gate:sensor_stale:water_temperature, got %s", dec.RejectionReason)
	}
}

// TestSafetyGate_Disabled — enabled=false 이면 환경 검사 skip, priority 로직만 동작.
func TestSafetyGate_Disabled(t *testing.T) {
	store := &mockStore{}
	// 센서 데이터 없음 — enabled=false 이므로 무시되어야 함.

	disabledCfg := config.ArbiterSafetyGateConfig{Enabled: false}
	a := newTestArbiterWithGate(t, store, disabledCfg)
	dec, err := a.Submit(context.Background(), arbiter.CycleRequest{
		TankID: "tank_g4",
		Source: arbiter.SourceOperatorManual,
		Mode:   "fixed",
		Params: fixedParams(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("expected accepted (gate disabled), got rejected: %s", dec.RejectionReason)
	}
}
