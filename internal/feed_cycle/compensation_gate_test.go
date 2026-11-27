package feed_cycle

import (
	"math"
	"testing"
	"time"
)

// gateCurve — 사이클 초반 3펄스는 큰 반응, 그 뒤 tailScale 로 줄어드는 합성 곡선.
// tailScale=0 이면 뒤쪽 펄스는 완전 무반응(진짜 무배출), 0.7 이면 반응이 남음(만복 거짓 no).
func gateCurve(n int, period int, pulses int, tailScale float64) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = 6.0 + 0.05*math.Sin(float64(i)/11.0) // 완만한 기저 흔들림
	}
	for k := 0; k < pulses; k++ {
		r := k * period
		amp := 2.5
		if k >= compGateRefPeriods {
			amp = 2.5 * tailScale
		}
		// 감쇠 시정수 60s — 실측처럼 gap 동안 기저로 완전히 내려오지 않는다
		// (실측 중앙 50s, 90%는 132s 필요. 2026-07-29 측정).
		for t := r + 10; t < r+90 && t < n; t++ {
			dt := float64(t - (r + 15))
			if dt >= 0 {
				s[t] += amp * math.Exp(-dt/60.0)
			} else {
				s[t] += amp * math.Exp(dt/4.0)
			}
		}
	}
	return s
}

func TestCompensationGate_TrueNoDischargePasses(t *testing.T) {
	start := time.Date(2026, 7, 30, 0, 30, 0, 0, time.UTC)
	period := 70
	s := gateCurve(560, period, 8, 0) // 뒤쪽 펄스 완전 무반응
	// p6 판정 (인덱스 5)
	res := compensationGate(s, start, start.Add(time.Duration(5*period)*time.Second), 6, 0, 0)
	if !res.Checked {
		t.Fatal("판정 가능해야 한다")
	}
	if !res.Pass {
		t.Errorf("진짜 무배출은 게이트를 통과해야 한다 (rel=%.3f var=%.3f)", res.Rel, res.VarRatio)
	}
}

func TestCompensationGate_SatiationFalseNoIsWithheld(t *testing.T) {
	start := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	period := 70
	s := gateCurve(560, period, 8, 0.7) // 뒤쪽도 반응이 남음 = 만복 거짓 no
	res := compensationGate(s, start, start.Add(time.Duration(5*period)*time.Second), 6, 0, 0)
	if !res.Checked {
		t.Fatal("판정 가능해야 한다")
	}
	if res.Pass {
		t.Errorf("만복 거짓 no 는 보류돼야 한다 (rel=%.3f var=%.3f)", res.Rel, res.VarRatio)
	}
}

func TestCompensationGate_EarlyPulsesNotChecked(t *testing.T) {
	start := time.Date(2026, 7, 30, 1, 30, 0, 0, time.UTC)
	period := 70
	s := gateCurve(560, period, 8, 0)
	for pn := 1; pn <= 3; pn++ {
		res := compensationGate(s, start, start.Add(time.Duration((pn-1)*period)*time.Second), pn, 0, 0)
		if res.Checked {
			t.Errorf("p%d 은 기준 구간이라 판정하지 않아야 한다", pn)
		}
	}
}

// 판정 불가 조건은 모두 Checked=false → 호출부가 종전대로 발사(fail-open).
func TestCompensationGate_FailOpenOnBadInput(t *testing.T) {
	start := time.Date(2026, 7, 30, 2, 0, 0, 0, time.UTC)
	period := 70
	runAt := start.Add(time.Duration(5*period) * time.Second)

	cases := map[string]struct {
		series []float64
		runAt  time.Time
	}{
		"series 없음":     {nil, runAt},
		"series 너무 짧음":  {gateCurve(40, period, 1, 0), runAt},
		"기저 0":          {make([]float64, 560), runAt},
		"run 시각이 세션 이전": {gateCurve(560, period, 8, 0), start.Add(-time.Minute)},
		"반응창이 곡선 밖":     {gateCurve(560, period, 8, 0), start.Add(600 * time.Second)},
	}
	for name, c := range cases {
		if res := compensationGate(c.series, start, c.runAt, 6, 0, 0); res.Checked {
			t.Errorf("%s: Checked=false 여야 한다 (fail-open)", name)
		}
	}
}

// 실측 재현 — 2026-07-29 11:30 tank_01. 육안 확정: p6 만 무배출, p7 은 배출.
// 자기기준비 실측 p6=0.73 / p7=0.89, 변동성 p6=0.036 / p7=0.099.
// 게이트가 p6 만 통과시키고 p7 은 보류해야 한다.
func TestCompensationGate_ReplayRealCycle(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rel, vr  float64
		wantPass bool
	}{
		{"p6 진짜 무배출", 0.73, 0.036, true},
		{"p7 거짓 no (변동성 높음)", 0.89, 0.099, false},
		{"14:00 t02 p8 진짜 무배출", 0.79, 0.021, true},
		{"09:30 t01 p6 거짓 no", 0.88, 0.056, false},
		{"12:30 t01 p7 거짓 no (기준비만 낮음)", 0.77, 0.063, false},
	} {
		pass := tc.rel < compGateDefaultRelMax && tc.vr < compGateDefaultVarMax
		if pass != tc.wantPass {
			t.Errorf("%s: rel=%.2f var=%.3f → pass=%v, want %v",
				tc.name, tc.rel, tc.vr, pass, tc.wantPass)
		}
	}
}

// 음수 rise 는 만복 완화보다 우선한다 (2026-08-16 운영자 결정).
// ⚠ 이 규칙은 "덜 깎는" 쪽이 아니다 — 8-16 14:40 t02 p8(rise −0.17, 마지막 트림 펄스)은
// 종전 규칙에서 만복으로 완화됐으나 이제 no 로 남아 차감된다.
func TestShouldApplySatiationGate_NegativeRiseOverrides(t *testing.T) {
	cases := []struct {
		name                                     string
		raw                                      string
		enabled, satiationPossible, negativeRise bool
		want                                     bool
	}{
		{"양수 저반응 + 만복 가능 → 완화 적용", "no", true, true, false, true},
		{"음수 → 완화 안 함 (핵심)", "no", true, true, true, false},
		{"음수여도 게이트 자체가 꺼져 있으면 무관", "no", false, true, true, false},
		{"배출 확인 부족이면 종전대로 완화 안 함", "no", true, false, false, false},
		{"no 가 아니면 완화 대상 아님", "unclear", true, true, false, false},
	}
	for _, c := range cases {
		if got := shouldApplySatiationGate(c.raw, c.enabled, c.satiationPossible, c.negativeRise); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
