package vision_pipeline

import "testing"

func TestParseAgitationOutput(t *testing.T) {
	good := []byte(`{"n_seconds":207,"baseline":9.84,"peak":12.29,"peak_at_s":26,"last5_mean":9.62,"calm_reached":true,"auc_above_baseline":192.06,"series":[9.85,9.9,10.13]}`)
	m, err := parseAgitationOutput(good)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Peak != 12.29 || m.PeakAtS != 26 || !m.CalmReached || len(m.Series) != 3 {
		t.Errorf("parsed mismatch: %+v", m)
	}

	if _, err := parseAgitationOutput([]byte(`{"error":"clip too short","n":1}`)); err == nil {
		t.Error("expected error for script error output")
	}
	if _, err := parseAgitationOutput([]byte(`not json`)); err == nil {
		t.Error("expected error for invalid json")
	}
	if _, err := parseAgitationOutput([]byte(`{"n_seconds":0,"series":[]}`)); err == nil {
		t.Error("expected error for empty output")
	}
}

func TestSegmentPrior(t *testing.T) {
	// baseline 10. 세그먼트 A(0..3s) 낮음 = 리드, 세그먼트 B(3..6s) 피크 20 = 강반응.
	series := []float64{10, 11, 10, 12, 20, 15, 9, 9}

	// 세그먼트 B: peak 20 → rise = (20-10)/10 = 1.0 → clamp 1.0.
	prior, peak, mean, n := SegmentPrior(series, 10, 3, 6)
	if n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}
	if peak != 20 {
		t.Errorf("peak = %v, want 20", peak)
	}
	if mean < 15.6 || mean > 15.7 { // (12+20+15)/3 = 15.667
		t.Errorf("mean = %v, want ~15.67", mean)
	}
	if prior != 1.0 {
		t.Errorf("prior = %v, want 1.0 (saturated)", prior)
	}

	// 세그먼트 A(리드): peak 11 → rise = 0.1 → prior 0.1.
	priorA, _, _, _ := SegmentPrior(series, 10, 0, 3)
	if priorA < 0.09 || priorA > 0.11 {
		t.Errorf("lead prior = %v, want ~0.1", priorA)
	}

	// 범위 밖 clamp: endSec > len → len 으로.
	_, _, _, nOut := SegmentPrior(series, 10, 6, 100)
	if nOut != 2 { // index 6,7
		t.Errorf("out-of-range n = %d, want 2", nOut)
	}

	// 빈 구간 / 빈 series.
	if _, _, _, z := SegmentPrior(series, 10, 5, 5); z != 0 {
		t.Errorf("empty window n = %d, want 0", z)
	}
	if _, _, _, z := SegmentPrior(nil, 10, 0, 3); z != 0 {
		t.Errorf("nil series n = %d, want 0", z)
	}

	// baseline<=0 → prior 0 (PeakRiseFrac 가드).
	if p, _, _, _ := SegmentPrior(series, 0, 3, 6); p != 0 {
		t.Errorf("baseline 0 prior = %v, want 0", p)
	}
}
