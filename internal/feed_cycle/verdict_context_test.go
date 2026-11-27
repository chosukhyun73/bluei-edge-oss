package feed_cycle_test

import (
	"math"
	"testing"
	"time"

	"bluei.kr/edge/internal/feed_cycle"
)

// contextTestSeries — 실제 사이클 모양을 본뜬 곡선 (판정 시각 r≈300s 기준).
//
//	초반 0~250s : 12.0  — 앞 펄스들의 반응으로 높음 (맥락 기준값이 된다)
//	pre  292~299: 10.0  — 판정 기준선
//	post 310~320: 10.0  — 상승 없음 → 상승폭 판정은 항상 "no"
//	gap  325~355:        tailActive 면 ±1.0 출렁(어체 여전히 활발) / 아니면 평탄
//
// 두 경우의 상승폭 판정은 똑같이 "no" 다. 갈리는 것은 맥락(반응창 변동성)뿐이다.
func contextTestSeries(n int, tailActive bool) []float64 {
	s := make([]float64, n)
	for i := range s {
		base := 10.0
		if i < 250 {
			base = 12.0
		}
		// 전 구간 미세 잡음 — 노이즈가 0 이면 판정이 성립하지 않는다.
		s[i] = base + 0.02*math.Sin(float64(i)/3.0)
	}
	if tailActive {
		for i := 325; i < 356 && i < n; i++ {
			s[i] = 10.0 + math.Sin(float64(i)/2.0)
		}
	}
	// 곡선 끝(맥락 창 밖)은 높게 — 만복 게이트가 보상을 폐기하지 않도록.
	// 여기서 검증하려는 것은 만복 게이트가 아니라 맥락 판정이다.
	for i := 356; i < n; i++ {
		s[i] = 20.0
	}
	return s
}

// 맥락 판정이 켜져 있으면, 요동이 계속 출렁이는(=어체가 활발한) 구간의 "no" 는
// unclear 로 낮춰지고 보상이 발사되지 않는다. 2026-07-30 운영자 지시로 도입된
// "차트의 값으로 각 펄스를 판단" 경로의 통합 검증.
func TestContextSoftensFalseNo_NoCompensation(t *testing.T) {
	runs := runContextCycle(t, "test_ctx_soft", true, true)
	if runs != 5 {
		t.Fatalf("맥락상 배출로 보이면 보상 없이 계획 5발이어야 한다, got %d", runs)
	}
}

// 반응창이 평탄해 no 가 유지되는 경우(진짜 무배출)에도 보상은 나가지 않는다.
// 2026-08-18 보상 절차 제외 — 종전에는 여기서 보상 1발이 붙어 6발이었다.
// 판정 자체(게이트 완화 여부)는 compensation_gate_test.go 가 단위로 검증한다.
func TestContextKeepsTrueNo_NoCompensation(t *testing.T) {
	runs := runContextCycle(t, "test_ctx_true", false, true)
	if runs != 5 {
		t.Fatalf("no 판정이어도 보상 없이 계획 5발이어야 한다, got %d", runs)
	}
}

// 맥락 판정이 꺼져 상승폭 판정만 써도 결과는 같다 — 보상 경로가 아예 없다.
func TestContextDisabled_NoCompensation(t *testing.T) {
	runs := runContextCycle(t, "test_ctx_off", true, false)
	if runs != 5 {
		t.Fatalf("게이트 off 여도 보상 없이 계획 5발이어야 한다, got %d", runs)
	}
}

func runContextCycle(t *testing.T, cycleID string, oscillate, gateOn bool) int {
	t.Helper()
	var runs int
	cfg := feed_cycle.TestConfig()
	cfg.CompensationMaxPerCycle = 1
	cfg.CompensationGateEnabled = gateOn
	store := &mockStore{}
	w := feed_cycle.NewWorker(nil, store, nil, cfg, nil)
	w.SetFeederActuateForTest(func(_ *feed_cycle.Cycle, cmd string) error {
		if cmd == "feeder.run" {
			runs++
		}
		return nil
	})
	// 마지막(5번째) 펄스만 판정 대상으로 삼는다 — 앞 펄스는 series 없음으로 skip.
	calls := 0
	w.SetLiveSeriesProvider(func(string) ([]float64, time.Time, bool) {
		calls++
		if calls < 5 {
			return nil, time.Time{}, false
		}
		return contextTestSeries(400, oscillate), time.Now().Add(-300 * time.Second), true
	})

	p := feed_cycle.FixedParams{
		PulseDurationMs: 2000,
		GapMs:           1500,
		TotalPulses:     5,
		TargetAmountG:   94, // 5펄스(18.8g×5)로 도달
	}
	c := feed_cycle.NewFixedCycle(cycleID, "tank_01", "", p)
	c.SetOnoffFeeder(831, true)

	go w.RunCycle(c)
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if c.State() == feed_cycle.StateCycleComplete {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.State() != feed_cycle.StateCycleComplete {
		t.Fatalf("cycle did not complete, state=%s", c.State())
	}
	return runs
}
