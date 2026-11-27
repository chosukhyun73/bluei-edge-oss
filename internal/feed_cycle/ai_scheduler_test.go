package feed_cycle

// Phase 1c — AIScheduler.computeNextCyclePlan unit tests.
//
// frontend lib/species-policy.ts 의 computeFeedingPolicy 와 동일 결과를 backend
// 에서 재현하는지 검증. atlantic_salmon × {fry, fingerling, growout} × {standard,
// aggressive, conservative} 의 대표 케이스 + 단계 미판정 / 종 미지원 / 정보 부족
// 의 negative 경로.

import (
	"math"
	"reflect"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

// approxEqual — 부동소수점 비교 (소수 4째자리까지).
func approxEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

// TestSalmonGrowoutFeedRatePct — vault 정본 수온-급이율 곡선 (2026-07-18).
func TestSalmonGrowoutFeedRatePct(t *testing.T) {
	cases := []struct {
		t    float64
		want float64
	}{
		{3.9, 0.0},     // S1 저온 중단
		{7.9, 0.0},     // S2 저온 중단 (사용자 결정: 8℃ 미만 중단)
		{8.0, 0.5},     // S3 하단
		{10.79, 0.965}, // S3 현재 운영 수온 (라이브 conservative≈1.0%와 근사)
		{11.0, 1.0},    // S3 중간
		{14.0, 1.5},    // S3 상단
		{16.0, 0.4},    // S4 고온 감량: 0.8×(18-16)/4 = 0.4
		{18.0, 0.0},    // S4/S5 경계
		{19.0, 0.0},    // S5 고온 중단
	}
	for _, c := range cases {
		got := salmonGrowoutFeedRatePct(c.t)
		if !approxEqual(got, c.want, 0.01) {
			t.Errorf("salmonGrowoutFeedRatePct(%.2f): got %.3f, want %.3f", c.t, got, c.want)
		}
	}
}

// TestBsfModeMultiplier — 곡선 위 mode 배율 유지 (2026-07-18).
func TestBsfModeMultiplier(t *testing.T) {
	cases := map[string]float64{
		"aggressive":   1.3,
		"standard":     1.0,
		"conservative": 0.75,
		"":             1.0, // 기본 = standard
		"unknown":      1.0,
	}
	for mode, want := range cases {
		if got := bsfModeMultiplier(mode); got != want {
			t.Errorf("bsfModeMultiplier(%q): got %v, want %v", mode, got, want)
		}
	}
}

// TestComputeNextCyclePlan_GrowoutTempIndexed — growout 이 수온곡선×배율을 쓰는지.
func TestComputeNextCyclePlan_GrowoutTempIndexed(t *testing.T) {
	tp := &storage.TankProfile{
		Species: "atlantic_salmon", FishCount: 100, AvgWeightG: 1000,
	}
	// DailyCycles=8 고정(운영자 지정)으로 분할을 같게 두면 target 비율 = mode 배율.
	// T=11℃: 곡선 1.0%. conservative(0.75×) daily=750 g / 8 = 93.75.
	//                    aggressive(1.3×)  daily=1300 g / 8 = 162.5.
	plan := computeNextCyclePlan(tp, &storage.FeedingPolicy{
		BsfMode: "conservative", OperatingMode: "auto", MaxDailyCycles: 8, DailyCycles: 8,
	}, 11.0)
	if plan == nil {
		t.Fatal("expected plan at 11℃, got nil")
	}
	planAgg := computeNextCyclePlan(tp, &storage.FeedingPolicy{
		BsfMode: "aggressive", OperatingMode: "auto", MaxDailyCycles: 8, DailyCycles: 8,
	}, 11.0)
	if planAgg == nil {
		t.Fatal("expected plan (aggressive), got nil")
	}
	ratio := planAgg.TargetAmountG / plan.TargetAmountG
	if !approxEqual(ratio, 1.3/0.75, 0.05) {
		t.Errorf("mode multiplier ratio: got %.3f, want ~%.3f", ratio, 1.3/0.75)
	}

	// T=7℃ — 2026-10-01 부터 저수온 곡선이 6℃까지 이어받는다(운영자 결정). 급이 유지.
	// 종전에는 8℃ 미만이 일괄 중단이었고 이 테스트도 nil 을 기대했다.
	if p := computeNextCyclePlan(tp, &storage.FeedingPolicy{
		BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 8, DailyCycles: 8,
	}, 7.0); p == nil {
		t.Error("expected plan at 7℃ (저수온 급이 구간), got nil")
	} else if p.DailyCycles != 2 {
		t.Errorf("9℃ 미만은 하루 2회로 축소돼야 한다: got %d", p.DailyCycles)
	}
	// T=5.5℃ — 하한(6℃) 아래 → 중단.
	if p := computeNextCyclePlan(tp, &storage.FeedingPolicy{
		BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 8, DailyCycles: 8,
	}, 5.5); p != nil {
		t.Errorf("expected nil plan at 5.5℃ (하한 미만), got %+v", p)
	}
	// T=19℃ (S5) → 중단 → plan nil.
	if p := computeNextCyclePlan(tp, &storage.FeedingPolicy{
		BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 8, DailyCycles: 8,
	}, 19.0); p != nil {
		t.Errorf("expected nil plan at 19℃ (중단 대역), got %+v", p)
	}
}

func TestComputeNextCyclePlan_GrowoutStandard(t *testing.T) {
	// tank_01 growout standard, T=14℃ fallback, max_daily_cycles=4.
	//
	// fish_count=100, avg_weight_g=1000 → biomass_kg=100, biomass_g=100_000.
	// bsf_pct = 1.5 (growout standard) → daily_feed_g = 100 × 0.015 × 1000 = 1500.
	// daily_cycles = meal_cap 기반: meal_max = 100 × 0.25% × 1000 = 250 g.
	//              = min(4, ceil(1500/250)) = min(4, 6) = 4.
	// cycle_target_g = 1500/4 = 375 → Round(375) = 375.
	// GET95 — vault 정본 BW 모델: 16.3 × 1000^0.31 × e^(−0.11×14)
	//       ≈ 16.3 × 8.512 × 0.2144 ≈ 29.74 h (vault 참조표 1kg@14℃=29.8 일치).
	// GET50 = GET95 × ln(2)/ln(20) ≈ 29.74 × 0.23137 ≈ 6.882 h.
	tp := &storage.TankProfile{
		TankID:     "tank_01",
		Species:    "atlantic_salmon",
		FishCount:  100,
		AvgWeightG: 1000,
		VolumeM3:   20,
	}
	pol := &storage.FeedingPolicy{
		BsfMode:        "standard",
		OperatingMode:  "auto",
		MaxDailyCycles: 4,
	}

	plan := computeNextCyclePlan(tp, pol, 14.0)
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	// target = 375 g (1500 / 4 cycles).
	if !approxEqual(plan.TargetAmountG, 375, 0.5) {
		t.Errorf("target_amount_g: got %v, want ~375", plan.TargetAmountG)
	}
	// GET50 ≈ 6.882 h (vault 정본 BW 모델).
	if !approxEqual(plan.Get50Hours, 6.882, 0.05) {
		t.Errorf("get50_h: got %v, want ~6.882", plan.Get50Hours)
	}
	// growout pattern default (2026-07-04 강릉 실측 보정: 공급 3s + 정지 6s).
	if plan.PulseDurationMs != 3000 || plan.GapMs != 6_000 || plan.MaxPulses != 6 ||
		plan.SpeedRpm != 32 || plan.Amount != 180 {
		t.Errorf("growout pattern mismatch: %+v", plan)
	}
}

func TestComputeNextCyclePlan_FingerlingAggressive(t *testing.T) {
	// fish_count=1000, avg_weight_g=200 → biomass_kg=200.
	// bsf_pct = 3.0 (fingerling aggressive) → daily_feed_g = 200 × 0.03 × 1000 = 6000.
	tp := &storage.TankProfile{
		Species:    "atlantic_salmon",
		FishCount:  1000,
		AvgWeightG: 200,
	}
	pol := &storage.FeedingPolicy{
		BsfMode:        "aggressive",
		OperatingMode:  "auto",
		MaxDailyCycles: 4,
	}
	plan := computeNextCyclePlan(tp, pol, 14.0)
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	// fingerling pattern.
	if plan.PulseDurationMs != 2000 || plan.GapMs != 60_000 || plan.MaxPulses != 5 ||
		plan.SpeedRpm != 24 || plan.Amount != 130 {
		t.Errorf("fingerling pattern mismatch: %+v", plan)
	}
	// daily_feed = 6000, 분할 후 target 양수.
	if plan.TargetAmountG <= 0 {
		t.Errorf("target_amount_g should be > 0, got %v", plan.TargetAmountG)
	}
	// GET50 > 0.
	if plan.Get50Hours <= 0 {
		t.Errorf("get50_h should be > 0, got %v", plan.Get50Hours)
	}
}

func TestComputeNextCyclePlan_FryConservative(t *testing.T) {
	tp := &storage.TankProfile{
		Species:    "atlantic_salmon",
		FishCount:  2000,
		AvgWeightG: 50,
	}
	pol := &storage.FeedingPolicy{
		BsfMode:        "conservative",
		OperatingMode:  "auto",
		MaxDailyCycles: 4,
	}
	plan := computeNextCyclePlan(tp, pol, 14.0)
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	// fry pattern.
	if plan.PulseDurationMs != 1500 || plan.GapMs != 45_000 || plan.MaxPulses != 3 ||
		plan.SpeedRpm != 18 || plan.Amount != 80 {
		t.Errorf("fry pattern mismatch: %+v", plan)
	}
}

func TestComputeNextCyclePlan_UnsupportedSpecies(t *testing.T) {
	tp := &storage.TankProfile{
		Species:    "olive_flounder", // 정책 사전 미정의
		FishCount:  100,
		AvgWeightG: 200,
	}
	pol := &storage.FeedingPolicy{BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 4}
	if plan := computeNextCyclePlan(tp, pol, 14.0); plan != nil {
		t.Errorf("expected nil for unsupported species, got %+v", plan)
	}
}

func TestComputeNextCyclePlan_MissingStockingInfo(t *testing.T) {
	tp := &storage.TankProfile{
		Species:    "atlantic_salmon",
		FishCount:  0, // 입식 정보 없음
		AvgWeightG: 0,
	}
	pol := &storage.FeedingPolicy{BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 4}
	if plan := computeNextCyclePlan(tp, pol, 14.0); plan != nil {
		t.Errorf("expected nil for missing stocking, got %+v", plan)
	}
}

// 체중 누락(마리수는 유효)이면 스케줄을 만들지 않아야 한다. 위 MissingStockingInfo 는
// FishCount 도 0 이라 체중 단독 누락 경로를 잠그지 못한다. 현재는 체중 가드(AvgWeightG<=0)와
// dailyFeedG<=0 가드가 이중으로 nil 을 보장한다 — 이 테스트는 그 계약을 고정한다.
func TestComputeNextCyclePlan_MissingWeightOnly(t *testing.T) {
	tp := &storage.TankProfile{
		Species:    "atlantic_salmon",
		FishCount:  79,
		AvgWeightG: 0, // 체중만 누락
	}
	pol := &storage.FeedingPolicy{BsfMode: "conservative", OperatingMode: "auto", MaxDailyCycles: 4}
	if plan := computeNextCyclePlan(tp, pol, 11.0); plan != nil {
		t.Errorf("expected nil when avg weight is missing, got %+v", plan)
	}
}

func TestComputeNextCyclePlan_GrowoutAboveMax(t *testing.T) {
	// avg_weight=8000g — growout 상한 (5000) 초과. findStage 가 growout 으로 처리.
	tp := &storage.TankProfile{
		Species:    "atlantic_salmon",
		FishCount:  50,
		AvgWeightG: 8000,
	}
	pol := &storage.FeedingPolicy{BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 4}
	plan := computeNextCyclePlan(tp, pol, 14.0)
	if plan == nil {
		t.Fatal("expected plan (growout above max), got nil")
	}
	if plan.PulseDurationMs != 3000 {
		t.Errorf("expected growout pattern, got pulse_duration_ms=%d", plan.PulseDurationMs)
	}
}

func TestComputeNextCyclePlan_DefaultBsfMode(t *testing.T) {
	// FeedingPolicy.BsfMode 빈 문자열 → "standard" fallback.
	tp := &storage.TankProfile{
		Species:    "atlantic_salmon",
		FishCount:  100,
		AvgWeightG: 1000,
	}
	pol := &storage.FeedingPolicy{
		BsfMode:        "", // 빈 문자열
		OperatingMode:  "auto",
		MaxDailyCycles: 4,
	}
	plan := computeNextCyclePlan(tp, pol, 14.0)
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	// standard 와 동일해야 함 (TargetAmountG=375).
	if !approxEqual(plan.TargetAmountG, 375, 0.5) {
		t.Errorf("default bsf_mode should fall back to standard: got target=%v, want 375", plan.TargetAmountG)
	}
}

func TestFindStage(t *testing.T) {
	pol := speciesPolicyTable["atlantic_salmon"]
	cases := []struct {
		weight float64
		want   string
	}{
		{0.1, "fry"},
		{50, "fry"},
		{99.9, "fry"},
		{100, "fingerling"},
		{400, "fingerling"},
		{500, "growout"},
		{4999, "growout"},
		{8000, "growout"}, // above max → still growout.
	}
	for _, c := range cases {
		if got := findStage(pol, c.weight); got != c.want {
			t.Errorf("findStage(%v) = %q, want %q", c.weight, got, c.want)
		}
	}
}

// 컴파일 가드: config.TankProfile = storage.TankProfile alias 가 깨지면 잡힘.
var _ storage.TankProfile = config.TankProfile{}

// 메인 대시보드 실측값 고정 — 점등창 09:00~17:30, tank_01/02 각 4회 교차 배치.
// (frontend generateCrossTankSchedule 와 동일해야 함: 2026-07-04 스크린샷 기준.)
func TestCrossTankTimes_TwoTanksMatchDashboard(t *testing.T) {
	entries := []crossTankEntry{
		{TankID: "tank_01", Cycles: 4},
		{TankID: "tank_02", Cycles: 4},
	}
	got1 := crossTankTimes(entries, 9*60, 17*60+30, "tank_01")
	want1 := []string{"09:57", "11:50", "13:43", "15:37"}
	if !reflect.DeepEqual(got1, want1) {
		t.Errorf("tank_01 times = %v, want %v", got1, want1)
	}
	got2 := crossTankTimes(entries, 9*60, 17*60+30, "tank_02")
	want2 := []string{"10:53", "12:47", "14:40", "16:33"}
	if !reflect.DeepEqual(got2, want2) {
		t.Errorf("tank_02 times = %v, want %v", got2, want2)
	}
}

// 단일 수조면 기존 computeDailyTimesInWindow 와 동일해야 함 (회귀 방지).
func TestCrossTankTimes_SingleTankEqualsWindowSplit(t *testing.T) {
	entries := []crossTankEntry{{TankID: "tank_01", Cycles: 4}}
	got := crossTankTimes(entries, 9*60, 17*60+30, "tank_01")
	want := computeDailyTimesInWindow(9*60, 17*60+30, 4)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("single tank = %v, want %v", got, want)
	}
}

func TestCrossTankTimes_UnknownTankEmpty(t *testing.T) {
	entries := []crossTankEntry{{TankID: "tank_01", Cycles: 4}}
	if got := crossTankTimes(entries, 9*60, 17*60, "tank_09"); len(got) != 0 {
		t.Errorf("unknown tank should get no times, got %v", got)
	}
}

// 대시보드 마이크로펄스 입력(그룹정책)이 종별 기본 패턴을 대체하는지.
func TestComputeNextCyclePlan_PolicyMicroPulseOverride(t *testing.T) {
	tp := &storage.TankProfile{TankID: "tank_02", Species: "atlantic_salmon", FishCount: 80, AvgWeightG: 880}
	pol := &storage.FeedingPolicy{
		BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 4,
		FeederRunSec: 3, FeederStopSec: 6,
	}
	plan := computeNextCyclePlan(tp, pol, 14.0)
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	if plan.PulseDurationMs != 3000 || plan.GapMs != 6000 {
		t.Errorf("policy override: pulse=%d gap=%d, want 3000/6000", plan.PulseDurationMs, plan.GapMs)
	}
	// 회당 목표 ≈ 264g (80×880g×1.5%/4) → 3s×9.4=28.2g/펄스 → ceil = 10.
	wantPulses := int(math.Ceil(plan.TargetAmountG / (3 * 9.4)))
	if plan.MaxPulses != wantPulses {
		t.Errorf("max_pulses=%d, want %d (target %.0fg)", plan.MaxPulses, wantPulses, plan.TargetAmountG)
	}
}

// 실측 수온 반영 — 찬물(12.5℃)에서 GET₅₀ 가 길어지는지 (2026-07-05).
func TestComputeNextCyclePlan_LiveTempExtendsGet50(t *testing.T) {
	tp := &storage.TankProfile{TankID: "tank_01", Species: "atlantic_salmon", FishCount: 79, AvgWeightG: 862}
	pol := &storage.FeedingPolicy{BsfMode: "standard", OperatingMode: "auto", MaxDailyCycles: 4}
	warm := computeNextCyclePlan(tp, pol, 14.0)
	cold := computeNextCyclePlan(tp, pol, 12.5)
	if warm == nil || cold == nil {
		t.Fatal("expected plans")
	}
	if !(cold.Get50Hours > warm.Get50Hours) {
		t.Errorf("cold GET50 (%.2f) should exceed warm GET50 (%.2f)", cold.Get50Hours, warm.Get50Hours)
	}
	if cold.TempC != 12.5 || warm.TempC != 14.0 {
		t.Errorf("TempC echo mismatch: %v / %v", cold.TempC, warm.TempC)
	}
}
