// Package retention archives high-volume telemetry events to monthly backup
// files and prunes them from the live SQLite DB, keeping the operational DB
// bounded for 365-day uptime.
//
// 설계 원칙:
//   - 완전 삭제 금지 — 만료 이벤트는 먼저 월별 백업 파일로 export(fsync) 한 뒤에만
//     live DB 에서 제거한다. 백업이 실패하면 삭제하지 않는다.
//   - 백업 파일은 자동 삭제하지 않는다. 저장/삭제는 운영자가 직접 관리한다.
//   - Rules 에 명시된 event_type 만 대상. 운영·감사 이벤트는 보존된다.
package retention

import (
	"compress/gzip"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"bluei.kr/edge/internal/storage"
)

// eventArchiver is the narrow slice of storage.Store the worker needs.
// storage.Store satisfies it; tests use a small fake.
type eventArchiver interface {
	SelectEventsOlderThan(ctx context.Context, eventType string, cutoff time.Time, limit int) ([]storage.ArchivableEvent, error)
	DeleteEventsOlderThanUpToSeq(ctx context.Context, eventType string, cutoff time.Time, maxSeq int64) (int64, error)
}

// dailyAggregator emits a per-day min/max/avg summary for raw readings recorded
// before `before`, returning how many summaries were emitted. Implemented by
// runtime.App. Optional — when nil, AggregateDaily rules skip the summary step.
type dailyAggregator interface {
	SummarizeSensorReadingsDaily(ctx context.Context, before time.Time, loc *time.Location) (int, error)
}

// Rule is the retention window for one event_type.
type Rule struct {
	EventType string
	KeepDays  int
	// AggregateDaily — true 면 prune 전에 일별 요약(min/max/avg)을 남긴다.
	// cutoff 도 rolling 이 아닌 로컬 달력일 경계로 정렬되어, 최근 24~48h raw 는 보존된다.
	AggregateDaily bool
}

// Config controls the retention worker.
type Config struct {
	Enabled      bool
	Interval     time.Duration // 0 → 24h
	InitialDelay time.Duration // 0 → 2m
	BatchSize    int           // 0 → 5000
	ArchiveDir   string        // 백업 파일 디렉터리 (필수)
	Rules        []Rule
	Location     *time.Location // AggregateDaily 일 경계 기준. nil → UTC
	Aggregator   dailyAggregator
}

// Worker periodically archives+prunes expired telemetry events.
type Worker struct {
	cfg   Config
	store eventArchiver
	agg   dailyAggregator
	loc   *time.Location
	log   *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

// NewWorker creates a Worker. Caller must call Start to launch the loop.
func NewWorker(store eventArchiver, cfg Config) *Worker {
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 2 * time.Minute
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5000
	}
	loc := cfg.Location
	if loc == nil {
		loc = time.UTC
	}
	return &Worker{
		cfg:   cfg,
		store: store,
		agg:   cfg.Aggregator,
		loc:   loc,
		log:   slog.With("service", "retention_worker"),
		done:  make(chan struct{}),
	}
}

// Service interface — runtime/lifecycle.go 호환 ----------------------------

func (w *Worker) Name() string { return "retention_worker" }

func (w *Worker) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.log.Info("retention worker disabled by config")
		close(w.done)
		return nil
	}
	if len(w.cfg.Rules) == 0 {
		w.log.Info("retention worker has no rules; not starting")
		close(w.done)
		return nil
	}
	if w.cfg.ArchiveDir == "" {
		close(w.done)
		return fmt.Errorf("retention: archive_dir is empty")
	}
	if err := os.MkdirAll(w.cfg.ArchiveDir, 0o755); err != nil {
		close(w.done)
		return fmt.Errorf("retention: create archive dir: %w", err)
	}
	// Long-running goroutine 은 startup ctx (timeout) 에 묶이면 안 됨 — Stop() 으로만 종료.
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(runCtx)
	w.log.Info("retention worker started",
		"interval", w.cfg.Interval, "initial_delay", w.cfg.InitialDelay,
		"archive_dir", w.cfg.ArchiveDir, "rules", len(w.cfg.Rules))
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

func (w *Worker) loop(ctx context.Context) {
	defer close(w.done)

	select {
	case <-ctx.Done():
		return
	case <-time.After(w.cfg.InitialDelay):
	}

	w.tick(ctx)

	ticker := time.NewTicker(w.cfg.Interval)
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

func (w *Worker) tick(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now().UTC()
	for _, r := range w.cfg.Rules {
		if ctx.Err() != nil {
			return
		}
		if r.EventType == "" || r.KeepDays <= 0 {
			continue
		}

		var cutoff time.Time
		if r.AggregateDaily {
			// 로컬 달력일 경계로 정렬 → 온전한 날만 요약/삭제, 최근 24~48h raw 보존.
			t := now.In(w.loc)
			startToday := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, w.loc)
			cutoff = startToday.AddDate(0, 0, -r.KeepDays)
			// prune 전에 삭제될 raw 를 일별 요약으로 보존.
			if w.agg != nil {
				emitted, err := w.agg.SummarizeSensorReadingsDaily(ctx, cutoff, w.loc)
				if err != nil {
					w.log.Warn("retention: daily summary failed; skipping prune to avoid data loss",
						"event_type", r.EventType, "error", err)
					continue
				}
				if emitted > 0 {
					w.log.Info("retention: daily summaries emitted",
						"event_type", r.EventType, "count", emitted)
				}
			}
		} else {
			cutoff = now.AddDate(0, 0, -r.KeepDays)
		}

		n, err := w.archiveAndPrune(ctx, r.EventType, cutoff)
		if err != nil {
			w.log.Warn("retention: archive/prune failed",
				"event_type", r.EventType, "error", err)
			continue
		}
		if n > 0 {
			w.log.Info("retention: archived+pruned",
				"event_type", r.EventType, "keep_days", r.KeepDays,
				"aggregate_daily", r.AggregateDaily,
				"cutoff", cutoff.Format(time.RFC3339), "rows", n)
		}
	}
}

// archiveAndPrune backs up then deletes all events of a type recorded before
// cutoff, batching to bound memory. Each batch is fully archived (fsync) before
// its rows are deleted, so a crash mid-run never loses data — at worst a batch
// is re-archived (harmless duplicate) on the next run.
func (w *Worker) archiveAndPrune(ctx context.Context, eventType string, cutoff time.Time) (int64, error) {
	var total int64
	for {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		batch, err := w.store.SelectEventsOlderThan(ctx, eventType, cutoff, w.cfg.BatchSize)
		if err != nil {
			return total, fmt.Errorf("select: %w", err)
		}
		if len(batch) == 0 {
			return total, nil
		}
		if err := w.archiveBatch(batch); err != nil {
			return total, fmt.Errorf("archive: %w", err)
		}
		maxSeq := batch[len(batch)-1].Sequence
		deleted, err := w.store.DeleteEventsOlderThanUpToSeq(ctx, eventType, cutoff, maxSeq)
		if err != nil {
			return total, fmt.Errorf("delete: %w", err)
		}
		total += deleted
		if len(batch) < w.cfg.BatchSize {
			return total, nil
		}
	}
}

// archiveBatch groups events by recorded month and appends each group to its
// monthly backup file (events-YYYY-MM.jsonl.gz). gzip members concatenate, so
// append is safe and readers (zcat / Go gzip multistream) handle it.
func (w *Worker) archiveBatch(batch []storage.ArchivableEvent) error {
	byMonth := map[string][]storage.ArchivableEvent{}
	for _, e := range batch {
		key := e.RecordedAt.UTC().Format("2006-01")
		byMonth[key] = append(byMonth[key], e)
	}
	months := make([]string, 0, len(byMonth))
	for k := range byMonth {
		months = append(months, k)
	}
	sort.Strings(months)
	for _, m := range months {
		if err := w.appendMonth(m, byMonth[m]); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) appendMonth(month string, evs []storage.ArchivableEvent) error {
	path := filepath.Join(w.cfg.ArchiveDir, fmt.Sprintf("events-%s.jsonl.gz", month))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	for _, e := range evs {
		if _, err := gz.Write([]byte(e.EventJSON)); err != nil {
			return err
		}
		if _, err := gz.Write([]byte("\n")); err != nil {
			return err
		}
	}
	if err := gz.Close(); err != nil { // flush gzip member
		return err
	}
	return f.Sync() // 백업이 디스크에 안착한 뒤에만 caller 가 DB 에서 삭제한다
}
