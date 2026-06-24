package feed_cycle

// Phase 1c — AIScheduler.computeNextCyclePlan unit tests.
//
// frontend lib/species-policy.ts 의 computeFeedingPolicy 와 동일 결과를 backend
// 에서 재현하는지 검증. atlantic_salmon × {fry, fingerling, growout} × {standard,
// aggressive, conservative} 의 대표 케이스 + 단계 미판정 / 종 미지원 / 정보 부족
// 의 negative 경로.

import (
	"math"
	"testing"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

// approxEqual — 부동소수점 비교 (소수 4째자리까지).
func approxEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func TestComputeNextCyclePlan_GrowoutStandard(t *testing.T) {
	// tank_01 growout standard, T=14℃ fallback, max_daily_cycles=4.
	//
	// fish_count=100, avg_weight_g=1000 → biomass_kg=100, biomass_g=100_000.
	// bsf_pct = 1.5 (growout standard) → daily_feed_g = 100 × 0.015 × 1000 = 1500.
	// estimate_per_cycle_g (3 분할) = 500 → meal_pct_bw = (500 / 100) × 0.1 = 0.5 (%BW).
	// GET95(T=14, M=0.5) = 24 × 2^((18-14)/10) × (0.5/1.0)^0.5
	//                    = 24 × 2^0.4 × sqrt(0.5)
	//                    ≈ 24 × 1.31951 × 0.70711 ≈ 22.395
	// GET50 = GET95 × ln(2)/ln(20) ≈ 22.395 × 0.23137 ≈ 5.1816 h.
	// daily_cycles = min(4, floor(24/5.18)) = min(4, 4) = 4.
	// cycle_target_g = 1500/4 = 375 → Round(375) = 375.
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

	plan := computeNextCyclePlan(tp, pol)
	if plan == nil {
		t.Fatal("expected plan, got nil")
	}
	// target = 375 g (1500 / 4 cycles).
	if !approxEqual(plan.TargetAmountG, 375, 0.5) {
		t.Errorf("target_amount_g: got %v, want ~375", plan.TargetAmountG)
	}
	// GET50 ≈ 5.18 h.
	if !approxEqual(plan.Get50Hours, 5.18, 0.05) {
		t.Errorf("get50_h: got %v, want ~5.18", plan.Get50Hours)
	}
	// growout pattern default.
	if plan.PulseDurationMs != 2500 || plan.GapMs != 75_000 || plan.MaxPulses != 6 ||
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
	plan := computeNextCyclePlan(tp, pol)
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
	plan := computeNextCyclePlan(tp, pol)
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
	if plan := computeNextCyclePlan(tp, pol); plan != nil {
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
	if plan := computeNextCyclePlan(tp, pol); plan != nil {
		t.Errorf("expected nil for missing stocking, got %+v", plan)
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
	plan := computeNextCyclePlan(tp, pol)
	if plan == nil {
		t.Fatal("expected plan (growout above max), got nil")
	}
	if plan.PulseDurationMs != 2500 {
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
	plan := computeNextCyclePlan(tp, pol)
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
