package feed_cycle

import (
	"testing"

	"bluei.kr/edge/internal/config"
)

// 판정 본체 — 실측 사이클을 그대로 옮긴 표.
func TestJudgeDischargeGated(t *testing.T) {
	cases := []struct {
		name        string
		values      []float64
		wantStop    int
		wantNoDisch int
	}{
		{
			// 8-03 10:00 tank_02 — 급이기 고장. 6발 전부 배출 안 됨(운영자 `배출=none`).
			// v0 는 여기서 "p2 중단"을 냈다. v1 은 중단하지 않고 무배출로만 센다.
			name:        "급이기 고장 — 중단하지 않고 무배출로 집계",
			values:      []float64{0.09, 0.07, 0.01, 0.04, -0.01, -0.05},
			wantStop:    0,
			wantNoDisch: 6,
		},
		{
			// 8-03 09:30 tank_01 — 정상 급이. 전 펄스 반응 충분.
			name:        "정상 사이클 — 중단 없음",
			values:      []float64{0.54, 0.50, 0.59, 0.59, 0.77, 0.32},
			wantStop:    0,
			wantNoDisch: 0,
		},
		{
			// 8-03 11:00 tank_02 — 배출은 됐는데 반응이 낮다. p1·p2 연속 미달.
			name:     "저반응 연속 — p2 에서 중단",
			values:   []float64{0.22, 0.26, 0.11, 0.15, 0.02, 0.07, 0.13},
			wantStop: 2,
			// peak 0.26 → 하한 clamp(0.104, 0.12, 0.20) = 0.12. 0.11·0.02·0.07 만 미달.
			wantNoDisch: 3,
		},
		{
			// 무배출이 중간에 끼어도 배출 펄스의 연속성은 유지된다.
			// p2(0.28) 낮음 → 무배출 p3 은 건너뜀 → p4(0.25) 낮음 = 연속 2발.
			name:        "무배출은 연속 카운트를 끊지 않는다",
			values:      []float64{0.45, 0.28, 0.05, 0.25, 0.40},
			wantStop:    4,
			wantNoDisch: 1,
		},
		{
			// 강반응이 끼면 누적을 리셋한다.
			name:        "강반응이 연속을 리셋한다",
			values:      []float64{0.28, 0.50, 0.29, 0.45},
			wantStop:    0,
			wantNoDisch: 0,
		},
		{
			// 만복 진행 — 뒤로 갈수록 낮아져 마지막 두 발이 미달.
			name:        "만복 감쇠 — 마지막에 중단",
			values:      []float64{0.40, 0.42, 0.39, 0.38, 0.29, 0.28},
			wantStop:    6,
			wantNoDisch: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			minV := dischargeFloor(tc.values)
			discharged, noDisch, stop := judgeDischargeGated(tc.values, minV, shadowLowReaction, shadowLowStreak)
			if stop != tc.wantStop {
				t.Errorf("stop_at_pulse = %d, want %d (values %v)", stop, tc.wantStop, tc.values)
			}
			if noDisch != tc.wantNoDisch {
				t.Errorf("no_discharge = %d, want %d", noDisch, tc.wantNoDisch)
			}
			if len(discharged) != len(tc.values) {
				t.Fatalf("discharged len = %d, want %d", len(discharged), len(tc.values))
			}
			for i, v := range tc.values {
				if want := v >= minV; discharged[i] != want {
					t.Errorf("discharged[%d] = %v, want %v (v=%.2f)", i, discharged[i], want, v)
				}
			}
		})
	}
}

// 배출이 한 발도 확인되지 않으면 절대 중단이 나오지 않아야 한다 — v0 가 틀렸던 지점.
// (모든 값이 하한 미만인 경우. 하한은 절대 바닥 0.12 아래로 내려가지 않는다.)
func TestJudgeDischargeGatedNeverStopsOnFeederFault(t *testing.T) {
	for _, v := range [][]float64{
		{0.0, 0.0, 0.0, 0.0},
		{0.09, 0.07, 0.01, 0.04, -0.01, -0.05}, // 8-03 10:00 tank_02 실측
		{-0.05, 0.10, 0.02, 0.11, 0.00, 0.07},
	} {
		if _, _, stop := judgeDischargeGated(v, dischargeFloor(v), shadowLowReaction, shadowLowStreak); stop != 0 {
			t.Errorf("values %v → stop %d, want 0 (배출 0발이면 식욕 판단 불가)", v, stop)
		}
	}
}

// 하한은 사이클 식욕에 연동하되 [절대바닥, 상한] 을 벗어나지 않는다.
func TestDischargeFloor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []float64
		want   float64
	}{
		{"고식욕 — 상한에 걸림", []float64{0.54, 0.77, 0.32}, shadowDischargeMin},
		{"중식욕 — 연동 구간", []float64{0.40, 0.42, 0.28}, 0.42 * shadowDischargeRel},
		{"연동 하단", []float64{0.30, 0.28, 0.11}, 0.30 * shadowDischargeRel},
		// peak 0.26×0.40 = 0.104 < 바닥 0.12 → 연동이 끊기고 바닥이 쓰인다.
		// 즉 연동이 실제로 작동하는 구간은 peak 0.30~0.50 뿐이다.
		{"저식욕 — 바닥에 걸림", []float64{0.22, 0.26, 0.11}, shadowDischargeAbsMin},
		// 급이기 고장 사이클이 스스로를 정규화하지 못하게 바닥이 받쳐준다.
		{"고장 — 절대바닥이 받침", []float64{0.09, 0.07, -0.05}, shadowDischargeAbsMin},
		{"전부 0", []float64{0, 0, 0}, shadowDischargeAbsMin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dischargeFloor(tc.values); got != tc.want {
				t.Errorf("dischargeFloor(%v) = %v, want %v", tc.values, got, tc.want)
			}
		})
	}
}

// 최초 배출 펄스 대비 낙폭(섀도). 표는 8-10 실측 사이클을 그대로 옮긴 것이다.
func TestShadowStopAnchorBlock(t *testing.T) {
	for _, tc := range []struct {
		name       string
		values     []float64
		discharged []bool
		wantAnchor float64
		wantPulse  int
		wantStop   int
		wantRatios []any
	}{
		{
			// 8-10 17:08 tank_02 — 전 펄스 배출, 첫 발 대비 오히려 상승. 중단 없음.
			name:       "정상 사이클 — 중단 없음",
			values:     []float64{0.67, 0.63, 0.71, 0.70, 0.80, 0.70},
			discharged: []bool{true, true, true, true, true, true},
			wantAnchor: 0.67, wantPulse: 1, wantStop: 0,
			wantRatios: []any{nil, 0.94, 1.06, 1.04, 1.19, 1.04},
		},
		{
			// 8-10 14:14 tank_02 — 무배출 2발(p8·p11)이 섞인 11발. 무배출은
			// 비교에서 빠지고(nil), p9 가 앵커 대비 0.37 로 처음 임계를 깬다.
			name:       "무배출 섞임 — 비교에서 빠지고 p9 에서 중단",
			values:     []float64{0.60, 0.86, 0.74, 0.54, 0.80, 0.37, 0.48, 0.16, 0.22, 0.41, 0.14},
			discharged: []bool{true, true, true, true, true, true, true, false, true, true, false},
			wantAnchor: 0.60, wantPulse: 1, wantStop: 9,
			wantRatios: []any{nil, 1.43, 1.23, 0.90, 1.33, 0.62, 0.80, nil, 0.37, 0.68, nil},
		},
		{
			// 첫 발이 무배출이면 앵커는 첫 '배출' 펄스로 밀린다 (v3 와 같은 규약).
			name:       "첫 발 무배출 — 앵커가 p2 로 밀림",
			values:     []float64{0.05, 0.50, 0.45, 0.20},
			discharged: []bool{false, true, true, true},
			wantAnchor: 0.50, wantPulse: 2, wantStop: 4,
			wantRatios: []any{nil, nil, 0.90, 0.40},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shadowStopAnchorBlock(tc.values, tc.discharged)
			if got == nil {
				t.Fatal("nil block")
			}
			if got["anchor"] != tc.wantAnchor || got["anchor_pulse"] != tc.wantPulse {
				t.Errorf("anchor = %v (p%v), want %v (p%v)",
					got["anchor"], got["anchor_pulse"], tc.wantAnchor, tc.wantPulse)
			}
			if got["stop_at_pulse"] != tc.wantStop {
				t.Errorf("stop_at_pulse = %v, want %d", got["stop_at_pulse"], tc.wantStop)
			}
			ratios, _ := got["ratios"].([]any)
			if len(ratios) != len(tc.wantRatios) {
				t.Fatalf("ratios len = %d, want %d", len(ratios), len(tc.wantRatios))
			}
			for i := range ratios {
				if ratios[i] != tc.wantRatios[i] {
					t.Errorf("ratios[%d] = %v, want %v", i, ratios[i], tc.wantRatios[i])
				}
			}
		})
	}
}

// 배출이 한 발도 없으면 앵커를 잡을 수 없다 — 급이기 고장을 만복으로 읽지 않는다.
func TestShadowStopAnchorBlockNoDischarge(t *testing.T) {
	for _, tc := range []struct {
		name       string
		values     []float64
		discharged []bool
	}{
		{"전 펄스 무배출", []float64{0.09, 0.07, 0.01, 0.04}, []bool{false, false, false, false}},
		{"펄스 1발", []float64{0.60}, []bool{true}},
		{"길이 불일치", []float64{0.60, 0.50}, []bool{true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shadowStopAnchorBlock(tc.values, tc.discharged); got != nil {
				t.Errorf("got %v, want nil", got)
			}
		})
	}
}

// 추세(기울기) 축 — 한 발 급락 대신 여러 발의 흐름.
func TestShadowStopSlopeBlock(t *testing.T) {
	t.Run("실측 사이클 — 무배출 제외 + 창 채우기", func(t *testing.T) {
		// 8-12 11:00 tank_02. p1 무배출이라 열에서 빠지고, 창(4발)이 p5 에서야 찬다.
		// ⚠ 이 사이클에서 규칙은 p5 에 발동하지만 p6·p7 이 0.84·0.97 로 되살아난다 —
		// 거짓 중단이다. 검증셋 성적(결식 0)은 평균일 뿐 개별 사이클을 보장하지 않는다.
		got := shadowStopSlopeBlock(
			[]float64{0.04, 0.90, 0.31, 0.23, 0.29, 0.84, 0.97, 0.44, 0.73, 0.48, 0.23},
			[]bool{false, true, true, true, true, true, true, true, true, true, true})
		if got == nil {
			t.Fatal("nil block")
		}
		if got["stop_at_pulse"] != 5 {
			t.Errorf("stop_at_pulse = %v, want 5", got["stop_at_pulse"])
		}
		want := []any{nil, nil, nil, nil, -0.44, 0.40, 0.48, 0.09, -0.12, -0.18, -0.19}
		slopes, _ := got["slopes"].([]any)
		if len(slopes) != len(want) {
			t.Fatalf("slopes len = %d, want %d", len(slopes), len(want))
		}
		for i := range want {
			if slopes[i] != want[i] {
				t.Errorf("slopes[%d] = %v, want %v", i, slopes[i], want[i])
			}
		}
	})

	t.Run("상승 추세 — 중단하지 않는다", func(t *testing.T) {
		got := shadowStopSlopeBlock(
			[]float64{0.30, 0.45, 0.55, 0.60, 0.70},
			[]bool{true, true, true, true, true})
		if got == nil {
			t.Fatal("nil block")
		}
		if got["stop_at_pulse"] != 0 {
			t.Errorf("stop_at_pulse = %v, want 0 (상승 중인데 중단)", got["stop_at_pulse"])
		}
	})

	t.Run("배출 펄스가 창보다 적으면 판정 불가", func(t *testing.T) {
		for _, tc := range []struct {
			values     []float64
			discharged []bool
		}{
			{[]float64{0.5, 0.4}, []bool{true, true}},                           // 2발
			{[]float64{0.5, 0.4, 0.3}, []bool{true, false, true}},               // 배출 2발
			{[]float64{0.5, 0.4, 0.3, 0.2}, []bool{false, false, false, false}}, // 배출 0발
		} {
			if got := shadowStopSlopeBlock(tc.values, tc.discharged); got != nil {
				t.Errorf("values %v discharged %v → %v, want nil", tc.values, tc.discharged, got)
			}
		}
	})
}

// 정규화 기울기 — 스케일이 달라도 같은 감소율이면 같은 값이어야 한다.
func TestSlopeNorm(t *testing.T) {
	a, okA := slopeNorm([]float64{1.0, 0.9, 0.8})
	b, okB := slopeNorm([]float64{0.5, 0.45, 0.40}) // 절반 스케일, 같은 비율
	if !okA || !okB {
		t.Fatal("계산 실패")
	}
	if diff := a - b; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("스케일이 다른데 값이 다름: %v vs %v", a, b)
	}
	if a >= 0 {
		t.Errorf("하강 열인데 기울기 %v", a)
	}
	// 평균이 0 이하면 판정 불가 (전부 무반응).
	if _, ok := slopeNorm([]float64{0, -0.1, 0}); ok {
		t.Error("평균 0 이하인데 판정함")
	}
	if _, ok := slopeNorm([]float64{0.5}); ok {
		t.Error("1개인데 판정함")
	}
}

func TestRoundTo2(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want float64
	}{{0.3849, 0.38}, {0.385, 0.39}, {-0.0549, -0.05}, {-0.055, -0.06}, {0, 0}} {
		if got := roundTo2(tc.in); got != tc.want {
			t.Errorf("roundTo2(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// 임계 해석 — 수조별 → 공유 → 코드 기본값 순, 항목별 폴백.
func TestResolveShadowStop(t *testing.T) {
	rp := &Reporter{}
	if low, streak, src := rp.resolveShadowStop("tank_01"); low != shadowLowReaction ||
		streak != shadowLowStreak || src != "default" {
		t.Errorf("미설정: got (%v, %d, %q), want (%v, %d, \"default\")",
			low, streak, src, shadowLowReaction, shadowLowStreak)
	}

	rp.shadowStop = config.ShadowStopThresholds{LowReaction: 0.25, LowStreak: 3}
	if low, streak, src := rp.resolveShadowStop("tank_01"); low != 0.25 || streak != 3 ||
		src != "shared" {
		t.Errorf("공유값: got (%v, %d, %q), want (0.25, 3, \"shared\")", low, streak, src)
	}

	// 수조별이 공유를 덮는다. 지정 없는 수조는 공유값 유지.
	rp.shadowStopByTank = map[string]config.ShadowStopThresholds{
		"tank_02": {LowReaction: 0.22},
	}
	if low, streak, src := rp.resolveShadowStop("tank_02"); low != 0.22 || streak != 3 ||
		src != "tank" {
		t.Errorf("수조별: got (%v, %d, %q), want (0.22, 3, \"tank\") — 미지정 항목은 공유값 유지",
			low, streak, src)
	}
	if low, _, src := rp.resolveShadowStop("tank_01"); low != 0.25 || src != "shared" {
		t.Errorf("다른 수조 무영향: got (%v, %q), want (0.25, \"shared\")", low, src)
	}
}

// TestShadowStopAbsoluteBlock — 절대 수준 중단 판정. 사례는 8-17 실측 사이클이다
// (평소 반응 tank_01 0.54 · tank_02 0.78, factor 0.40 → 임계 0.216 / 0.312).
func TestShadowStopAbsoluteBlock(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		ref    float64
		want   int
	}{
		{
			// 8-17 15:37 tank_01 — p4 에서 꺼졌다 p5~p8 에 되살아난다. 비율 규칙은
			// 여기서 끊었으나 평소선(0.216) 위라 통과해야 한다. 반등 보호의 핵심 사례.
			name:   "반등 사이클은 중단하지 않는다",
			values: []float64{0.74, 0.63, 0.69, 0.54, 0.65, 0.55, 0.54, 0.52},
			ref:    0.54,
			want:   0,
		},
		{
			// 8-17 13:43 tank_01 — p4 가 0.21 로 임계(0.216) 아래. 그 펄스는 이미
			// 나갔으므로 4발까지 허용하고 p5 부터 막는다.
			name:   "평소선 아래로 내려가면 그 다음 펄스부터 중단",
			values: []float64{0.42, 0.27, 0.34, 0.21, 0.26, 0.24, 0.17, 0.19},
			ref:    0.54,
			want:   4,
		},
		{
			// 8-17 14:40 tank_02 — p3 가 0.18 로 임계(0.312) 아래.
			name:   "수조별 평소선을 각자 쓴다",
			values: []float64{0.75, 0.41, 0.18, 0.41, 0.45, 0.29, 0.28, 0.19},
			ref:    0.78,
			want:   3,
		},
		{
			// 8-18 10:12 tank_01 — p8 이 0.38 이나 임계 위. 정상 사이클.
			name:   "정상 사이클은 건드리지 않는다",
			values: []float64{0.86, 0.89, 0.80, 0.75, 0.80, 0.76, 0.46, 0.38},
			ref:    0.54,
			want:   0,
		},
		{
			// 마지막 펄스에서 걸리면 막을 것이 없다 — 중단으로 기록하지 않는다.
			name:   "마지막 펄스는 중단 의미가 없다",
			values: []float64{0.74, 0.63, 0.69, 0.54, 0.65, 0.55, 0.54, 0.10},
			ref:    0.54,
			want:   0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := shadowStopAbsoluteBlock(c.values, c.ref, 12)
			if b == nil {
				t.Fatal("판정 블록이 nil")
			}
			if got := b["stop_at_pulse"].(int); got != c.want {
				t.Errorf("stop_at_pulse = %d, want %d (임계 %.3f)",
					got, c.want, c.ref*shadowAbsFactor)
			}
			if b["rule"] != "absolute_level_v1" || b["shadow"] != true {
				t.Errorf("섀도 표식 누락: %v", b)
			}
		})
	}

	// 기준선이 없으면 판정하지 않는다 (수조 이력이 부족한 초기 상태).
	if b := shadowStopAbsoluteBlock([]float64{0.5}, 0, 3); b != nil {
		t.Errorf("ref=0 이면 nil 이어야 한다: %v", b)
	}
}
