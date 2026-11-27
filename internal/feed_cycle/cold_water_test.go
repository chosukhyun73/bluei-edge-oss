package feed_cycle

import "testing"

// 저수온 보정 — 2026-10-01 운영자 결정 사항을 고정한다.
func TestColdWaterRatePct(t *testing.T) {
	cases := []struct{ temp, want float64 }{
		{8.0, 0},    // 8℃ 이상은 기존 곡선 담당 (여기선 0)
		{7.0, 0.35}, // 중간
		{6.0, 0.20}, // 하한 급이율
		{5.9, 0},    // 하한 미만 중단
	}
	for _, c := range cases {
		if got := coldWaterRatePct(c.temp); !approxEqual(got, c.want, 0.001) {
			t.Errorf("coldWaterRatePct(%.1f) = %.3f, want %.3f", c.temp, got, c.want)
		}
	}
	// 절벽 없음 — 8℃ 직전 값이 기존 곡선의 8℃ 값(0.50%)에 수렴해야 한다.
	if got := coldWaterRatePct(7.99); got < 0.49 || got > 0.50 {
		t.Errorf("8℃ 이음매가 끊겼다: %.4f", got)
	}
}

func TestColdGapMs(t *testing.T) {
	base := 77000
	if got := coldGapMs(base, 13.5); got != base {
		t.Errorf("기준 수온에서는 그대로여야 한다: %d", got)
	}
	// 계수 3.0 — 8℃ 면 약 1.83배 (운영자 결정으로 대사 Q10 보다 크게)
	if got := coldGapMs(base, 8.0); got < 138000 || got > 144000 {
		t.Errorf("8℃ 간격: got %d, want ~141000", got)
	}
	// 6℃ 에서 상한 직전까지 벌어진다
	if got := coldGapMs(base, 6.0); got < 172000 || got > 180000 {
		t.Errorf("6℃ 간격: got %d, want ~176000", got)
	}
	// 상한 3분
	if got := coldGapMs(base, -50); got != gapMaxMs {
		t.Errorf("상한 미적용: %d", got)
	}
}

func TestColdDailyCyclesAndRapidDrop(t *testing.T) {
	// 1일량이 작아 2회로 쪼개도 1회최대량 안쪽 — 종전대로 2회.
	if got := coldDailyCycles(4, 8.5, 228, 163); got != 2 {
		t.Errorf("9℃ 미만 축소 실패: %d", got)
	}
	if got := coldDailyCycles(4, 9.0, 228, 163); got != 4 {
		t.Errorf("9℃ 이상은 유지: %d", got)
	}

	// 2026-10-10 역전 사고 재현 — 8.85℃ 에서 1일 940g, 1회최대 163g.
	// 2회로 떨어뜨리면 회당 470g 으로 상한의 2.9배가 된다. 6회까지만 줄어야 한다.
	if got := coldDailyCycles(8, 8.85, 940, 163); got != 6 {
		t.Errorf("1회최대량 하한 미적용: got %d, want 6", got)
	}
	// 핵심 불변식 — 수온이 내려갔다고 회당 양이 1회최대량을 넘어서는 안 된다.
	for _, tc := range []struct {
		cycles           int
		tempC            float64
		dailyG, mealMaxG float64
	}{
		{8, 8.85, 940, 163}, {8, 8.0, 500, 163}, {8, 7.0, 228, 163},
		{4, 6.0, 130, 163}, {12, 8.99, 1200, 163},
	} {
		n := coldDailyCycles(tc.cycles, tc.tempC, tc.dailyG, tc.mealMaxG)
		if per := tc.dailyG / float64(n); per > tc.mealMaxG && n < tc.cycles {
			t.Errorf("%.2f℃: 회당 %.0fg 가 상한 %.0fg 초과 (횟수 %d→%d)",
				tc.tempC, per, tc.mealMaxG, tc.cycles, n)
		}
		if n > tc.cycles {
			t.Errorf("%.2f℃: 횟수가 늘어났다 %d→%d", tc.tempC, tc.cycles, n)
		}
	}
	// 1일량·상한을 모를 때는 회당 양을 키우지 않는다 — 그대로 둔다.
	if got := coldDailyCycles(8, 8.5, 0, 163); got != 8 {
		t.Errorf("1일량 미상 시 축소하면 안 된다: %d", got)
	}
	if got := coldDailyCycles(8, 8.5, 940, 0); got != 8 {
		t.Errorf("1회최대량 미상 시 축소하면 안 된다: %d", got)
	}
	// 급락 — 당일 절반
	if got := rapidChangeFactorFor(8.7, 10.5, 10.6); got != 0.5 {
		t.Errorf("1.8℃ 급락 시 감산되어야 한다: %.2f", got)
	}
	// 급등도 동일 — 방향 무관 (10-01 실측: 9시간 +4.4℃)
	if got := rapidChangeFactorFor(13.0, 8.7, 8.6); got != 0.5 {
		t.Errorf("급등도 감산되어야 한다: %.2f", got)
	}
	// 소폭 변화는 그대로
	if got := rapidChangeFactorFor(8.7, 9.2, 9.3); got != 1.0 {
		t.Errorf("0.5℃ 변화는 감산 없음: %.2f", got)
	}
	// 어제 급변 · 오늘 안정 → 적응 기간 70%
	if got := rapidChangeFactorFor(12.9, 13.0, 8.7); got != 0.7 {
		t.Errorf("급변 다음 날은 70%%: %.2f", got)
	}
}
