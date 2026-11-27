package feed_cycle

import "testing"

// 펄스별 공급시간 override (fixed 모드 램프 실험) — 인덱스 내 override, 범위 밖 폴백(0).
func TestPulseDurationOverrideMs(t *testing.T) {
	c := NewFixedCycle("cycle_ramp", "tank_01", "", FixedParams{
		PulseDurationMs:  2000,
		GapMs:            1000,
		TotalPulses:      5,
		PulseDurationsMs: []int{2000, 2000, 3000, 4000, 5000},
	})

	want := []int{2000, 2000, 3000, 4000, 5000}
	for i, w := range want {
		if got := c.pulseDurationOverrideMs(i); got != w {
			t.Errorf("idx %d: got %d want %d", i, got, w)
		}
	}
	// 배열 범위 밖 / 음수 인덱스 → 0 (호출부가 uniform 값 유지)
	if got := c.pulseDurationOverrideMs(5); got != 0 {
		t.Errorf("idx 5: got %d want 0", got)
	}
	if got := c.pulseDurationOverrideMs(-1); got != 0 {
		t.Errorf("idx -1: got %d want 0", got)
	}

	// 배열 미설정 fixed 사이클 → 항상 0
	c2 := NewFixedCycle("cycle_uniform", "tank_01", "", FixedParams{
		PulseDurationMs: 2000, GapMs: 1000, TotalPulses: 5,
	})
	if got := c2.pulseDurationOverrideMs(0); got != 0 {
		t.Errorf("no-array idx 0: got %d want 0", got)
	}

	// adaptive 사이클 → 항상 0
	c3 := NewAdaptiveCycle("cycle_adaptive", "tank_01", "", AdaptiveParams{TargetAmountG: 100})
	if got := c3.pulseDurationOverrideMs(0); got != 0 {
		t.Errorf("adaptive idx 0: got %d want 0", got)
	}
}
