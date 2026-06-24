package baseline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// ── Mock store ────────────────────────────────────────────────────────────────

// minimalStore — decision_timer_test 에서 필요한 QueryEvents 만 구현.
type minimalStore struct {
	storage.Store
	routedEvents   []*storage.Event
	resolvedEvents []*storage.Event
	queryErr       error
}

func (m *minimalStore) QueryEvents(_ context.Context, f storage.EventFilter) ([]*storage.Event, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	switch f.EventType {
	case events.EventTankDecisionRouted:
		return m.routedEvents, nil
	case events.EventTankDecisionResolved:
		return m.resolvedEvents, nil
	}
	return nil, nil
}

// ── Mock executor ─────────────────────────────────────────────────────────────

type mockExecutor struct {
	callCount atomic.Int32
	lastID    string
}

func (e *mockExecutor) ExecuteTimedOut(_ context.Context, decisionID, _, _ string, _ map[string]any) error {
	e.callCount.Add(1)
	e.lastID = decisionID
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func makeRoutedEvent(decisionID, tankID, route string, proposedAt time.Time) *storage.Event {
	p := events.TankDecisionRoutedPayload{
		DecisionID:      decisionID,
		TankID:          tankID,
		DecisionKind:    "feeding",
		DecisionData:    nil,
		ProposingSource: "test",
		Confidence:      0.75,
		AutonomousMode:  "partial",
		Route:           route,
		Reasoning:       "test",
		ProposedAt:      proposedAt.Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(p)
	return &storage.Event{
		EventID:     "evt_" + decisionID,
		EventType:   events.EventTankDecisionRouted,
		PayloadJSON: string(b),
	}
}

func makeResolvedEvent(decisionID, tankID string) *storage.Event {
	p := events.TankDecisionResolvedPayload{
		DecisionID: decisionID,
		TankID:     tankID,
		Resolution: "approved",
		ResolvedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(p)
	return &storage.Event{
		EventID:     "evt_res_" + decisionID,
		EventType:   events.EventTankDecisionResolved,
		PayloadJSON: string(b),
	}
}

func newTimerForTest(store storage.Store, policy func(context.Context, string) (bool, int), exec TimerExecutor) *DecisionTimer {
	return &DecisionTimer{
		cfg:          TimerConfig{Enabled: true, Interval: time.Minute, InitialDelay: time.Minute},
		store:        store,
		log:          slog.Default(),
		policyLookup: policy,
		executor:     exec,
		done:         make(chan struct{}),
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestTimerSkipsWhenDisabled(t *testing.T) {
	// policyLookup → enabled=false → executor never called
	store := &minimalStore{
		routedEvents: []*storage.Event{
			makeRoutedEvent("d1", "tank_01", string(RoutePendingNotify),
				time.Now().Add(-15*time.Minute)), // grace 충분히 경과
		},
	}
	exec := &mockExecutor{}
	timer := newTimerForTest(store,
		func(_ context.Context, _ string) (bool, int) { return false, 10 },
		exec,
	)
	timer.tick(context.Background())
	if exec.callCount.Load() != 0 {
		t.Errorf("executor should not be called when disabled, got %d calls", exec.callCount.Load())
	}
}

func TestTimerSkipsBeforeGrace(t *testing.T) {
	// grace=10분, 5분만 경과 → executor never called
	store := &minimalStore{
		routedEvents: []*storage.Event{
			makeRoutedEvent("d2", "tank_01", string(RoutePendingNotify),
				time.Now().Add(-5*time.Minute)),
		},
	}
	exec := &mockExecutor{}
	timer := newTimerForTest(store,
		func(_ context.Context, _ string) (bool, int) { return true, 10 },
		exec,
	)
	timer.tick(context.Background())
	if exec.callCount.Load() != 0 {
		t.Errorf("executor should not be called before grace, got %d calls", exec.callCount.Load())
	}
}

func TestTimerCallsExecutorAfterGrace(t *testing.T) {
	// grace=10분, 11분 경과 → executor called once
	store := &minimalStore{
		routedEvents: []*storage.Event{
			makeRoutedEvent("d3", "tank_01", string(RoutePendingNotify),
				time.Now().Add(-11*time.Minute)),
		},
	}
	exec := &mockExecutor{}
	timer := newTimerForTest(store,
		func(_ context.Context, _ string) (bool, int) { return true, 10 },
		exec,
	)
	timer.tick(context.Background())
	if exec.callCount.Load() != 1 {
		t.Errorf("expected 1 executor call, got %d", exec.callCount.Load())
	}
	if exec.lastID != "d3" {
		t.Errorf("expected decision_id=d3, got %q", exec.lastID)
	}
}

func TestTimerSkipsAlreadyResolved(t *testing.T) {
	// decision d4 가 이미 resolved → executor not called
	store := &minimalStore{
		routedEvents: []*storage.Event{
			makeRoutedEvent("d4", "tank_01", string(RoutePendingNotify),
				time.Now().Add(-15*time.Minute)),
		},
		resolvedEvents: []*storage.Event{
			makeResolvedEvent("d4", "tank_01"),
		},
	}
	exec := &mockExecutor{}
	timer := newTimerForTest(store,
		func(_ context.Context, _ string) (bool, int) { return true, 10 },
		exec,
	)
	timer.tick(context.Background())
	if exec.callCount.Load() != 0 {
		t.Errorf("executor should not be called for resolved decision, got %d calls", exec.callCount.Load())
	}
}

func TestTimerOnlyHandlesPendingNotify(t *testing.T) {
	// auto_executed / pending_approval / advisory_only / rejected → all skip
	now := time.Now().Add(-15 * time.Minute)
	routes := []string{
		string(RouteAutoExecuted),
		string(RoutePendingApproval),
		string(RouteAdvisoryOnly),
		string(RouteRejected),
	}
	for i, route := range routes {
		store := &minimalStore{
			routedEvents: []*storage.Event{
				makeRoutedEvent(fmt.Sprintf("d_route_%d", i), "tank_01", route, now),
			},
		}
		exec := &mockExecutor{}
		timer := newTimerForTest(store,
			func(_ context.Context, _ string) (bool, int) { return true, 10 },
			exec,
		)
		timer.tick(context.Background())
		if exec.callCount.Load() != 0 {
			t.Errorf("route=%s: executor should not be called, got %d calls", route, exec.callCount.Load())
		}
	}
}
