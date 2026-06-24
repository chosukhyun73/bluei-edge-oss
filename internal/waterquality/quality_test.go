package waterquality

import (
	"testing"

	"bluei.kr/edge/internal/events"
)

func TestClassifyReadingFlagsZeroAndRangeValues(t *testing.T) {
	tests := []struct {
		name       string
		metric     string
		value      *float64
		wantQ      string
		wantReason string
	}{
		{"normal_do", events.MetricDissolvedOxygen, ptr(8.4), events.QualityOK, ReasonNormal},
		{"zero_do", events.MetricDissolvedOxygen, ptr(0), events.QualitySuspect, ReasonInvalidZero},
		{"zero_ph", events.MetricPH, ptr(0), events.QualitySuspect, ReasonInvalidZero},
		{"high_ph", events.MetricPH, ptr(13.52), events.QualitySuspect, ReasonInvalidRange},
		{"zero_temperature", events.MetricWaterTemperature, ptr(0), events.QualitySuspect, ReasonInvalidZero},
		{"missing", events.MetricDissolvedOxygen, nil, events.QualityMissing, ReasonNoValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyReading(tt.metric, tt.value)
			if got.Quality != tt.wantQ {
				t.Fatalf("quality = %q, want %q", got.Quality, tt.wantQ)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func ptr(v float64) *float64 { return &v }
