package feed_cycle

import "testing"

func TestR3ShadowClipDurationSec(t *testing.T) {
	tests := []struct {
		name  string
		gapMs int
		want  int
	}{
		{"typical 60s gap + tail", 60_000, 62},
		{"typical 90s gap + tail", 90_000, 92},
		{"floored at min for tiny gap", 1_000, r3ShadowClipMinSec},
		{"zero gap floors to min", 0, r3ShadowClipMinSec},
		{"capped at max for huge gap", 600_000, r3ShadowClipMaxSec},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r3ShadowClipDurationSec(tt.gapMs); got != tt.want {
				t.Errorf("r3ShadowClipDurationSec(%d) = %d, want %d", tt.gapMs, got, tt.want)
			}
		})
	}
}

func TestR3ShadowThresholdDefault(t *testing.T) {
	// 0 → 0.5 기본.
	w := &Worker{}
	if got := w.r3ShadowThreshold(); got != 0.5 {
		t.Errorf("default threshold = %v, want 0.5", got)
	}
	// 명시값은 그대로.
	w.cfg.R3ShadowThreshold = 0.65
	if got := w.r3ShadowThreshold(); got != 0.65 {
		t.Errorf("configured threshold = %v, want 0.65", got)
	}
}

func TestR3ShadowEligibleDisabledByDefault(t *testing.T) {
	// R3ShadowEnabled=false 면 의존성 유무와 무관히 비활성 (기본 안전).
	w := &Worker{}
	c := &Cycle{Mode: ModeFixed}
	if w.r3ShadowEligible(c) {
		t.Error("r3 shadow must be disabled when R3ShadowEnabled=false")
	}
}
