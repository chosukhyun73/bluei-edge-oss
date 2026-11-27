package feed_cycle

// AIScheduler — Phase 1d.
//
// operating_mode='auto' 인 수조의 사이클 자율 운영.
//
// **방식 변경 (Phase 1d)**: in-memory timer 방식 폐기 → feeding_schedules
// 테이블에 AI-managed schedule (prefix `ai_`) 을 24h 전체로 등록한다.
// 기존 schedule.Worker 가 등록된 시각을 자동 trigger.
//
//   - backend crash 시에도 schedule 은 DB 영구. 기존 timer 분실 문제 해결.
//   - frontend 가 보는 일정과 backend 실 동작 일치 (둘 다 같은 DB row 참조).
//   - cycle 종료 hook + 5 분 polling fallback 으로 schedule 재동기.
//
// 다음 cycle 시각 = lastCompletedAt + GET₅₀ (어체 중량/수온/식사량 반영).
// 24h 분할 시각 = computeDailyTimes (균등 분할, plan.DailyCycles 개).
//
// 안전:
//   - 운영 정책 (operating_mode, bsf_mode, max_daily_cycles) 은 매 시점 DB 재조회.
//     사용자가 [AI 일시정지] 누른 즉시 다음 polling 또는 cycle 종료 hook 에서
//     AI-managed schedule 모두 삭제.
//   - arbiter 경유 (schedule.Worker 가 호출) → 안전 게이트 (C-3p/C-3l/C-3w) +
//     우선순위 (manual_override) 자동 적용.
//   - min cycle interval (30 분) — GET₅₀ 가 너무 작아도 그 이상 대기.

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// CycleArbiter — arbiter.Arbiter 의 좁은 인터페이스 (import cycle 회피).
// Phase 1d 에서는 schedule.Worker 가 arbiter 호출을 담당하므로 AIScheduler 는
// 더 이상 arbiter 를 직접 호출하지 않는다. 하위 호환을 위해 인터페이스는 유지.
type CycleArbiter interface {
	Submit(ctx context.Context, req ArbiterCycleRequest) (ArbiterDecision, error)
}

// ArbiterCycleRequest / ArbiterDecision — arbiter 패키지의 동형 (import cycle 회피용).
type ArbiterCycleRequest struct {
	TankID       string
	ControllerID string
	Source       string
	Mode         string
	Params       map[string]any
	SubmittedAt  time.Time
	IntentID     string
}

type ArbiterDecision struct {
	Accepted         bool
	RejectionReason  string
	ExistingCycleID  string
	ResultingCycleID string
	DecisionID       string
	PreemptedCycleID string
}

const (
	// minCycleIntervalSec — GET₅₀ 가 너무 작은 경우 최소 대기. 30 분 hardcoded.
	minCycleIntervalSec = 30 * 60

	// defaultPollIntervalSec — fallback polling 주기. 5 분 (Phase 1d 단축).
	defaultPollIntervalSec = 5 * 60

	// aiSchedulePrefix — AIScheduler 가 만든 feeding_schedules row 의 schedule_id prefix.
	// 운영자 수동 schedule 과 구분하기 위함. listAIManagedSchedules 가 이 prefix 로 필터.
	aiSchedulePrefix = "ai_"

	// aiScheduleCreatedBy — created_by 필드 표식. metadata_json 컬럼이 없으므로 부가 식별자.
	aiScheduleCreatedBy = "ai_scheduler"

	// firstCycleStartDelay — 첫 cycle (recent 없음) 또는 GET₅₀ 이미 지난 경우 즉시 시작 방지.
	firstCycleStartDelay = 30 * time.Minute
)

// get50Factor — GET₅₀ = GET₉₅ × ln(2)/ln(20). frontend 와 동일.
var get50Factor = math.Log(2) / math.Log(20)

// mealCapPctBW — 1회 최대 투여량(%BW). 급이 횟수 = 1일량 ÷ 1회최대량(수처리 순간부하).
// frontend species-policy.ts DEFAULT_MEAL_CAP_PCT_BW 와 동일.
const mealCapPctBW = 0.25

// CyclePlan — AI 가 계산한 다음 cycle 의 파라미터.
type CyclePlan struct {
	TargetAmountG   float64
	MaxPulses       int
	GapMs           int
	PulseDurationMs int
	SpeedRpm        int
	Amount          int
	// Get50Hours — 다음 cycle 까지 대기 시간 (시간 단위). min interval 적용 전.
	Get50Hours float64
	// DailyCycles — max_daily_cycles 과 24/GET₅₀ 의 작은 값. 24h 분할용.
	DailyCycles int
	// TempC — GET 계산에 사용한 수온 (실측·WTG 대표·마지막 유효값 중 하나).
	// 수온 미가용이면 plan 자체를 만들지 않으므로 TempMeasured 는 항상 true (2026-07-18).
	TempC        float64
	TempMeasured bool

	// 수온 급변 감산 — TargetAmountG 에 이미 반영돼 있다. 화면 노출용 근거 (2026-10-03).
	// 0 또는 1.0 이면 감산 없음.
	ReductionFactor  float64
	ReductionReason  string
	Temp24hAgoC      float64
	TargetBeforeCutG float64
}

// AIScheduler — auto 모드 수조의 cycle 자율 운영자.
// Phase 1d 부터는 feeding_schedules 테이블에 24h 전체 schedule 을 등록/갱신한다.
type AIScheduler struct {
	store storage.Store
	arb   CycleArbiter // Phase 1c 호환. Phase 1d 부터는 직접 호출하지 않음.
	log   *slog.Logger

	pollInterval time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

// NewAIScheduler — Phase 1d AI 스케줄러 생성.
func NewAIScheduler(store storage.Store, arb CycleArbiter) *AIScheduler {
	return &AIScheduler{
		store:        store,
		arb:          arb,
		log:          slog.With("service", "ai_scheduler"),
		pollInterval: time.Duration(defaultPollIntervalSec) * time.Second,
		done:         make(chan struct{}),
	}
}

// Name — runtime.Service 인터페이스.
func (a *AIScheduler) Name() string { return "ai_scheduler" }

// Start — polling fallback 시작.
func (a *AIScheduler) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	go a.pollLoop(runCtx)
	a.log.Info("ai scheduler started", "poll_interval", a.pollInterval)
	return nil
}

// Stop — polling 취소.
func (a *AIScheduler) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	select {
	case <-a.done:
	case <-ctx.Done():
	}
	return nil
}

// OnCycleComplete — cycle 종료 hook (worker.finalizeCycle 끝).
// 해당 tank 의 AI-managed schedule 을 재계산하여 DB 에 갱신.
func (a *AIScheduler) OnCycleComplete(ctx context.Context, tankID string, completedAt time.Time) {
	if a == nil {
		return
	}
	if err := a.RebuildScheduleForTank(ctx, tankID); err != nil {
		a.log.Warn("rebuild schedule after cycle complete failed", "tank_id", tankID, "error", err)
	}
}

// RebuildScheduleForTank — 해당 수조의 AI-managed schedule 을 1일 (24h) 전체로
// 재계산 + DB 갱신.
//
// 호출 시점:
//  1. backend 시작 직후 (pollLoop 초기 1회)
//  2. cycle 종료 hook 후 (OnCycleComplete)
//  3. polling (5 분마다) — operating_mode 변경 감지 + 재동기
//
// 동작:
//  1. policy 조회. operating_mode != auto → AI-managed schedule 모두 삭제.
//  2. tank profile + 정책 → CyclePlan 계산. nil 이면 schedule 모두 삭제.
//  3. 첫 fire 시각 = lastCompletedAt + GET₅₀, 없으면 now + 30 분.
//  4. 24h 균등 분할 (plan.DailyCycles 개) → times[].
//  5. 기존 AI schedule 과 비교. 동일하면 no-op. 다르면 삭제 + 신규 등록.
func (a *AIScheduler) RebuildScheduleForTank(ctx context.Context, tankID string) error {
	policy, err := a.store.GetEffectiveFeedingPolicy(ctx, tankID)
	if err != nil {
		return fmt.Errorf("get policy: %w", err)
	}
	if policy.OperatingMode != "auto" {
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	tp, err := a.store.GetTankProfile(ctx, tankID)
	if err != nil {
		return fmt.Errorf("get tank profile: %w", err)
	}
	if tp == nil {
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	tempC, tempMeasured := a.liveTempC(ctx, tankID)
	if !tempMeasured {
		// 수온 미가용(자기 수조·WTG·마지막값 모두 없음) → 14℃ fallback 폐지로 급이 스킵.
		a.log.Warn("ai schedule skipped — 수온 미가용 (fallback 14℃ 폐지)", "tank_id", tankID)
		return a.removeAIManagedSchedules(ctx, tankID)
	}
	plan := computeNextCyclePlan(a.feedingWeightProfile(ctx, tp), policy, tempC)
	if plan != nil {
		plan.TempMeasured = tempMeasured
		// 급락 감산 (2026-10-01 운영자 결정) — 24시간 낙폭이 1℃ 이상이면 그날 절반만.
		// 절대 수온보다 변화 속도에 식욕이 먼저 반응한다(9-30 하루 1.8℃ 급락 실측).
		t24, ok24 := a.tempCAgo(ctx, tankID, 24)
		t48, _ := a.tempCAgo(ctx, tankID, 48)
		if ok24 {
			prev := t24
			if f := rapidChangeFactorFor(tempC, t24, t48); f < 1.0 {
				before := plan.TargetAmountG
				plan.TargetAmountG = math.Round(plan.TargetAmountG * f)
				// 화면이 감산 전 값을 보여주던 괴리를 없애기 위해 근거를 plan 에 싣는다.
				plan.ReductionFactor = f
				plan.ReductionReason = "rapid_change_24h"
				if f == adaptationFactor {
					plan.ReductionReason = "adaptation_48h"
				}
				plan.Temp24hAgoC = prev
				plan.TargetBeforeCutG = before
				a.log.Warn("수온 급변 — 급이량 감산",
					"tank_id", tankID, "temp_now", tempC, "temp_24h_ago", math.Round(prev*100)/100,
					"change_c", math.Round((tempC-prev)*100)/100,
					"factor", f, "target_g_before", before, "target_g_after", plan.TargetAmountG)
			}
		}
	}
	if plan == nil {
		a.log.Info("ai schedule cleared — policy/profile insufficient", "tank_id", tankID,
			"species", tp.Species, "fish_count", tp.FishCount, "avg_weight_g", tp.AvgWeightG)
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	var times []string
	if validPhotoperiod(policy.PhotoperiodStartMin, policy.PhotoperiodEndMin) {
		// 2026-07-04: 같은 점등창의 auto 수조 전체를 교차 배치 (동시 급이 방지 — pH/DO
		// 순간부하). 메인 대시보드 표시(lib/feeding-schedule.ts)와 동일 알고리즘.
		entries := a.collectCrossTankEntries(ctx, policy.PhotoperiodStartMin, policy.PhotoperiodEndMin)
		times = crossTankTimes(entries, policy.PhotoperiodStartMin, policy.PhotoperiodEndMin, tankID)
		if len(times) == 0 {
			// store 조회 실패 등 — 기존 단독 배치로 degrade (schedule 소실 방지).
			times = computeDailyTimesInWindow(policy.PhotoperiodStartMin, policy.PhotoperiodEndMin, plan.DailyCycles)
		}
	} else {
		startAt := a.computeFirstFireTime(ctx, tankID, plan)
		times = computeDailyTimes(startAt, plan.DailyCycles)
	}

	existing, err := a.listAIManagedSchedules(ctx, tankID)
	if err != nil {
		return fmt.Errorf("list existing ai schedules: %w", err)
	}

	// 2026-05-20: 새 schedule 의 예상 priority (현재 autonomous mode 기반).
	// scheduleMatches 가 이 값을 기존 schedule.Priority 와 비교 — 모드 변경 시 dedupe 비활성.
	expectedPriority := "ai_advisory"
	if mode, mErr := a.store.GetTankAutonomousMode(ctx, tankID); mErr == nil && mode != nil {
		switch mode.Mode {
		case "full", "partial":
			expectedPriority = "ai_autonomous"
		}
	}
	if scheduleMatches(existing, times, plan, expectedPriority) {
		return nil
	}

	for _, s := range existing {
		if err := a.store.DeleteSchedule(ctx, s.ScheduleID); err != nil {
			a.log.Warn("delete stale ai schedule failed", "schedule_id", s.ScheduleID, "error", err)
		}
	}
	return a.createAIManagedSchedule(ctx, tankID, times, plan)
}

// removeAIManagedSchedules — 해당 tank 의 AI-managed schedule 을 모두 삭제.
// 운영자 수동 schedule 은 prefix 가 다르므로 영향 없음.
func (a *AIScheduler) removeAIManagedSchedules(ctx context.Context, tankID string) error {
	existing, err := a.listAIManagedSchedules(ctx, tankID)
	if err != nil {
		return err
	}
	for _, s := range existing {
		if err := a.store.DeleteSchedule(ctx, s.ScheduleID); err != nil {
			a.log.Warn("delete ai schedule failed", "schedule_id", s.ScheduleID, "error", err)
		}
	}
	if len(existing) > 0 {
		a.log.Info("ai schedules removed", "tank_id", tankID, "count", len(existing))
	}
	return nil
}

// listAIManagedSchedules — 해당 tank 가 포함된 AI-managed schedule (prefix `ai_`) 목록.
func (a *AIScheduler) listAIManagedSchedules(ctx context.Context, tankID string) ([]*storage.FeedingSchedule, error) {
	all, err := a.store.ListAllSchedules(ctx)
	if err != nil {
		return nil, err
	}
	var out []*storage.FeedingSchedule
	for _, s := range all {
		if !strings.HasPrefix(s.ScheduleID, aiSchedulePrefix) {
			continue
		}
		for _, t := range s.TankIDs {
			if t == tankID {
				out = append(out, s)
				break
			}
		}
	}
	return out, nil
}

// createAIManagedSchedule — 새 AI-managed schedule row 를 등록.
// schedule_id 패턴: ai_{tank_id}_{epoch_sec}.
//
// 2026-05-20: tank 의 autonomous mode 에 따라 schedule priority 결정.
//   - mode=full        → ai_autonomous  (AI 자율 실행, 운영자 승인 불필요)
//   - mode=partial     → ai_autonomous  (부분 자율 — 사료 영역은 자율 가능)
//   - mode=observation → ai_advisory    (관찰 — 운영자 결정)
//   - mode=off / 미설정 → ai_advisory    (기본 — 운영자 결정)
//
// arbiter 로그가 priority 별로 audit 가능 — 5-G Progressive Autonomy 정합.
func (a *AIScheduler) createAIManagedSchedule(ctx context.Context, tankID string, times []string, plan *CyclePlan) error {
	if len(times) == 0 {
		return nil
	}
	now := time.Now().UTC()
	target := plan.TargetAmountG

	priority := "ai_advisory"
	if mode, err := a.store.GetTankAutonomousMode(ctx, tankID); err == nil && mode != nil {
		switch mode.Mode {
		case "full", "partial":
			priority = "ai_autonomous"
		}
	}

	sched := &storage.FeedingSchedule{
		ScheduleID: fmt.Sprintf("%s%s_%d", aiSchedulePrefix, tankID, now.Unix()),
		TankIDs:    []string{tankID},
		Times:      times,
		Pattern: storage.FeedingSchedulePattern{
			PulseDurationMs: plan.PulseDurationMs,
			GapMs:           plan.GapMs,
			TotalPulses:     plan.MaxPulses,
			TargetAmountG:   &target,
		},
		Priority:   priority,
		SafetyGate: true,
		Enabled:    true,
		CreatedBy:  aiScheduleCreatedBy,
		CreatedAt:  now,
	}
	if plan.ReductionFactor > 0 && plan.ReductionFactor < 1.0 {
		tempNow, t24, before := plan.TempC, plan.Temp24hAgoC, plan.TargetBeforeCutG
		sched.Pattern.ReductionFactor = plan.ReductionFactor
		sched.Pattern.ReductionReason = plan.ReductionReason
		sched.Pattern.TempNowC = &tempNow
		sched.Pattern.Temp24hAgoC = &t24
		sched.Pattern.TargetBeforeCutG = &before
	}
	if err := a.store.UpsertSchedule(ctx, sched); err != nil {
		return fmt.Errorf("upsert ai schedule: %w", err)
	}
	a.log.Info("ai_managed schedule created", "tank_id", tankID, "schedule_id", sched.ScheduleID,
		"times", times, "daily_cycles", plan.DailyCycles,
		"target_amount_g", plan.TargetAmountG, "get50_h", plan.Get50Hours, "priority", priority,
		"temp_c", plan.TempC, "temp_measured", plan.TempMeasured)
	return nil
}

// computeFirstFireTime — 첫 cycle fire 시각.
//
//   - 마지막 완료 cycle 있음 + (completedAt + GET₅₀) 미래 → 그 시각.
//   - 그 외 (첫 cycle, GET₅₀ 이미 지남) → now + 30 분 (즉시 시작 방지).
func (a *AIScheduler) computeFirstFireTime(ctx context.Context, tankID string, plan *CyclePlan) time.Time {
	now := time.Now().UTC()
	recent, err := a.store.ListRecentFeedCycles(ctx, tankID, 1)
	if err == nil && len(recent) > 0 && recent[0].CompletedAt != nil {
		delaySec := plan.Get50Hours * 3600
		if delaySec < minCycleIntervalSec {
			delaySec = minCycleIntervalSec
		}
		next := recent[0].CompletedAt.Add(time.Duration(delaySec) * time.Second)
		if next.After(now) {
			return next.UTC()
		}
	}
	return now.Add(firstCycleStartDelay)
}

// computeDailyTimes — 첫 시각부터 24h / dailyCycles 간격으로 균등 분할한 HH:MM 목록.
// times[0] 이 24h 안에서 가장 가까운 미래 fire 시각. min interval 30 분은 cap.
func computeDailyTimes(startAt time.Time, dailyCycles int) []string {
	if dailyCycles <= 0 {
		return nil
	}
	intervalSec := int64(86400 / dailyCycles)
	if intervalSec < int64(minCycleIntervalSec) {
		intervalSec = int64(minCycleIntervalSec)
	}
	startLocal := startAt.Local()
	out := make([]string, 0, dailyCycles)
	for i := 0; i < dailyCycles; i++ {
		t := startLocal.Add(time.Duration(i) * time.Duration(intervalSec) * time.Second)
		out = append(out, t.Format("15:04"))
	}
	// 중복 제거 (분 단위 충돌 — interval < 1분인 경우는 거의 없음).
	out = dedupOrdered(out)
	sort.Strings(out)
	return out
}

// tempStaleAfter — 수온 reading 이 이보다 오래되면 stale 로 간주 (관측 주기 1분 → 15회 누락).
// RAS 순환수는 수온 급변이 없어 이 창 안의 값은 신뢰 가능.
const tempStaleAfter = 15 * time.Minute

// tankWaterTemp — 수조의 최신 water_temperature reading (값, 관측시각, 존재여부).
func (a *AIScheduler) tankWaterTemp(ctx context.Context, tankID string) (float64, time.Time, bool) {
	envs, err := a.store.ListTankEnvironment(ctx, tankID)
	if err != nil {
		return 0, time.Time{}, false
	}
	for _, e := range envs {
		if e.Metric == events.MetricWaterTemperature && e.Value != nil {
			at, _ := time.Parse(time.RFC3339Nano, e.ObservedAt)
			return *e.Value, at, true
		}
	}
	return 0, time.Time{}, false
}

// liveTempC — 급이 계산용 수온. 반환 bool=measured. fallback 14℃ 폐지(2026-07-18,
// 사용자 결정): 저수온을 적온으로 오인한 과급이 방지. 우선순위:
//
//	① 자기 수조 fresh reading
//	② 같은 WTG 다른 수조 fresh (RAS 단일 순환수 = 계통 수온 공유)
//	③ 마지막 유효 수온 (stale 허용 — RAS 는 수온 급변 없음)
//	④ 전무 → measured=false → 호출부에서 급이 스킵
func (a *AIScheduler) liveTempC(ctx context.Context, tankID string) (float64, bool) {
	now := time.Now().UTC()
	var lastVal float64
	var lastAt time.Time
	var lastFound bool
	remember := func(v float64, at time.Time) {
		if !lastFound || at.After(lastAt) {
			lastVal, lastAt, lastFound = v, at, true
		}
	}

	// ① 자기 수조
	if v, at, ok := a.tankWaterTemp(ctx, tankID); ok {
		if now.Sub(at) <= tempStaleAfter {
			return v, true
		}
		remember(v, at)
	}

	// ② 같은 WTG 다른 수조 (RAS 단일 순환수)
	if tp, err := a.store.GetTankProfile(ctx, tankID); err == nil && tp != nil && tp.WTGID != "" {
		if profiles, err := a.store.ListTankProfiles(ctx); err == nil {
			for _, p := range profiles {
				if p.TankID == tankID || p.WTGID != tp.WTGID {
					continue
				}
				if v, at, ok := a.tankWaterTemp(ctx, p.TankID); ok {
					if now.Sub(at) <= tempStaleAfter {
						return v, true
					}
					remember(v, at)
				}
			}
		}
	}

	// ③ 마지막 유효 수온 (stale 허용)
	if lastFound {
		return lastVal, true
	}

	// ④ 수온 전무 → 급이 스킵
	return 0, false
}

// feedingWeightProfile — 급이 계산용 tank profile 복사본. avg_weight_g 를 FCR 역산
// 추정 평균중량(tank_weight_history 최신 스냅샷)으로 교체한다. 추정값이 없거나 0 이면
// profile 의 정적 anchor(입식/샘플링 체중)로 폴백.
// 2026-07-17 사용자 결정: 급이 바이오매스·GET 계산을 정적 anchor → 추정중량으로 전환.
func (a *AIScheduler) feedingWeightProfile(ctx context.Context, tp *storage.TankProfile) *storage.TankProfile {
	eff := *tp
	hist, err := a.store.ListTankWeightHistory(ctx, tp.TankID, 2)
	if err == nil && len(hist) > 0 {
		// ListTankWeightHistory 는 snapshot_date ASC — 마지막 원소가 최신.
		if w := hist[len(hist)-1].EstimatedAvgWeightG; w > 0 {
			eff.AvgWeightG = w
		}
	}
	return &eff
}

// crossTankEntry — 교차 배치 대상 수조 (auto 모드 + plan 유효 + 같은 점등창).
type crossTankEntry struct {
	TankID string
	Cycles int
}

// collectCrossTankEntries — 지정 점등창을 공유하는 auto 수조들의 (tank_id, daily_cycles).
// tank_id 오름차순 — 메인 대시보드의 수조 나열 순서(tank_01, tank_02, …)와 일치.
func (a *AIScheduler) collectCrossTankEntries(ctx context.Context, startMin, endMin int) []crossTankEntry {
	profiles, err := a.store.ListTankProfiles(ctx)
	if err != nil {
		return nil
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].TankID < profiles[j].TankID })
	var out []crossTankEntry
	for _, tp := range profiles {
		pol, err := a.store.GetEffectiveFeedingPolicy(ctx, tp.TankID)
		if err != nil || pol.OperatingMode != "auto" {
			continue
		}
		if pol.PhotoperiodStartMin != startMin || pol.PhotoperiodEndMin != endMin {
			continue
		}
		entryTemp, entryMeasured := a.liveTempC(ctx, tp.TankID)
		if !entryMeasured {
			continue // 수온 미가용 수조는 교차 배치에서 제외 (급이 스킵과 일관)
		}
		plan := computeNextCyclePlan(a.feedingWeightProfile(ctx, tp), pol, entryTemp)
		if plan == nil {
			continue
		}
		out = append(out, crossTankEntry{TankID: tp.TankID, Cycles: plan.DailyCycles})
	}
	return out
}

// crossTankTimes — frontend generateCrossTankSchedule (lib/feeding-schedule.ts) 의 backend 포팅.
// 전 수조×전 급이 이벤트를 round-robin 순서로 점등창을 (총이벤트+1) 분할한 슬롯에 배치해
// 수조간 동시 급이가 없게 하고, tankID 에 배정된 시각만 반환한다. 단일 수조면 기존
// computeDailyTimesInWindow 와 동일한 결과가 된다.
func crossTankTimes(entries []crossTankEntry, startMin, endMin int, tankID string) []string {
	total := 0
	for _, e := range entries {
		total += e.Cycles
	}
	if total == 0 {
		return nil
	}
	remaining := make([]int, len(entries))
	for i, e := range entries {
		remaining[i] = e.Cycles
	}
	slot := float64(endMin-startMin) / float64(total+1)
	var mine []string
	rank := 0
	for anyLeft := true; anyLeft; {
		anyLeft = false
		for i := range entries {
			if remaining[i] <= 0 {
				continue
			}
			remaining[i]--
			anyLeft = true
			rank++
			if entries[i].TankID == tankID {
				m := int(math.Round(float64(startMin) + float64(rank)*slot))
				mine = append(mine, fmtHHMM(m))
			}
		}
	}
	mine = dedupOrdered(mine)
	sort.Strings(mine)
	return mine
}

// computeDailyTimesInWindow — 점등창 [startMin,endMin] (자정기준 분) 안에 dailyCycles 개를
// 균등 배치한 HH:MM. 급이는 소등 시 카메라 영상이 없어 점등시간대에서만 해야 한다(야간 급이 방지).
// N개를 (N+1) 분할의 안쪽 지점에 놓아 점등 직후/소등 직전에 붙지 않게 한다.
func computeDailyTimesInWindow(startMin, endMin, dailyCycles int) []string {
	if dailyCycles <= 0 {
		return nil
	}
	windowMin := endMin - startMin
	out := make([]string, 0, dailyCycles)
	for i := 0; i < dailyCycles; i++ {
		m := startMin + windowMin*(i+1)/(dailyCycles+1)
		out = append(out, fmtHHMM(m))
	}
	out = dedupOrdered(out)
	sort.Strings(out)
	return out
}

// validPhotoperiod — 점등창이 유효한지 (0<=start<end<=24h).
func validPhotoperiod(startMin, endMin int) bool {
	return startMin >= 0 && endMin > startMin && endMin <= 24*60
}

// fmtHHMM — 자정기준 분 → "15:04".
func fmtHHMM(min int) string {
	min = ((min % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

func dedupOrdered(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// scheduleMatches — 기존 AI schedule 들이 새로 계산된 times + plan 과 동일한지 검사.
// 동일하면 no-op (DB 쓰기 방지).
func scheduleMatches(existing []*storage.FeedingSchedule, times []string, plan *CyclePlan, priority string) bool {
	if len(existing) != 1 {
		return false
	}
	s := existing[0]
	if !s.Enabled {
		return false
	}
	// 2026-05-20: autonomous mode 변경 시 schedule priority 도 바뀌어야 하므로 비교 추가.
	if s.Priority != priority {
		return false
	}
	sortedExisting := append([]string(nil), s.Times...)
	sort.Strings(sortedExisting)
	sortedNew := append([]string(nil), times...)
	sort.Strings(sortedNew)
	if !reflect.DeepEqual(sortedExisting, sortedNew) {
		return false
	}
	if s.Pattern.PulseDurationMs != plan.PulseDurationMs {
		return false
	}
	if s.Pattern.GapMs != plan.GapMs {
		return false
	}
	if s.Pattern.TotalPulses != plan.MaxPulses {
		return false
	}
	if s.Pattern.TargetAmountG == nil || math.Abs(*s.Pattern.TargetAmountG-plan.TargetAmountG) > 0.5 {
		return false
	}
	// 감산 근거도 비교한다 (2026-10-03). 목표량이 우연히 같아도 계수·사유가 달라지면
	// 화면 배너가 옛 근거를 계속 보여주므로 재생성해야 한다.
	if normalizedFactor(s.Pattern.ReductionFactor) != normalizedFactor(plan.ReductionFactor) {
		return false
	}
	return true
}

// normalizedFactor — 감산 없음을 0 과 1.0 둘 다로 표현할 수 있어 비교 전에 1.0 으로 모은다.
func normalizedFactor(f float64) float64 {
	if f <= 0 || f >= 1.0 {
		return 1.0
	}
	return f
}

// PolicyOverride — F.2 운영자가 LLM 분석을 명시적으로 적용할 때 사용하는 1회성 override.
// nil 이면 기존 RebuildScheduleForTank 와 동일 동작.
// tank metadata 영구 변경 없음 — 다음 GET100 cycle 재계산 시 원래 정책으로 복귀.
type PolicyOverride struct {
	MaxDailyCyclesOverride *int
	BsfModeOverride        *string
	GetFactor              *float64
	MinIntervalMin         *int
}

// RebuildScheduleForTankWithOverride — 1회성 override 를 적용하여 AI-managed schedule 을 재계산.
// override == nil 이면 RebuildScheduleForTank 와 동일 동작.
func (a *AIScheduler) RebuildScheduleForTankWithOverride(ctx context.Context, tankID string, override *PolicyOverride) error {
	if override == nil {
		return a.RebuildScheduleForTank(ctx, tankID)
	}

	policy, err := a.store.GetEffectiveFeedingPolicy(ctx, tankID)
	if err != nil {
		return fmt.Errorf("get policy: %w", err)
	}
	if policy.OperatingMode != "auto" {
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	tp, err := a.store.GetTankProfile(ctx, tankID)
	if err != nil {
		return fmt.Errorf("get tank profile: %w", err)
	}
	if tp == nil {
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	// override 값 우선 적용 (policy 복사 후 덮어쓰기).
	overridePolicy := *policy
	if override.MaxDailyCyclesOverride != nil {
		overridePolicy.MaxDailyCycles = *override.MaxDailyCyclesOverride
	}
	if override.BsfModeOverride != nil {
		overridePolicy.BsfMode = *override.BsfModeOverride
	}

	overrideTemp, overrideMeasured := a.liveTempC(ctx, tankID)
	if !overrideMeasured {
		a.log.Warn("ai schedule (override) skipped — 수온 미가용 (fallback 14℃ 폐지)", "tank_id", tankID)
		return a.removeAIManagedSchedules(ctx, tankID)
	}
	plan := computeNextCyclePlan(a.feedingWeightProfile(ctx, tp), &overridePolicy, overrideTemp)
	if plan == nil {
		a.log.Info("ai schedule cleared — override policy/profile insufficient", "tank_id", tankID)
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	// get_factor override — GET₅₀ 배율 조정.
	intervalAdjusted := false
	if override.GetFactor != nil && *override.GetFactor > 0 {
		plan.Get50Hours = plan.Get50Hours * *override.GetFactor
		intervalAdjusted = true
	}

	// min_interval_min override — 최소 간격 enforce (초 변환).
	effectiveMinIntervalSec := float64(minCycleIntervalSec)
	if override.MinIntervalMin != nil && *override.MinIntervalMin > 0 {
		effectiveMinIntervalSec = float64(*override.MinIntervalMin * 60)
	}
	if plan.Get50Hours*3600 < effectiveMinIntervalSec {
		plan.Get50Hours = effectiveMinIntervalSec / 3600.0
		intervalAdjusted = true
	}

	// daily cycles: 기본은 plan(meal_cap 기반, base 경로와 동일).
	// 단 operator 가 get_factor/min_interval 로 급이 간격을 명시 조정한 경우에만 GET 간격 기반 재계산.
	if intervalAdjusted {
		maxDaily := overridePolicy.MaxDailyCycles
		if maxDaily <= 0 {
			maxDaily = 4
		}
		dailyCycles := int(math.Floor(24.0 / plan.Get50Hours))
		if dailyCycles < 1 {
			dailyCycles = 1
		}
		if dailyCycles > maxDaily {
			dailyCycles = maxDaily
		}
		plan.DailyCycles = dailyCycles
	}

	var times []string
	if validPhotoperiod(overridePolicy.PhotoperiodStartMin, overridePolicy.PhotoperiodEndMin) {
		times = computeDailyTimesInWindow(overridePolicy.PhotoperiodStartMin, overridePolicy.PhotoperiodEndMin, plan.DailyCycles)
	} else {
		startAt := a.computeFirstFireTimeWithMinInterval(ctx, tankID, plan, effectiveMinIntervalSec)
		times = computeDailyTimes(startAt, plan.DailyCycles)
	}

	existing, err := a.listAIManagedSchedules(ctx, tankID)
	if err != nil {
		return fmt.Errorf("list existing ai schedules: %w", err)
	}
	for _, s := range existing {
		if err := a.store.DeleteSchedule(ctx, s.ScheduleID); err != nil {
			a.log.Warn("delete stale ai schedule (override) failed", "schedule_id", s.ScheduleID, "error", err)
		}
	}
	a.log.Info("ai schedule rebuilt with override", "tank_id", tankID,
		"daily_cycles", plan.DailyCycles, "get50_h", plan.Get50Hours,
		"override_max_daily", override.MaxDailyCyclesOverride,
		"override_bsf_mode", override.BsfModeOverride,
		"override_get_factor", override.GetFactor,
		"override_min_interval_min", override.MinIntervalMin)
	return a.createAIManagedSchedule(ctx, tankID, times, plan)
}

// computeFirstFireTimeWithMinInterval — override min_interval_sec 를 적용한 첫 fire 시각.
func (a *AIScheduler) computeFirstFireTimeWithMinInterval(ctx context.Context, tankID string, plan *CyclePlan, minIntervalSec float64) time.Time {
	now := time.Now().UTC()
	recent, err := a.store.ListRecentFeedCycles(ctx, tankID, 1)
	if err == nil && len(recent) > 0 && recent[0].CompletedAt != nil {
		delaySec := plan.Get50Hours * 3600
		if delaySec < minIntervalSec {
			delaySec = minIntervalSec
		}
		next := recent[0].CompletedAt.Add(time.Duration(delaySec) * time.Second)
		if next.After(now) {
			return next.UTC()
		}
	}
	return now.Add(firstCycleStartDelay)
}

// pollLoop — 5 분마다 모든 auto 수조 schedule 재동기 (정책 변경 감지 + chain 끊김 복구).
func (a *AIScheduler) pollLoop(ctx context.Context) {
	defer close(a.done)
	a.checkAllTanks(ctx)

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAllTanks(ctx)
		}
	}
}

func (a *AIScheduler) checkAllTanks(ctx context.Context) {
	tanks, err := a.store.ListTankProfiles(ctx)
	if err != nil {
		a.log.Warn("polling: list tanks failed", "error", err)
		return
	}
	for _, tp := range tanks {
		if err := a.RebuildScheduleForTank(ctx, tp.TankID); err != nil {
			a.log.Warn("polling rebuild failed", "tank_id", tp.TankID, "error", err)
		}
	}
}

// --- 수온 급이율 인덱스 (대서양연어, vault 정본) ------------------------------

// salmonGrowoutFeedRatePct — 수온(℃) → 일일 총 급이율(%BW). vault 대서양연어 정본.
// 온도단계 S1~S5 기반 (2026-07-18 사용자 확정):
//
//	T < 8℃         → 0 (중단)      S1(<4)·S2(4~8) — 저온 대사정지, 급이 스킵
//	8 ≤ T ≤ 14℃    → 0.5→1.5 선형   S3 최적성장 (Handeland: feed intake 는 수온 증가)
//	14 < T ≤ 18℃   → 0.8→0 선형     S4 고온경계 — 선제 감량 (14℃ 진입 시 1.5→0.8 급감)
//	T > 18℃        → 0 (중단)       S5 — 냉수성 고온 위험
//
// bsfMode 배율은 별도(bsfModeMultiplier). GET(분할 간격)과 독립된 총량 축.
func salmonGrowoutFeedRatePct(t float64) float64 {
	switch {
	case t < 8.0:
		return 0
	case t <= 14.0:
		return 0.5 + (t-8.0)/6.0*1.0
	case t <= 18.0:
		return 0.8 * (18.0 - t) / 4.0
	default:
		return 0
	}
}

// bsfModeMultiplier — 운영 정책 bsf_mode → 급이율 배율 (곡선 위에 얹음).
func bsfModeMultiplier(mode string) float64 {
	switch mode {
	case "aggressive":
		return 1.3
	case "conservative":
		return 0.75
	default: // standard
		return 1.0
	}
}

// --- 정책 계산 ---------------------------------------------------------------

// computeNextCyclePlan — tank profile + 정책 → CyclePlan.
// frontend lib/species-policy.ts 의 computeFeedingPolicy 와 동일한 결과를 backend 에서 재현.
//
// 계획 급이량 계산식
//
//	하루 계획량(g) = 마릿수 × 평균 체중(g) × 급이율(%BW) / 100
//	회당 계획량(g) = 하루 계획량 ÷ 하루 횟수
//
//	평균 체중 = 추정 체중 (feedingWeightProfile, 2026-07-17~; 그 전은 입식·샘플링 고정값)
//	          = 기준 체중 + 기준일 이후 누적 급이 기록(g) ÷ FCR ÷ 입식 마릿수
//	            (FCR = 수조별 보정값(tank FCR calibration)이 있으면 그 값, 없으면 어종·단계 표 기본값.
//	             강릉 실증: 07-17까지 1.5(표 조회 실패 폴백) → 이후 보정값 1.8)
//	            (기준 = 최근 샘플링, 없으면 입식. 누적 급이 = feeding.recorded 합 = 시스템 실행 기록)
//	급이율(%BW), 성어(500g~) 대서양연어:
//	  2026-07-18~ 수온 곡선 × 모드 배율 (안정적 conservative 0.75 · 일반 standard 1.0 · 공격적 aggressive 1.3)
//	              수온 곡선: 8~14℃ 0.5→1.5% 선형, 14~18℃ 감량, 18℃ 초과 중단,
//	              8℃ 미만은 coldWaterRatePct (6℃ 까지, 2026-10-01~)
//	  ~2026-07-17 모드별 고정값 (안정적 1.0 · 일반 1.5 · 공격적 2.0%)
//	하루 횟수 = ceil(하루 계획량 ÷ 1회 최대량(0.25%BW)), 운영자 지정(daily_cycles)이 있으면 그 값
//
// 시스템 실행 기록(급이량 장부)은 feed_cycle 의 펄스 가동시간 × 배출률(9.4 g/s) 추정이다
// (worker.go estimatePerPulse). 저울 실측이 아니므로 추정 체중도 그 오차를 그대로 따른다.
func computeNextCyclePlan(tp *storage.TankProfile, policy *storage.FeedingPolicy, tempC float64) *CyclePlan {
	pol, ok := speciesPolicyTable[tp.Species]
	if !ok {
		return nil
	}
	if tp.FishCount <= 0 || tp.AvgWeightG <= 0 {
		return nil
	}
	stage := findStage(pol, tp.AvgWeightG)
	if stage == "" {
		return nil
	}
	stagePol, ok := pol.Stages[stage]
	if !ok {
		return nil
	}

	bsfMode := policy.BsfMode
	if bsfMode == "" {
		bsfMode = "standard"
	}
	bsfPct, ok := stagePol.BsfPercentBWPerDay[bsfMode]
	if !ok || bsfPct <= 0 {
		return nil
	}

	// 수온 급이율 인덱스 (2026-07-18, 사용자 결정) — 대서양연어 growout 만.
	// BSF(일일 총 급이율)를 vault 정본 수온-급이율 곡선으로 대체. bsfMode 는 배율로 유지.
	// GET(분할 간격)과 독립 축(총량). S2(<8℃)·S5(>18℃) 중단, S4(14~18℃) 고온감량.
	if pol.SalmonAlpha > 0 && stage == "growout" {
		base := salmonGrowoutFeedRatePct(tempC)
		if base <= 0 {
			// 8℃ 미만은 저수온 곡선이 6℃까지 이어받는다 (2026-10-01 운영자 결정).
			// 8.0℃ 에서 0.50% 로 맞물려 종전의 절벽이 사라진다. 6℃ 미만·18℃ 초과는 중단.
			base = coldWaterRatePct(tempC)
		}
		if base <= 0 {
			return nil
		}
		bsfPct = base * bsfModeMultiplier(bsfMode)
	}

	biomassG := float64(tp.FishCount) * tp.AvgWeightG
	biomassKg := biomassG / 1000.0
	dailyFeedG := biomassKg * (bsfPct / 100.0) * 1000.0
	if dailyFeedG <= 0 {
		return nil
	}

	maxDaily := policy.MaxDailyCycles
	if maxDaily <= 0 {
		maxDaily = 4
	}
	// 급이 횟수 = 1일량 ÷ 1회최대량(수처리 순간부하). frontend species-policy.ts 와 동일.
	// GET(소화간격)은 참고·최소간격 검증용이며 횟수 하드제약이 아님.
	mealMaxG := biomassKg * (mealCapPctBW / 100.0) * 1000.0
	dailyCycles := 1
	if mealMaxG > 0 {
		dailyCycles = int(math.Ceil(dailyFeedG / mealMaxG))
	}
	if dailyCycles < 1 {
		dailyCycles = 1
	}
	if dailyCycles > maxDaily {
		dailyCycles = maxDaily
	}
	// 운영자 직접 지정(정책 daily_cycles, 2026-07-07) — 자동 산출·maxDaily 를 대체한다.
	// 1일 총량은 불변, 회당 목표만 분할이 바뀐다. 안전 상한 12 (점등창 내 배치 한계).
	if policy.DailyCycles > 0 {
		dailyCycles = policy.DailyCycles
		if dailyCycles > 12 {
			dailyCycles = 12
		}
	}
	// 저수온에서는 횟수를 줄여 회당 양을 물고기가 인지할 크기로 유지한다.
	// 1회최대량을 같이 넘겨 회당 양이 상한을 넘도록 줄어드는 것을 막는다 (2026-10-10).
	dailyCycles = coldDailyCycles(dailyCycles, tempC, dailyFeedG, mealMaxG)
	cycleTargetG := dailyFeedG / float64(dailyCycles)

	// GET95 — vault 대서양연어 정본 BW 모델 우선: GET95 = α·BW_g^β·e^(−γ·T).
	// BW 파라미터가 없거나 체중 미상이면 일반 meal 기반 폴백. frontend computeGet95 와 동일.
	mealPctBW := (cycleTargetG / biomassKg) * 0.1
	var get95 float64
	if pol.SalmonAlpha > 0 && tp.AvgWeightG > 0 {
		get95 = pol.SalmonAlpha * math.Pow(tp.AvgWeightG, pol.SalmonBeta) * math.Exp(-pol.SalmonGamma*tempC)
	} else {
		get95 = pol.GETBase * math.Pow(pol.Q10, (pol.RefTempC-tempC)/10.0) * math.Pow(mealPctBW/pol.RefMealPctBW, pol.MealExponent)
	}
	get50 := get95 * get50Factor
	if get50 <= 0 {
		return nil
	}

	// 대시보드 마이크로펄스 입력(그룹정책 feeder_run_sec/feeder_stop_sec) 우선 —
	// 설정된 경우 종별 기본 패턴을 대체하고, 펄스 수는 회당 목표량/공급률로 재계산.
	pulseMs := stagePol.Pattern.PulseDurationMs
	gapMs := stagePol.Pattern.GapMs
	maxPulses := stagePol.Pattern.TotalPulses
	if policy.FeederRunSec > 0 {
		pulseMs = policy.FeederRunSec * 1000
		burstG := float64(policy.FeederRunSec) * defaultFeederRateGPS
		maxPulses = int(math.Ceil(cycleTargetG / burstG))
		if maxPulses < 1 {
			maxPulses = 1
		}
	}
	if policy.FeederStopSec > 0 {
		gapMs = policy.FeederStopSec * 1000
	}
	// 저수온 보정 — 대사가 느려진 배수만큼 간격을 늘려 앞 펄스를 먹을 시간을 준다.
	// 상한 3분. 배출 판정 창(펄스 시작 +10~30초)은 간격과 무관하므로 영향 없다.
	// ⚠ "간격 끝 복귀도" 지표는 간격이 길어지면 더 가라앉은 것처럼 보인다 —
	//   사이클마다 실제 사용한 간격을 기록해 사후 보정할 수 있게 한다(GapMs 노출).
	gapMs = coldGapMs(gapMs, tempC)

	return &CyclePlan{
		TargetAmountG:   math.Round(cycleTargetG),
		MaxPulses:       maxPulses,
		GapMs:           gapMs,
		PulseDurationMs: pulseMs,
		SpeedRpm:        stagePol.Pattern.SpeedRpm,
		Amount:          stagePol.Pattern.Amount,
		Get50Hours:      get50,
		DailyCycles:     dailyCycles,
		TempC:           tempC,
	}
}

// --- species policy table (frontend lib/species-policy.ts 와 동기화) ----------

type stagePattern struct {
	PulseDurationMs int
	GapMs           int
	TotalPulses     int
	SpeedRpm        int
	Amount          int
}

type stagePolicyEntry struct {
	WeightRangeG       [2]float64
	BsfPercentBWPerDay map[string]float64
	Pattern            stagePattern
}

type speciesPolicyEntry struct {
	Stages       map[string]stagePolicyEntry
	GETBase      float64
	Q10          float64
	RefTempC     float64
	RefMealPctBW float64
	MealExponent float64
	// vault 대서양연어 GET 정본 BW 모델: GET95 = α·BW_g^β·e^(−γ·T). 0 이면 미설정(폴백).
	SalmonAlpha float64
	SalmonBeta  float64
	SalmonGamma float64
}

var speciesPolicyTable = map[string]speciesPolicyEntry{
	"atlantic_salmon": {
		Stages: map[string]stagePolicyEntry{
			"fry": {
				WeightRangeG: [2]float64{0, 100},
				BsfPercentBWPerDay: map[string]float64{
					"aggressive":   5.0,
					"standard":     4.0,
					"conservative": 3.0,
				},
				Pattern: stagePattern{
					PulseDurationMs: 1500,
					GapMs:           45_000,
					TotalPulses:     3,
					SpeedRpm:        18,
					Amount:          80,
				},
			},
			"fingerling": {
				WeightRangeG: [2]float64{100, 500},
				BsfPercentBWPerDay: map[string]float64{
					"aggressive":   3.0,
					"standard":     2.5,
					"conservative": 2.0,
				},
				Pattern: stagePattern{
					PulseDurationMs: 2000,
					GapMs:           60_000,
					TotalPulses:     5,
					SpeedRpm:        24,
					Amount:          130,
				},
			},
			"growout": {
				WeightRangeG: [2]float64{500, 5000},
				BsfPercentBWPerDay: map[string]float64{
					"aggressive":   2.0,
					"standard":     1.5,
					"conservative": 1.0,
				},
				// 2026-07-04 강릉 실측 보정: onoff 급이기 공급모터 리드(11s)와 함께
				// 공급 3s + 정지 6s 마이크로펄스로 운영 (사용자 결정). frontend species-policy.ts 와 동기.
				Pattern: stagePattern{
					PulseDurationMs: 3000,
					GapMs:           6_000,
					TotalPulses:     6,
					SpeedRpm:        32,
					Amount:          180,
				},
			},
		},
		GETBase:      24.0,
		Q10:          2.0,
		RefTempC:     18.0,
		RefMealPctBW: 1.0,
		MealExponent: 0.5,
		// vault Atlantic Salmon GET Model Report 정본 (α=16.3, β=0.31, γ=0.11).
		SalmonAlpha: 16.3,
		SalmonBeta:  0.31,
		SalmonGamma: 0.11,
	},
}

// findStage — avg_weight_g 가 어느 단계에 속하는지 판정.
func findStage(pol speciesPolicyEntry, avgWeightG float64) string {
	order := []string{"fry", "fingerling", "growout"}
	for _, k := range order {
		s, ok := pol.Stages[k]
		if !ok {
			continue
		}
		if avgWeightG >= s.WeightRangeG[0] && avgWeightG < s.WeightRangeG[1] {
			return k
		}
	}
	if g, ok := pol.Stages["growout"]; ok && avgWeightG >= g.WeightRangeG[0] {
		return "growout"
	}
	return ""
}
