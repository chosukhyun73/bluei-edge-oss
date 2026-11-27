package vision_pipeline

import "testing"

func TestJudgeStopPulse(t *testing.T) {
	u, tt := TrackUParams(), TrackTParams()
	cases := []struct {
		name   string
		priors []float64
		p      JudgeParams
		want   int
	}{
		// 오프라인 재생 실측 사례 (2026-07-05 tank_02 07:59) — p4~p7 반응 소멸.
		{"tank02_dieoff_U", []float64{0.62, 0.69, 1.00, 0.00, 0.00, 0.00, 0.00}, u, 5},
		{"tank02_dieoff_T", []float64{0.62, 0.69, 1.00, 0.00, 0.00, 0.00, 0.00}, tt, 5},
		// 07-05 tank_02 07:33 — 점진 하강: T 가 U 보다 한 펄스 먼저.
		{"tank02_decline_U", []float64{0.68, 0.60, 0.28, 0.14, 0.11, 0.01, 0.22}, u, 5},
		{"tank02_decline_T", []float64{0.68, 0.60, 0.28, 0.14, 0.11, 0.01, 0.22}, tt, 4},
		// 강반응 유지 — 중단 없음.
		{"strong_all", []float64{0.85, 0.86, 0.82, 0.86, 0.83, 0.84, 0.70}, u, 0},
		// 강반응이 누적을 리셋.
		{"reset_on_strong", []float64{0.05, 0.80, 0.05, 0.80, 0.05}, u, 0},
		// 약반응만 연속 — U 는 무시, T 는 3연속(0.5×3)에서 중단.
		{"weak_only_U", []float64{0.2, 0.2, 0.2, 0.2}, u, 0},
		{"weak_only_T", []float64{0.2, 0.2, 0.2, 0.2}, tt, 3},
		{"empty", nil, u, 0},
	}
	for _, c := range cases {
		if got := JudgeStopPulse(c.priors, c.p); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
