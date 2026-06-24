package baseline

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// TimerExecutor — pending_notify 의 grace 경과 처리 콜백.
// 실제 control command 발행 + audit 은 구현체(api.decisionTimerExecutor) 가 담당.
type TimerExecutor interface {
	// ExecuteTimedOut — grace 경과한 결정 처리. safety gate + mode 재확인 후 실행/스킵.
	ExecuteTimedOut(ctx context.Context, decisionID, tankID, kind string, data map[string]any) error
}

// TimerConfig — DecisionTimer 동작 설정.
type TimerConfig struct {
	// Enabled — false 면 worker 시작 안 함. Cage/Tank별 enabled 가 진짜 게이트.
	Enabled bool
	// Interval — tick 주기. 0 이면 1분.
	Interval time.Duration
	// InitialDelay — 시작 후 첫 검사까지 대기. 0 이면 60초.
	InitialDelay time.Duration
}

// DecisionTimer — pending_notify 결정의 grace 경과 시 자동 실행 트리거.
// 1분마다 events 를 검사하여 grace 가 지난 unresolved pending_notify 를 처리.
// enabled=false Cage/Tank는 절대 트리거 X.
type DecisionTimer struct {
	cfg          TimerConfig
	store        storage.Store
	app          *runtime.App
	log          *slog.Logger
	policyLookup func(ctx context.Context, tankID string) (enabled bool, graceMinutes int)
	executor     TimerExecutor

	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

// NewDecisionTimer creates a DecisionTimer.
// policyLookup — Cage/Tank별 (enabled, graceMinutes) 반환. api.Server.EffectiveDecisionPolicy 를 주입.
func NewDecisionTimer(
	app *runtime.App,
	store storage.Store,
	cfg TimerConfig,
	policyLookup func(ctx context.Context, tankID string) (bool, int),
	executor TimerExecutor,
) *DecisionTimer {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 60 * time.Second
	}
	return &DecisionTimer{
		cfg:          cfg,
		store:        store,
		app:          app,
		log:          slog.With("service", "decision_timer"),
		policyLookup: policyLookup,
		executor:     executor,
		done:         make(chan struct{}),
	}
}

func (t *DecisionTimer) Name() string { return "decision_timer" }

func (t *DecisionTimer) Start(ctx context.Context) error {
	if !t.cfg.Enabled {
		t.log.Info("decision timer disabled by config")
		close(t.done)
		return nil
	}
	// Long-running goroutine 은 startup ctx (timeout) 에 묶이면 안 됨 — Stop() 으로만 종료.
	runCtx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go t.loop(runCtx)
	t.log.Info("decision timer started",
		"interval", t.cfg.Interval, "initial_delay", t.cfg.InitialDelay)
	return nil
}

func (t *DecisionTimer) Stop(ctx context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}
	select {
	case <-t.done:
	case <-ctx.Done():
	}
	return nil
}

// loop — InitialDelay 후 즉시 1회, 이후 Interval 마다 tick.
func (t *DecisionTimer) loop(ctx context.Context) {
	defer close(t.done)

	select {
	case <-ctx.Done():
		return
	case <-time.After(t.cfg.InitialDelay):
	}

	t.tick(ctx)

	ticker := time.NewTicker(t.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.tick(ctx)
		}
	}
}

// tick — 이전 tick 이 아직 실행 중이면 skip (mu.TryLock).
// 최근 200개 routed 이벤트 중 pending_notify + unresolved + grace 경과 → executor 호출.
func (t *DecisionTimer) tick(ctx context.Context) {
	// 이전 tick 이 아직 끝나지 않으면 skip
	if !t.mu.TryLock() {
		t.log.Debug("decision timer tick: previous tick still running, skipping")
		return
	}
	defer t.mu.Unlock()

	routedEvents, err := t.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankDecisionRouted,
		Limit:     200,
	})
	if err != nil {
		t.log.Warn("decision timer: query routed events failed", "error", err)
		return
	}

	// resolved decision_id set 구축 — 한 번만 조회해서 재사용
	resolvedIDs, err := t.loadResolvedIDs(ctx)
	if err != nil {
		t.log.Warn("decision timer: query resolved events failed", "error", err)
		return
	}

	expired, executed, skipped := 0, 0, 0
	for _, e := range routedEvents {
		if ctx.Err() != nil {
			return
		}
		var p events.TankDecisionRoutedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		// pending_notify 이외 route 는 처리하지 않음
		if p.Route != string(RoutePendingNotify) {
			continue
		}
		// 이미 resolve 된 결정은 skip
		if resolvedIDs[p.DecisionID] {
			continue
		}
		// 정책 조회 — enabled=false Cage/Tank는 절대 실행 X
		enabled, graceMinutes := t.policyLookup(ctx, p.TankID)
		if !enabled {
			continue
		}
		// grace 아직 안 지남 — skip
		proposedAt, err := time.Parse(time.RFC3339Nano, p.ProposedAt)
		if err != nil {
			t.log.Warn("decision timer: parse proposed_at failed",
				"decision_id", p.DecisionID, "proposed_at", p.ProposedAt)
			continue
		}
		if time.Since(proposedAt) < time.Duration(graceMinutes)*time.Minute {
			continue
		}
		// grace 경과 → executor 호출
		expired++
		if err := t.executor.ExecuteTimedOut(ctx, p.DecisionID, p.TankID, p.DecisionKind, p.DecisionData); err != nil {
			t.log.Warn("decision timer: ExecuteTimedOut failed",
				"decision_id", p.DecisionID, "tank_id", p.TankID, "error", err)
			skipped++
		} else {
			executed++
		}
	}

	if expired > 0 || executed > 0 {
		t.log.Info("decision timer tick",
			"expired", expired, "executed", executed, "skipped", skipped)
	}
}

// loadResolvedIDs — tank.decision.resolved 이벤트에서 모든 decision_id set.
func (t *DecisionTimer) loadResolvedIDs(ctx context.Context) (map[string]bool, error) {
	es, err := t.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankDecisionResolved,
		Limit:     500,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(es))
	for _, e := range es {
		var p events.TankDecisionResolvedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		out[p.DecisionID] = true
	}
	return out, nil
}
