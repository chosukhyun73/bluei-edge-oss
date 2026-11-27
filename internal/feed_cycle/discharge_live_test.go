package feed_cycle

import (
	"testing"
	"time"
)

// 합성 series 생성: 길이 n, 기저 base + 노이즈(결정적 지그재그 ±j).
func flatSeries(n int, base, j float64) []float64 {
	s := make([]float64, n)
	for i := range s {
		d := j
		if i%2 == 1 {
			d = -j
		}
		s[i] = base + d
	}
	return s
}

func TestJudgeLivePulseDischarge(t *testing.T) {
	start := time.Date(2026, 7, 8, 7, 0, 0, 0, time.UTC)
	runAt := start.Add(60 * time.Second) // r=60

	// 뚜렷한 착수 상승 (r+23~33 창에 큰 피크) → yes
	s := flatSeries(120, 5.0, 0.3)
	for i := 85; i < 91; i++ {
		s[i] = 12.0
	}
	if got, _ := judgeLivePulseDischarge(s, start, runAt); got != "yes" {
		t.Errorf("clear rise: got %q want yes", got)
	}

	// 상승 없음 — 앞쪽에 이전 펄스 활동(σ 원천)이 있고 착수창은 평탄 → no
	s2 := flatSeries(120, 5.0, 0.05)
	for i := 20; i < 35; i++ {
		s2[i] = 8.0
	}
	if got, _ := judgeLivePulseDischarge(s2, start, runAt); got != "no" {
		t.Errorf("flat landing window: got %q want no", got)
	}

	// 경미한 상승(배고픔 요동, ×2 미만) + 평탄 반응창 → 가드 통과, no (보상 허용)
	// — 기준선(run 이전)과 반응창(run+23~33)이 같은 수준으로 고르게 상승한 케이스.
	s3 := flatSeries(120, 5.0, 0.05)
	for i := 20; i < 35; i++ {
		s3[i] = 8.0 // σ 원천 (이전 활동)
	}
	for i := 50; i < 95; i++ {
		s3[i] = 7.0 // run 이전 기준선창(52~59)과 반응창(83~93)이 동일 수준 — 국소상승 없음
	}
	if got, _ := judgeLivePulseDischarge(s3, start, runAt); got != "no" {
		t.Errorf("mildly elevated flat: got %q want no", got)
	}

	// 극단 포화 가드 — 스핀업창이 기저 ×2.0 초과 (렌즈 가림급) → unclear (보상 금지)
	s5 := flatSeries(120, 5.0, 0.3)
	for i := 50; i < 75; i++ {
		s5[i] = 11.0 // 기준선창(52~59) 전체가 기저 5 × 2.0 = 10 초과
	}
	if got, _ := judgeLivePulseDischarge(s5, start, runAt); got != "unclear" {
		t.Errorf("saturated pre (guard): got %q want unclear", got)
	}

	// 데이터 부족 (착수창이 아직 안 참) → unknown
	s4 := flatSeries(70, 5.0, 0.3) // r+23=83 > 길이 70 → 반응창 빔
	if got, _ := judgeLivePulseDischarge(s4, start, runAt); got != "unknown" {
		t.Errorf("short series: got %q want unknown", got)
	}
}

func TestGrantCompensation_ExtendsMaxPulsesAndDeducts(t *testing.T) {
	c := NewFixedCycle("cycle_comp", "tank_02", "", FixedParams{
		PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5, TargetAmountG: 85,
	})
	if got := c.maxPulses(); got != 5 {
		t.Fatalf("maxPulses before grant: got %d want 5", got)
	}
	c.recordPulseResult(18.8, 18.8)
	c.recordPulseResult(18.8, 18.8)
	before := c.TotalAmountG()

	c.grantCompensation(18.8)
	if got := c.maxPulses(); got != 6 {
		t.Errorf("maxPulses after grant: got %d want 6", got)
	}
	if got := c.CompensationsGranted(); got != 1 {
		t.Errorf("granted: got %d want 1", got)
	}
	if got := c.TotalAmountG(); got != before-18.8 {
		t.Errorf("phantom deduct: got %v want %v", got, before-18.8)
	}

	// 차감이 target 조기 도달을 막는지 — 5펄스 추정 합(85 트림 가정 근사)에서
	// 1발 차감되면 isTargetReached=false 여야 함.
	c2 := NewFixedCycle("cycle_comp2", "tank_02", "", FixedParams{
		PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5, TargetAmountG: 85,
	})
	for i := 0; i < 4; i++ {
		c2.recordPulseResult(18.8, 18.8)
	}
	c2.recordPulseResult(9.8, 9.8) // 트림된 마지막 펄스 → 합 ≈ 85
	if !c2.isTargetReached() {
		t.Fatalf("precondition: target should be reached at ~85g")
	}
	c2.grantCompensation(18.8)
	if c2.isTargetReached() {
		t.Errorf("after deduct: target should NOT be reached")
	}
}
