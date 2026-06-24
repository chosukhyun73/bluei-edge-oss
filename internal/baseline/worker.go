package baseline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision"
)

// Worker periodically scores all tanks that have an active baseline model.
// Results are appended as tank.baseline.scored events.
//
// 설계 원칙 (docs/29):
//   - 한 Cage/Tank 실패가 다른 Cage/Tank에 영향 X — per-tank 격리
//   - subprocess 는 직렬 (한 번에 하나) — GX10 자원 보호. 학습/추론과 GPU 경합 방지
//   - 운영자 부재 시에도 baseline 이 백그라운드에서 항상 동작 → 자율 운영 기반
type Worker struct {
	cfg    Config
	scorer *Scorer
	app    *runtime.App
	store  storage.Store
	log    *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
}

// Config controls the worker's behaviour.
type Config struct {
	// Enabled - false 면 worker 가 시작되지 않음.
	Enabled bool
	// Interval - 한 사이클의 주기. 0 이면 600s (10분).
	Interval time.Duration
	// InitialDelay - 시작 후 첫 평가까지의 대기. 0 이면 30s. 부팅 직후 다른
	// 서비스 startup 과 충돌 회피용.
	InitialDelay time.Duration
}

// NewWorker creates a Worker. Caller must call Start to launch the loop.
func NewWorker(app *runtime.App, store storage.Store, scorer *Scorer, cfg Config) *Worker {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Minute
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 30 * time.Second
	}
	return &Worker{
		cfg:    cfg,
		scorer: scorer,
		app:    app,
		store:  store,
		log:    slog.With("service", "baseline_worker"),
		done:   make(chan struct{}),
	}
}

// Service interface — runtime/lifecycle.go 호환 ----------------------------

func (w *Worker) Name() string { return "baseline_worker" }

func (w *Worker) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.log.Info("baseline worker disabled by config")
		close(w.done)
		return nil
	}
	// Long-running goroutine 은 startup ctx (timeout) 에 묶이면 안 됨 — Stop() 으로만 종료.
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(runCtx)
	w.log.Info("baseline worker started",
		"interval", w.cfg.Interval, "initial_delay", w.cfg.InitialDelay)
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

// ── Internal loop ─────────────────────────────────────────────────────────

func (w *Worker) loop(ctx context.Context) {
	defer close(w.done)

	// 부팅 직후는 InitialDelay 만큼 대기 (다른 서비스 startup 충돌 회피)
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

// tick — 모든 Cage/Tank에 대해 활성화된 모델 (baseline / forecast) 을 직렬 실행.
// per-tank-per-kind 격리: 한 모델 실패가 다른 모델/Cage/Tank에 영향 X.
func (w *Worker) tick(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	profiles, err := w.store.ListTankProfiles(ctx)
	if err != nil {
		w.log.Warn("worker tick: ListTankProfiles failed", "error", err)
		return
	}
	baselineScored, baselineSkipped := 0, 0
	forecastScored, forecastSkipped := 0, 0
	for _, p := range profiles {
		if ctx.Err() != nil {
			return
		}
		// (1) baseline anomaly score
		base, err := vision.ActiveTankBaseline(p.TankID)
		if err != nil {
			w.log.Warn("tick: ActiveTankBaseline", "tank_id", p.TankID, "error", err)
		} else if base.ActiveWeightsPath == "" {
			baselineSkipped++
		} else if err := w.scoreOne(ctx, p.TankID, base); err != nil {
			w.log.Warn("tick: baseline score failed", "tank_id", p.TankID, "error", err)
		} else {
			baselineScored++
		}
		// (2) water forecast
		if ctx.Err() != nil {
			return
		}
		fc, err := vision.ActiveTankWaterForecast(p.TankID)
		if err != nil {
			w.log.Warn("tick: ActiveTankWaterForecast", "tank_id", p.TankID, "error", err)
		} else if fc.ActiveWeightsPath == "" {
			forecastSkipped++
		} else if err := w.forecastOne(ctx, p.TankID, fc); err != nil {
			w.log.Warn("tick: forecast failed", "tank_id", p.TankID, "error", err)
		} else {
			forecastScored++
		}
	}

	// 전환 감지 (Phase 3.5) — 활성 baseline 모델이 있는 Cage/Tank만 대상.
	for _, p := range profiles {
		if ctx.Err() != nil {
			return
		}
		base, _ := vision.ActiveTankBaseline(p.TankID)
		if base.ActiveWeightsPath == "" {
			continue
		}
		if err := w.checkTransition(ctx, p.TankID); err != nil {
			w.log.Warn("tick: transition check failed", "tank_id", p.TankID, "error", err)
		}
	}

	w.log.Info("worker tick complete",
		"baseline_scored", baselineScored, "baseline_skipped", baselineSkipped,
		"forecast_scored", forecastScored, "forecast_skipped", forecastSkipped,
		"total_tanks", len(profiles))
}

// scoreOne runs subprocess + appends event for a single tank.
func (w *Worker) scoreOne(ctx context.Context, tankID string, active vision.AlgorithmActiveState) error {
	sr, err := w.scorer.Score(ctx, tankID, active.ActiveWeightsPath)
	if err != nil {
		return err
	}
	if sr.EvaluatedAt == "" {
		sr.EvaluatedAt = common.FormatTime(common.NowUTC())
	}
	payload := events.TankBaselineScoredPayload{
		TankID:       tankID,
		ModelDir:     active.ActiveWeightsPath,
		JobID:        active.ActiveJobID,
		AnomalyScore: sr.AnomalyScore,
		P95Threshold: sr.P95Threshold,
		P99Threshold: sr.P99Threshold,
		Verdict:      sr.Verdict,
		FeatureDiff:  sr.FeatureDiff,
		EvaluatedAt:  sr.EvaluatedAt,
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	if _, err := w.app.AppendEvent(ctx, "baseline_worker", "auto", tankID,
		events.EventTankBaselineScored, tankID, payload); err != nil {
		return err
	}
	// verdict 변화에 따라 대시보드 알림 raise/update/close
	if err := MaybeRaiseOrCloseAlert(ctx, w.app, w.store, payload); err != nil {
		w.log.Warn("anomaly alert update failed",
			"tank_id", tankID, "verdict", payload.Verdict, "error", err)
		// 알림 실패는 score 자체를 무효화하지 않음
	}
	return nil
}

// checkTransition — 한 Cage/Tank 전환 감지, dedupe, audit event + alert 적재.
func (w *Worker) checkTransition(ctx context.Context, tankID string) error {
	res, err := DetectGrowthTransition(ctx, w.store, tankID)
	if err != nil {
		return err
	}
	if !res.Detected {
		return nil
	}

	// Dedupe: 직전 24h 내 같은 reason 으로 이미 emit 했으면 skip.
	if already, err := recentTransitionExists(ctx, w.store, tankID, res.Reason, 24*time.Hour); err != nil {
		return err
	} else if already {
		return nil
	}

	payload := events.TankTransitionDetectedPayload{
		TankID:     tankID,
		Reason:     res.Reason,
		DetectedAt: common.FormatTime(res.DetectedAt),
		Evidence:   res.Evidence,
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	if _, err := w.app.AppendEvent(ctx, "baseline_worker", "auto", tankID,
		events.EventTankTransitionDetected, common.NewID("transition"), payload); err != nil {
		return err
	}

	// Dashboard alert (severity=warning)
	if err := raiseTransitionAlert(ctx, w.app, w.store, payload); err != nil {
		w.log.Warn("transition alert failed", "tank_id", tankID, "error", err)
	}

	// C-3: 전환 감지 시 partial|full 모드 → observation 자동 다운그레이드.
	// 전환 감지는 baseline 이 달라졌음을 의미 — 자율 결정 안전성 보장 불가.
	if err := w.autoDowngradeOnTransition(ctx, tankID, payload.Reason); err != nil {
		w.log.Warn("auto-downgrade on transition failed", "tank_id", tankID, "error", err)
	}
	return nil
}

// autoDowngradeOnTransition — 전환 감지 시 partial|full 자율 모드를 observation 으로 낮춤.
func (w *Worker) autoDowngradeOnTransition(ctx context.Context, tankID, transitionReason string) error {
	mode, err := w.store.GetTankAutonomousMode(ctx, tankID)
	if err != nil {
		return err
	}
	if mode == nil || (mode.Mode != "partial" && mode.Mode != "full") {
		return nil // off | observation → noop
	}
	prev := mode.Mode
	mode.Mode = "observation"
	mode.Reason = "auto-downgraded by baseline_worker: transition_detected"
	mode.ChangedAt = common.NowUTC()
	mode.ChangedBy = "baseline_worker"
	if err := w.store.UpsertTankAutonomousMode(ctx, mode); err != nil {
		return err
	}
	ev := events.AutonomousModeAutoDowngradePayload{
		TankID:       tankID,
		PreviousMode: prev,
		NewMode:      "observation",
		Reason:       "transition_detected",
		Detail:       transitionReason,
		DowngradedAt: common.FormatTime(common.NowUTC()),
	}
	if err := ev.Validate(); err != nil {
		return err
	}
	_, err = w.app.AppendEvent(ctx, "baseline_worker", "auto", tankID,
		events.EventAutonomousModeAutoDowngrade, common.NewID("downgrade"), ev)
	return err
}

// recentTransitionExists — 최근 window 내 같은 tankID + reason 의 전환 이벤트 존재 여부.
func recentTransitionExists(ctx context.Context, store storage.Store, tankID, reason string, window time.Duration) (bool, error) {
	es, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankTransitionDetected,
		Limit:     50,
	})
	if err != nil {
		return false, fmt.Errorf("query transition dedupe: %w", err)
	}
	cutoff := time.Now().UTC().Add(-window)
	for _, e := range es {
		if e.RecordedAt.Before(cutoff) {
			continue
		}
		var p events.TankTransitionDetectedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID && p.Reason == reason {
			return true, nil
		}
	}
	return false, nil
}

// forecastOne — 한 Cage/Tank의 단기 수질 예측 + events 적재.
func (w *Worker) forecastOne(ctx context.Context, tankID string, active vision.AlgorithmActiveState) error {
	fr, err := w.scorer.Forecast(ctx, tankID, active.ActiveWeightsPath)
	if err != nil {
		return err
	}
	if fr.EvaluatedAt == "" {
		fr.EvaluatedAt = common.FormatTime(common.NowUTC())
	}
	payload := events.WaterForecastRecordedPayload{
		TankID:          tankID,
		ModelDir:        active.ActiveWeightsPath,
		JobID:           active.ActiveJobID,
		Metric:          fr.TargetMetric,
		HorizonMinutes:  fr.HorizonMinutes,
		PredictedValues: fr.PredictedValues,
		CurrentValue:    fr.CurrentValue,
		EvaluatedAt:     fr.EvaluatedAt,
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	_, err = w.app.AppendEvent(ctx, "baseline_worker", "auto", tankID,
		events.EventWaterForecastRecorded, tankID, payload)
	return err
}
