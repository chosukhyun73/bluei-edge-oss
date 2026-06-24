package feed_cycle

// EnvironmentMonitor — Phase 1e-B.
//
// 수온/DO 시계열 감시. threshold 초과 변화 감지 시 affected tank 의
// AIScheduler.RebuildScheduleForTank 호출.
//
// 5개월 실증 안전성 — 환경 악화 시 다음 cycle 미루기 + 양 감량 (정책 계산은
// AIScheduler 가 effective policy + sensor 실측을 다시 읽어 적용).
//
// 동작:
//   - 5 분 polling. 모든 tank profile 순회.
//   - tank 별 current_tank_environment 에서 수온/DO 최신값 조회.
//   - 이전 polling 의 값과 비교. threshold 초과 시 변화로 판단.
//   - 변화 감지 → AIScheduler.RebuildScheduleForTank.
//
// threshold (도메인 hardcoded — 단계별 분리는 향후 phase):
//   - 수온: |Δ| ≥ 2.0 °C — 어류 스트레스 임계.
//   - DO:   |Δ| ≥ 1.0 mg/L 또는 절대값 < 5.0 mg/L (저산소).
//
// 안전:
//   - 첫 polling 은 baseline 채움 전용. 변화 감지 안 됨 (정상).
//   - AIScheduler.RebuildScheduleForTank 는 정책상 idempotent — 동일 schedule 이면
//     no-op (scheduleMatches). race 발생해도 DB 쓰기 중복 없음.
//   - operating_mode != auto 인 tank 는 RebuildScheduleForTank 가 schedule 삭제만
//     수행. 환경 감시 자체는 비용 무시 수준.

import (
	"context"
	"log/slog"
	"math"
	"time"

	"bluei.kr/edge/internal/storage"
)

const (
	envMonitorPollIntervalSec = 5 * 60

	envTempDeltaC      = 2.0 // 수온 |Δ| ≥ 2°C → 변화
	envDODeltaMgPerL   = 1.0 // DO |Δ| ≥ 1 mg/L → 변화
	envDOLowThreshMgPL = 5.0 // DO 절대값 < 5 mg/L → 저산소 (변화 간주)

	envMetricWaterTemp = "water_temperature"
	envMetricDO        = "dissolved_oxygen"
)

// EnvironmentMonitor — 수온/DO 변화 감시 worker.
type EnvironmentMonitor struct {
	store     storage.Store
	scheduler *AIScheduler
	log       *slog.Logger

	pollInterval time.Duration
	cancel       context.CancelFunc
	done         chan struct{}

	// 이전 측정값 cache (변화량 비교용). tank_id → 값.
	lastTemps map[string]float64
	lastDOs   map[string]float64
}

// NewEnvironmentMonitor — 신규 생성. scheduler 가 nil 이면 변화 감지 시 no-op.
func NewEnvironmentMonitor(store storage.Store, scheduler *AIScheduler) *EnvironmentMonitor {
	return &EnvironmentMonitor{
		store:        store,
		scheduler:    scheduler,
		log:          slog.With("service", "env_monitor"),
		pollInterval: time.Duration(envMonitorPollIntervalSec) * time.Second,
		lastTemps:    make(map[string]float64),
		lastDOs:      make(map[string]float64),
		done:         make(chan struct{}),
	}
}

// Name — runtime.Service 인터페이스.
func (e *EnvironmentMonitor) Name() string { return "env_monitor" }

// Start — polling 시작.
func (e *EnvironmentMonitor) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	go e.pollLoop(runCtx)
	e.log.Info("environment monitor started", "poll_interval", e.pollInterval)
	return nil
}

// Stop — polling 취소.
func (e *EnvironmentMonitor) Stop(ctx context.Context) error {
	if e.cancel != nil {
		e.cancel()
	}
	select {
	case <-e.done:
	case <-ctx.Done():
	}
	return nil
}

// pollLoop — pollInterval 마다 모든 tank 수온/DO 변화량 확인.
func (e *EnvironmentMonitor) pollLoop(ctx context.Context) {
	defer close(e.done)
	e.check(ctx) // start 즉시 baseline 채움
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.check(ctx)
		}
	}
}

// check — 모든 tank 순회 + 변화 감지 + RebuildScheduleForTank 트리거.
func (e *EnvironmentMonitor) check(ctx context.Context) {
	tanks, err := e.store.ListTankProfiles(ctx)
	if err != nil {
		e.log.Warn("list tank profiles failed", "error", err)
		return
	}
	for _, tp := range tanks {
		e.checkTank(ctx, tp.TankID)
	}
}

// checkTank — 단일 tank 의 수온/DO 변화량 검사.
func (e *EnvironmentMonitor) checkTank(ctx context.Context, tankID string) {
	readings, err := e.store.ListTankEnvironment(ctx, tankID)
	if err != nil {
		e.log.Warn("list tank environment failed", "tank_id", tankID, "error", err)
		return
	}

	var currentTemp, currentDO float64
	var tempPresent, doPresent bool
	for _, r := range readings {
		if r == nil || r.Value == nil {
			continue
		}
		switch r.Metric {
		case envMetricWaterTemp:
			currentTemp = *r.Value
			tempPresent = true
		case envMetricDO:
			currentDO = *r.Value
			doPresent = true
		}
	}

	changed := false

	if tempPresent {
		if prev, ok := e.lastTemps[tankID]; ok {
			if math.Abs(currentTemp-prev) >= envTempDeltaC {
				e.log.Info("tank water temperature changed significantly",
					"tank_id", tankID, "prev", prev, "current", currentTemp)
				changed = true
			}
		}
		e.lastTemps[tankID] = currentTemp
	}

	if doPresent {
		if prev, ok := e.lastDOs[tankID]; ok {
			if math.Abs(currentDO-prev) >= envDODeltaMgPerL || currentDO < envDOLowThreshMgPL {
				e.log.Info("tank dissolved oxygen changed significantly",
					"tank_id", tankID, "prev", prev, "current", currentDO)
				changed = true
			}
		}
		e.lastDOs[tankID] = currentDO
	}

	if changed && e.scheduler != nil {
		if err := e.scheduler.RebuildScheduleForTank(ctx, tankID); err != nil {
			e.log.Warn("rebuild schedule after env change failed",
				"tank_id", tankID, "error", err)
		}
	}
}
