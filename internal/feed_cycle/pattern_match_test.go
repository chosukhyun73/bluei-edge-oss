package feed_cycle

import (
	"math"
	"testing"
	"time"
)

// 합성 곡선 — 펄스마다 [지연 → 정점 → 감쇠] 반응을 얹고, 완만한 추세와 흔들림을 섞는다.
// respScale=0 이면 반응이 전혀 없는 무배출 사이클.
func synthCurve(start time.Time, pulseStarts []time.Time, n int, respScale float64) agitationCurve {
	s := make([]float64, n)
	for i := range s {
		// 기저 6 + 완만한 드리프트 + 결정적 흔들림 (테스트 재현성 위해 난수 미사용)
		s[i] = 6.0 + 0.4*math.Sin(float64(i)/17.0) + 0.15*math.Sin(float64(i)/3.0)
	}
	for _, p := range pulseStarts {
		r := int(p.Sub(start).Seconds())
		for t := r + 10; t < r+70 && t < n; t++ {
			if t < 0 {
				continue
			}
			dt := float64(t - (r + 15))
			if dt >= 0 {
				s[t] += respScale * math.Exp(-dt/25.0)
			} else {
				s[t] += respScale * math.Exp(dt/4.0)
			}
		}
	}
	return agitationCurve{Series: s, Baseline: 6.0, StartAt: start, CameraID: "cam_test"}
}

func pulseTimes(start time.Time, count, everySec int) []time.Time {
	out := make([]time.Time, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, start.Add(time.Duration(i*everySec)*time.Second))
	}
	return out
}

func segsAt(starts []time.Time) []Segment {
	segs := make([]Segment, 0, len(starts))
	for i, t := range starts {
		segs = append(segs, Segment{Index: i, PulseNo: i + 1, StartAbs: t})
	}
	return segs
}

func TestPulsePatternMatch_DischargedCycleScoresHigh(t *testing.T) {
	start := time.Date(2026, 7, 29, 0, 30, 0, 0, time.UTC)
	pulses := pulseTimes(start, 8, 70)
	c := synthCurve(start, pulses, 560, 2.5)

	got, ok := pulsePatternMatch(segsAt(pulses), []agitationCurve{c})
	if !ok {
		t.Fatal("판정 가능해야 한다")
	}
	if got < 0.5 {
		t.Errorf("배출 사이클 일치도가 너무 낮다: %.3f (>=0.5 기대)", got)
	}
}

func TestPulsePatternMatch_NoDischargeCycleScoresLow(t *testing.T) {
	start := time.Date(2026, 7, 28, 7, 30, 0, 0, time.UTC)
	pulses := pulseTimes(start, 8, 70)
	c := synthCurve(start, pulses, 560, 0) // 반응 없음 = 전량 무배출

	got, ok := pulsePatternMatch(segsAt(pulses), []agitationCurve{c})
	if !ok {
		t.Fatal("판정 가능해야 한다")
	}
	if got >= patternMatchWeak {
		t.Errorf("무배출 사이클이 임계 위로 나왔다: %.3f (<%.2f 기대)", got, patternMatchWeak)
	}
}

func TestPulsePatternMatch_TakesBestCurve(t *testing.T) {
	start := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	pulses := pulseTimes(start, 8, 70)
	flat := synthCurve(start, pulses, 560, 0)
	good := synthCurve(start, pulses, 560, 2.5)

	got, ok := pulsePatternMatch(segsAt(pulses), []agitationCurve{flat, good})
	if !ok {
		t.Fatal("판정 가능해야 한다")
	}
	if got < 0.5 {
		t.Errorf("한 카메라라도 패턴을 봤으면 그 값을 써야 한다: %.3f", got)
	}
}

func TestPulsePatternMatch_InsufficientData(t *testing.T) {
	start := time.Date(2026, 7, 29, 1, 30, 0, 0, time.UTC)

	// 펄스 부족
	few := pulseTimes(start, 2, 70)
	if _, ok := pulsePatternMatch(segsAt(few), []agitationCurve{synthCurve(start, few, 300, 2.5)}); ok {
		t.Error("펄스 2발이면 판정 불가여야 한다")
	}

	// 곡선 없음
	pulses := pulseTimes(start, 8, 70)
	if _, ok := pulsePatternMatch(segsAt(pulses), nil); ok {
		t.Error("곡선이 없으면 판정 불가여야 한다")
	}

	// 곡선이 너무 짧음
	short := synthCurve(start, pulses, 30, 2.5)
	if _, ok := pulsePatternMatch(segsAt(pulses), []agitationCurve{short}); ok {
		t.Error("곡선이 짧으면 판정 불가여야 한다")
	}
}

func TestPulsePatternMatch_FlatSeriesIsNotJudged(t *testing.T) {
	start := time.Date(2026, 7, 29, 2, 0, 0, 0, time.UTC)
	pulses := pulseTimes(start, 8, 70)
	flat := agitationCurve{Series: make([]float64, 560), Baseline: 0, StartAt: start, CameraID: "cam_dead"}

	if _, ok := pulsePatternMatch(segsAt(pulses), []agitationCurve{flat}); ok {
		t.Error("분산 0(신호 없음)이면 판정 불가여야 한다")
	}
}
