package api

import "testing"

func TestRisePercentile(t *testing.T) {
	// 표본 부족 → 미확보.
	if _, ok := risePercentile([]float64{0.1, 0.2}, 0.9); ok {
		t.Fatal("expected not ok for small sample")
	}
	// 20표본 0.01~0.20 → P90 = 정렬 인덱스 18 = 0.19.
	rises := make([]float64, 20)
	for i := range rises {
		rises[i] = float64(i+1) / 100
	}
	p90, ok := risePercentile(rises, 0.9)
	if !ok || p90 < 0.18 || p90 > 0.20 {
		t.Fatalf("p90 = %v ok=%v", p90, ok)
	}
	// 전부 0 → 스케일 불가.
	if _, ok := risePercentile(make([]float64, 20), 0.9); ok {
		t.Fatal("expected not ok for zero rises")
	}
}

func TestCalibratedRiseScore(t *testing.T) {
	// tank_02 급: P90 0.55 에서 +34% 상승 → ~0.62 (legacy 0.34 에서 회복).
	if got := calibratedRiseScore(0.34, 0.55); got < 0.60 || got > 0.65 {
		t.Fatalf("calibrated = %v", got)
	}
	// P90 초과는 1.0 포화.
	if got := calibratedRiseScore(0.80, 0.55); got != 1.0 {
		t.Fatalf("saturate = %v", got)
	}
	// P90 미확보(0) → legacy clamp(rise).
	if got := calibratedRiseScore(0.34, 0); got != 0.34 {
		t.Fatalf("legacy = %v", got)
	}
	if got := calibratedRiseScore(-0.1, 0.55); got != 0 {
		t.Fatalf("negative clamp = %v", got)
	}
}

func TestSegmentPriorScore(t *testing.T) {
	// 앵커 우선 — 절대 상승폭 ÷ 앵커. 기저 20 · 피크 21.91 = 상승폭 1.91,
	// 앵커 3.82 (cam_top_7_1_2_L) → 0.50. P90 인자는 무시돼야 한다.
	if got := segmentPriorScore(20, 21.91, 3.82, 0.55); got < 0.49 || got > 0.51 {
		t.Fatalf("anchored = %v", got)
	}
	// 앵커 초과는 1.0 포화.
	if got := segmentPriorScore(20, 25, 3.82, 0.55); got != 1.0 {
		t.Fatalf("saturate = %v", got)
	}
	// 기저 아래(음수 상승폭)는 0.
	if got := segmentPriorScore(20, 19, 3.82, 0.55); got != 0 {
		t.Fatalf("negative clamp = %v", got)
	}
	// 앵커 미설정 카메라 → 종전 상승률 ÷ P90 폴백. 상승률 (21.91−20)/20 = 0.0955,
	// P90 0.55 → ~0.17.
	if got := segmentPriorScore(20, 21.91, 0, 0.55); got < 0.16 || got > 0.18 {
		t.Fatalf("fallback = %v", got)
	}
	// 기저 0 (곡선 미확보) → 앵커가 있어도 폴백 경로, PeakRiseFrac 가드로 0.
	if got := segmentPriorScore(0, 5, 3.82, 0.55); got != 0 {
		t.Fatalf("no baseline = %v", got)
	}
}
