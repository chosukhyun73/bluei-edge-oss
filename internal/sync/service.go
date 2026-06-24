package sync

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

// Service manages the daily sync queue.
//
// Two timers run concurrently:
//   - retryTick (60s): retries any pending/failed batches against the backend
//     endpoint, so a transient outage doesn't block the queue until next day.
//   - dailyTick (60s probe + KST time match): creates and pushes the daily
//     batch at cfg.DailyCronKST (e.g. "03:00"). Runs at most once per
//     calendar day (tracked via lastDailyRunDate).
//
// When cfg.DailyCronKST is empty the daily trigger is disabled (batches are
// still retried if any were created by other means).
type Service struct {
	app        *runtime.App
	cfg        *config.SyncConfig
	store      storage.Store
	cancel     context.CancelFunc
	httpClient *http.Client
}

func NewService(app *runtime.App, cfg *config.SyncConfig, store storage.Store) *Service {
	timeout := time.Duration(cfg.HTTPTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Service{
		app:        app,
		cfg:        cfg,
		store:      store,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (s *Service) Name() string { return "sync" }

func (s *Service) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.app.Health.Set("sync", "disabled", "sync disabled by config")
		return nil
	}

	// Long-running goroutine 은 startup ctx (timeout) 에 묶이면 안 됨 — Stop() 으로만 종료.
	ctx, s.cancel = context.WithCancel(context.Background())

	if s.cfg.Endpoint == nil || *s.cfg.Endpoint == "" {
		s.app.Health.Set("sync", "offline", "sync endpoint is not configured; offline queue mode active")
		slog.Info("sync running in offline queue mode, no endpoint configured")
	} else {
		s.app.Health.Set("sync", "ok", "")
	}

	go s.loop(ctx)
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *Service) loop(ctx context.Context) {
	retryTick := time.NewTicker(60 * time.Second)
	defer retryTick.Stop()

	// Inbound (DOWN) pull — platform-originated tank deltas onto local counts.
	// 60s 주기 + 시작 20s 후 첫 pull(부팅 직후 따라잡기).
	pullTick := time.NewTicker(60 * time.Second)
	defer pullTick.Stop()
	initialPull := time.NewTimer(20 * time.Second)
	defer initialPull.Stop()

	dailyTick := time.NewTicker(1 * time.Minute)
	defer dailyTick.Stop()

	dailyHour, dailyMin, dailyEnabled := parseDailyTimeKST(s.cfg.DailyCronKST)
	if dailyEnabled {
		slog.Info("sync daily cron configured", "kst_time", s.cfg.DailyCronKST)
	} else {
		slog.Info("sync daily cron disabled (no daily_cron_kst)")
	}

	lastDailyRunDate := ""

	for {
		select {
		case <-ctx.Done():
			return
		case <-retryTick.C:
			s.retryQueue(ctx)
		case <-initialPull.C:
			s.pullTankDeltas(ctx)
		case <-pullTick.C:
			s.pullTankDeltas(ctx)
		case <-dailyTick.C:
			if !dailyEnabled {
				continue
			}
			nowKST := time.Now().In(kstLocation())
			dateKey := nowKST.Format("2006-01-02")
			if nowKST.Hour() == dailyHour && nowKST.Minute() == dailyMin && lastDailyRunDate != dateKey {
				s.runDailyBatch(ctx, nowKST)
				lastDailyRunDate = dateKey
			}
		}
	}
}

// retryQueue retries any pending failed batches. Does not create new batches.
func (s *Service) retryQueue(ctx context.Context) {
	if s.cfg.Endpoint != nil && *s.cfg.Endpoint != "" {
		if err := s.sendPending(ctx); err != nil {
			slog.Warn("sync retry failed", "error", err)
		}
	}
	s.app.Health.Touch("sync")
}

// runDailyBatch is invoked at DailyCronKST. It creates one new batch from any
// unsynced events and immediately attempts to push it.
func (s *Service) runDailyBatch(ctx context.Context, nowKST time.Time) {
	slog.Info("sync daily batch starting", "date", nowKST.Format("2006-01-02"))

	if err := createBatch(ctx, s.app, s.store, s.cfg.BatchSize); err != nil {
		slog.Warn("daily batch creation failed", "error", err)
		return
	}

	if s.cfg.Endpoint == nil || *s.cfg.Endpoint == "" {
		slog.Info("daily batch created; endpoint not configured, kept in offline queue")
		return
	}

	if err := s.sendPending(ctx); err != nil {
		slog.Warn("daily batch send failed", "error", err)
		return
	}

	slog.Info("sync daily batch completed", "date", nowKST.Format("2006-01-02"))
}

// sendPending fetches the oldest pending batch, transforms its events into the
// backend envelope, POSTs to the configured endpoint, and records the ACK.
// Batches whose events are entirely backend-unsupported are marked
// acknowledged without a network round-trip so they don't block the queue.
func (s *Service) sendPending(ctx context.Context) error {
	batch, err := s.store.GetPendingSyncBatch(ctx)
	if err != nil || batch == nil {
		return err
	}

	envelope, err := BuildBatchEnvelope(ctx, s.store, batch)
	if err != nil {
		return err
	}

	if envelope.EventCount == 0 {
		slog.Info("sync envelope has no backend-supported events; marking acknowledged",
			"batch_id", batch.BatchID)
		markBatchAcknowledged(ctx, s.app, s.store, batch.BatchID, "")
		return nil
	}

	resp, err := s.postEnvelope(ctx, envelope)
	if err != nil {
		slog.Warn("sync POST failed", "batch_id", batch.BatchID, "error", err)
		markBatchFailed(ctx, s.app, s.store, batch.BatchID, "TRANSPORT_ERROR", err.Error())
		return err
	}

	if resp.OK {
		markBatchAcknowledged(ctx, s.app, s.store, batch.BatchID, resp.BatchID)
		slog.Info("sync batch acknowledged",
			"batch_id", batch.BatchID,
			"accepted", resp.Accepted,
			"projected", resp.Projected,
			"ignored", resp.Ignored,
			"node_code", resp.NodeCode)
		return nil
	}

	errMsg := strings.Join(resp.Errors, "; ")
	if errMsg == "" {
		errMsg = "backend returned ok=false"
	}
	markBatchFailed(ctx, s.app, s.store, batch.BatchID, "REJECTED", errMsg)
	return fmt.Errorf("batch rejected: %s", errMsg)
}

// parseDailyTimeKST parses an "HH:MM" KST time string. Empty string or
// invalid input returns enabled=false.
func parseDailyTimeKST(s string) (hour, minute int, enabled bool) {
	if s == "" {
		return 0, 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		slog.Warn("invalid daily_cron_kst format, expected HH:MM", "value", s)
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		slog.Warn("invalid daily_cron_kst values", "value", s)
		return 0, 0, false
	}
	return h, m, true
}

// kstLocation returns the KST timezone. Falls back to a fixed +09:00 offset
// if the system tzdata is missing.
func kstLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.FixedZone("KST", 9*60*60)
	}
	return loc
}
