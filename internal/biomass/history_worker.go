package biomass

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/storage"
)

// HistoryWorkerConfig — HistorySnapshotWorker 동작 설정.
type HistoryWorkerConfig struct {
	Enabled      bool
	Interval     time.Duration // 0 → 1 hour
	InitialDelay time.Duration // 0 → 30s
}

// HistorySnapshotWorker — active lifecycle Cage/Tank 마다 주기적으로
// 추정 체중 스냅샷을 upsert. 같은 날 안에서는 마지막 값으로 갱신.
type HistorySnapshotWorker struct {
	store        storage.Store
	interval     time.Duration
	initialDelay time.Duration
	log          *slog.Logger
	cancel       context.CancelFunc
	done         chan struct{}
	mu           sync.Mutex
	enabled      bool
}

func NewHistorySnapshotWorker(store storage.Store, cfg HistoryWorkerConfig, log *slog.Logger) *HistorySnapshotWorker {
	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	initialDelay := cfg.InitialDelay
	if initialDelay < 0 {
		initialDelay = 30 * time.Second
	}
	return &HistorySnapshotWorker{
		store:        store,
		interval:     interval,
		initialDelay: initialDelay,
		log:          log.With("service", "history_snapshot"),
		done:         make(chan struct{}),
		enabled:      cfg.Enabled,
	}
}

func (w *HistorySnapshotWorker) Name() string { return "history_snapshot" }

func (w *HistorySnapshotWorker) Start(ctx context.Context) error {
	if !w.enabled {
		w.log.Info("history snapshot worker disabled by config")
		close(w.done)
		return nil
	}
	// Long-running goroutine 은 startup ctx (timeout) 에 묶이면 안 됨 — Stop() 으로만 종료.
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(runCtx)
	w.log.Info("history snapshot worker started",
		"interval", w.interval, "initial_delay", w.initialDelay)
	return nil
}

func (w *HistorySnapshotWorker) Stop(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	select {
	case <-w.done:
	case <-ctx.Done():
	}
	return nil
}

// loop — InitialDelay 후 즉시 1회, 이후 Interval 마다 tick.
func (w *HistorySnapshotWorker) loop(ctx context.Context) {
	defer close(w.done)

	select {
	case <-ctx.Done():
		return
	case <-time.After(w.initialDelay):
	}

	w.tick(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick — 이전 tick 이 실행 중이면 skip.
// active lifecycle 이 있는 Cage/Tank 마다 SnapshotForTank 호출.
func (w *HistorySnapshotWorker) tick(ctx context.Context) {
	if !w.mu.TryLock() {
		w.log.Debug("history snapshot tick: previous tick still running, skipping")
		return
	}
	defer w.mu.Unlock()

	profiles, err := w.store.ListTankProfiles(ctx)
	if err != nil {
		w.log.Warn("history snapshot: list tank profiles failed", "error", err)
		return
	}

	now := time.Now()
	snapshotted, skipped, errors := 0, 0, 0
	for _, p := range profiles {
		ok, err := SnapshotForTank(ctx, w.store, p.TankID, now)
		if err != nil {
			w.log.Warn("history snapshot: snapshot failed", "tank_id", p.TankID, "error", err)
			errors++
			continue
		}
		if ok {
			snapshotted++
		} else {
			skipped++
		}
	}

	w.log.Info("history snapshot tick",
		"tanks_snapshotted", snapshotted,
		"skipped", skipped,
		"errors", errors,
	)
}
