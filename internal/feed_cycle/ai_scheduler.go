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

	// defaultFallbackTempC — 수온 sensor 없을 때 사용. frontend 와 동일.
	defaultFallbackTempC = 14.0

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

	plan := computeNextCyclePlan(tp, policy)
	if plan == nil {
		a.log.Info("ai schedule cleared — policy/profile insufficient", "tank_id", tankID,
			"species", tp.Species, "fish_count", tp.FishCount, "avg_weight_g", tp.AvgWeightG)
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	startAt := a.computeFirstFireTime(ctx, tankID, plan)
	times := computeDailyTimes(startAt, plan.DailyCycles)

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
	if err := a.store.UpsertSchedule(ctx, sched); err != nil {
		return fmt.Errorf("upsert ai schedule: %w", err)
	}
	a.log.Info("ai_managed schedule created", "tank_id", tankID, "schedule_id", sched.ScheduleID,
		"times", times, "daily_cycles", plan.DailyCycles,
		"target_amount_g", plan.TargetAmountG, "get50_h", plan.Get50Hours, "priority", priority)
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
	return true
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

	plan := computeNextCyclePlan(tp, &overridePolicy)
	if plan == nil {
		a.log.Info("ai schedule cleared — override policy/profile insufficient", "tank_id", tankID)
		return a.removeAIManagedSchedules(ctx, tankID)
	}

	// get_factor override — GET₅₀ 배율 조정.
	if override.GetFactor != nil && *override.GetFactor > 0 {
		plan.Get50Hours = plan.Get50Hours * *override.GetFactor
	}

	// min_interval_min override — 최소 간격 enforce (초 변환).
	effectiveMinIntervalSec := float64(minCycleIntervalSec)
	if override.MinIntervalMin != nil && *override.MinIntervalMin > 0 {
		effectiveMinIntervalSec = float64(*override.MinIntervalMin * 60)
	}
	if plan.Get50Hours*3600 < effectiveMinIntervalSec {
		plan.Get50Hours = effectiveMinIntervalSec / 3600.0
	}

	// get_factor/min_interval 반영 후 daily cycles 재계산.
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

	startAt := a.computeFirstFireTimeWithMinInterval(ctx, tankID, plan, effectiveMinIntervalSec)
	times := computeDailyTimes(startAt, plan.DailyCycles)

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

// --- 정책 계산 ---------------------------------------------------------------

// computeNextCyclePlan — tank profile + 정책 → CyclePlan.
// frontend lib/species-policy.ts 의 computeFeedingPolicy 와 동일한 결과를 backend 에서 재현.
func computeNextCyclePlan(tp *storage.TankProfile, policy *storage.FeedingPolicy) *CyclePlan {
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

	biomassG := float64(tp.FishCount) * tp.AvgWeightG
	biomassKg := biomassG / 1000.0
	dailyFeedG := biomassKg * (bsfPct / 100.0) * 1000.0
	if dailyFeedG <= 0 {
		return nil
	}

	estimatePerCycleG := dailyFeedG / 3.0
	mealPctBW := (estimatePerCycleG / biomassKg) * 0.1

	tempC := defaultFallbackTempC
	get95 := pol.GETBase * math.Pow(pol.Q10, (pol.RefTempC-tempC)/10.0) * math.Pow(mealPctBW/pol.RefMealPctBW, pol.MealExponent)
	get50 := get95 * get50Factor
	if get50 <= 0 {
		return nil
	}

	maxDaily := policy.MaxDailyCycles
	if maxDaily <= 0 {
		maxDaily = 4
	}
	dailyCycles := int(math.Floor(24.0 / get50))
	if dailyCycles < 1 {
		dailyCycles = 1
	}
	if dailyCycles > maxDaily {
		dailyCycles = maxDaily
	}
	cycleTargetG := dailyFeedG / float64(dailyCycles)

	return &CyclePlan{
		TargetAmountG:   math.Round(cycleTargetG),
		MaxPulses:       stagePol.Pattern.TotalPulses,
		GapMs:           stagePol.Pattern.GapMs,
		PulseDurationMs: stagePol.Pattern.PulseDurationMs,
		SpeedRpm:        stagePol.Pattern.SpeedRpm,
		Amount:          stagePol.Pattern.Amount,
		Get50Hours:      get50,
		DailyCycles:     dailyCycles,
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
				Pattern: stagePattern{
					PulseDurationMs: 2500,
					GapMs:           75_000,
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
