package schedule

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/arbiter"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
)

// Config holds schedule worker settings.
type Config struct {
	Enabled     bool `yaml:"enabled"`
	IntervalSec int  `yaml:"interval_sec"`
}

// DefaultConfig returns a safe default (enabled, 30 s interval).
func DefaultConfig() Config {
	return Config{Enabled: true, IntervalSec: 30}
}

// Worker periodically checks active feeding schedules and triggers feed cycles.
type Worker struct {
	cfg     Config
	store   storage.Store
	fc      *feed_cycle.Worker
	arbiter *arbiter.Arbiter
	log     *slog.Logger
	fired   map[string]struct{} // schedule_id+YYYY-MM-DDTHH:MM dedup key
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewWorker creates a schedule Worker.
// arb 가 nil 이면 직접 fc 로 사이클을 시작한다 (하위 호환).
func NewWorker(store storage.Store, fc *feed_cycle.Worker, cfg Config) *Worker {
	if cfg.IntervalSec <= 0 {
		cfg.IntervalSec = 30
	}
	return &Worker{
		cfg:   cfg,
		store: store,
		fc:    fc,
		log:   slog.With("service", "schedule_worker"),
		fired: make(map[string]struct{}),
		done:  make(chan struct{}),
	}
}

// SetArbiter 는 schedule Worker 가 사이클 시작 시 Arbiter 를 경유하도록 설정한다.
func (w *Worker) SetArbiter(arb *arbiter.Arbiter) {
	w.arbiter = arb
}

func (w *Worker) Name() string { return "schedule" }

func (w *Worker) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.log.Info("schedule worker disabled by config")
		close(w.done)
		return nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go func() {
		defer close(w.done)
		w.loop(runCtx)
	}()
	w.log.Info("schedule worker started", "interval_sec", w.cfg.IntervalSec)
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
	interval := time.Duration(w.cfg.IntervalSec) * time.Second
	tick := time.NewTicker(interval)
	defer tick.Stop()

	// check immediately on start
	w.check(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			w.check(ctx)
		}
	}
}

// check loads enabled schedules and fires any that match the current HH:MM.
func (w *Worker) check(ctx context.Context) {
	schedules, err := w.store.ListEnabledSchedules(ctx)
	if err != nil {
		w.log.Warn("schedule worker: list enabled schedules failed", "error", err)
		return
	}

	now := time.Now()
	currentHHMM := now.Format("15:04")
	// dedup key includes date so a restart within the same minute won't double-fire
	datePrefix := now.Format("2006-01-02")

	for _, s := range schedules {
		for _, fireTime := range effectiveFireTimes(s) {
			if fireTime != currentHHMM {
				continue
			}
			dedupKey := s.ScheduleID + "+" + datePrefix + "T" + fireTime
			w.mu.Lock()
			_, alreadyFired := w.fired[dedupKey]
			w.mu.Unlock()
			if alreadyFired {
				continue
			}
			// mark before firing to avoid racing duplicate triggers
			w.mu.Lock()
			w.fired[dedupKey] = struct{}{}
			w.mu.Unlock()

			w.fireCycle(ctx, s)
		}
	}
}

// effectiveFireTimes returns HH:MM fire times for a schedule.
// Cron takes precedence over times list if non-empty.
func effectiveFireTimes(s *storage.FeedingSchedule) []string {
	if s.Cron != "" {
		times, err := parseCronTimes(s.Cron)
		if err == nil {
			return times
		}
	}
	return s.Times
}

// fireCycle starts a fixed feed cycle for each tank in the schedule.
// Arbiter 가 설정된 경우 Arbiter 경유 (source=operator_schedule, priority=manual_override).
func (w *Worker) fireCycle(ctx context.Context, s *storage.FeedingSchedule) {
	if s.Pattern.PulseDurationMs <= 0 || s.Pattern.TotalPulses <= 0 {
		w.log.Warn("schedule: skipping fire — invalid pattern", "schedule_id", s.ScheduleID)
		return
	}
	gapMs := s.Pattern.GapMs
	if gapMs < 0 {
		gapMs = 0
	}

	params := map[string]any{
		"pulse_duration_ms": float64(s.Pattern.PulseDurationMs),
		"gap_ms":            float64(gapMs),
		"total_pulses":      float64(s.Pattern.TotalPulses),
	}
	// 회당 목표량 — fixed 사이클 마지막 펄스 트림용 (올림 과급 방지, 2026-07-07).
	if s.Pattern.TargetAmountG != nil && *s.Pattern.TargetAmountG > 0 {
		params["target_amount_g"] = *s.Pattern.TargetAmountG
	}

	// 2026-05-20: schedule.Priority 를 Arbiter Source 로 정확 매핑.
	// 이전: 모든 schedule fire 가 SourceOperatorSchedule → arbiter 에 manual_override 로 기록.
	//        AI가 만든 ai_advisory schedule 도 fire 시 manual_override 로 둔갑.
	// 이후: AIScheduler 가 만든 ai_advisory / ai_autonomous schedule 이 arbiter 에 정확히
	//        그 priority 로 기록 → 5-G Progressive Autonomy 로그 audit 가능.
	src := arbiter.SourceOperatorSchedule
	switch s.Priority {
	case "ai_advisory":
		src = arbiter.SourceAIAdvisory
	case "ai_autonomous":
		src = arbiter.SourceAIAutonomous
	}

	for _, tankID := range s.TankIDs {
		// 재시작 이중 발화 가드 (2026-07-07 14:00 실사고): 발화 이력(fired 맵)이
		// 메모리에만 있어, 발화 직후 같은 분(HH:MM) 안에 재시작하면 새 프로세스가
		// 같은 슬롯을 재발화한다(p1 중복 투입). DB 의 feed.cycle.started 최근 90초에
		// 같은 수조 발화가 있으면 skip — 프로세스 수명과 무관하게 멱등.
		if w.recentCycleStarted(ctx, tankID, 90*time.Second) {
			w.log.Info("schedule: skipping fire — recent cycle already started (restart double-fire guard)",
				"schedule_id", s.ScheduleID, "tank_id", tankID)
			continue
		}

		// 결손 이월 폐지 (2026-08-23 운영자 지시). 종전에는 직전 사이클이
		// safety_block 으로 조기 종료되면 못 쏜 펄스를 다음 사이클에 가산했다
		// (2026-07-16 지시). 8-22 15:37 이 저쪽 502 로 5/8 발에서 멈추자 다음 날
		// 첫 사이클이 11발(평소 1.4배)로 나갔고, 뒤쪽 펄스에서 rise −0.20(no)·
		// unclear 가 나왔다. **못 나간 사료는 못 나간 것으로 두고 넘긴다.**
		// 발화는 항상 스케줄 패턴 그대로이며 params 는 수조 간 그대로 공유된다.
		if w.arbiter != nil {
			// Arbiter 경유: 충돌 시 로그만 남기고 계속
			dec, err := w.arbiter.Submit(ctx, arbiter.CycleRequest{
				TankID:      tankID,
				Source:      src,
				Mode:        "fixed",
				Params:      params,
				SubmittedAt: time.Now().UTC(),
			})
			if err != nil {
				w.log.Warn("schedule: arbiter submit failed",
					"schedule_id", s.ScheduleID, "tank_id", tankID, "error", err)
				continue
			}
			if !dec.Accepted {
				w.log.Info("schedule: cycle rejected by arbiter",
					"schedule_id", s.ScheduleID, "tank_id", tankID,
					"reason", dec.RejectionReason, "existing_cycle_id", dec.ExistingCycleID)
				continue
			}
			w.log.Info("schedule: feed cycle started via arbiter",
				"schedule_id", s.ScheduleID, "tank_id", tankID,
				"cycle_id", dec.ResultingCycleID, "decision_id", dec.DecisionID)
			continue
		}

		// Arbiter 미설정: 직접 feed_cycle.Worker 호출 (하위 호환)
		p := feed_cycle.FixedParams{
			PulseDurationMs: s.Pattern.PulseDurationMs,
			GapMs:           gapMs,
			TotalPulses:     s.Pattern.TotalPulses,
		}
		if s.Pattern.TargetAmountG != nil && *s.Pattern.TargetAmountG > 0 {
			p.TargetAmountG = *s.Pattern.TargetAmountG
		}
		cycle, err := w.fc.StartFixedCycle(ctx, tankID, "", p, "manual_override", "")
		if err != nil {
			w.log.Warn("schedule: start fixed cycle failed",
				"schedule_id", s.ScheduleID, "tank_id", tankID, "error", err)
			continue
		}
		w.log.Info("schedule: feed cycle started",
			"schedule_id", s.ScheduleID, "tank_id", tankID, "cycle_id", cycle.CycleID)
	}
}

// recentCycleStarted — 이 수조의 feed.cycle.started 이벤트가 최근 window 안에 있는지.
// 재시작 이중 발화 가드용 (조회 실패 시 false — 발화를 막지 않는 보수적 폴백).
func (w *Worker) recentCycleStarted(ctx context.Context, tankID string, window time.Duration) bool {
	since := time.Now().UTC().Add(-window)
	evs, err := w.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "feed.cycle.started",
		Since:     &since,
		Limit:     50,
	})
	if err != nil {
		w.log.Warn("schedule: recent cycle query failed", "tank_id", tankID, "error", err)
		return false
	}
	for _, e := range evs {
		var p struct {
			TankID string `json:"tank_id"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil && p.TankID == tankID {
			return true
		}
	}
	return false
}
