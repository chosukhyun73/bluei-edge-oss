package waterquality

import (
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
)

func TestBuildTwoMinuteBucketsAggregatesReadings(t *testing.T) {
	base := time.Date(2026, 5, 8, 1, 0, 0, 0, time.UTC)
	readings := []events.SensorReadingPayload{
		reading("tank_1", events.MetricDissolvedOxygen, 8.0, events.QualityOK, base.Add(10*time.Second)),
		reading("tank_1", events.MetricDissolvedOxygen, 8.4, events.QualityOK, base.Add(70*time.Second)),
		reading("tank_1", events.MetricPH, 0, events.QualitySuspect, base.Add(80*time.Second)),
		reading("tank_1", events.MetricDissolvedOxygen, 7.5, events.QualityOK, base.Add(130*time.Second)),
	}

	buckets := BuildTwoMinuteBuckets(readings)
	if len(buckets) != 2 {
		t.Fatalf("bucket len = %d, want 2", len(buckets))
	}

	first := buckets[0]
	if first.TankID != "tank_1" || !first.BucketStart.Equal(base) {
		t.Fatalf("first bucket key = %s %s", first.TankID, first.BucketStart)
	}
	if first.SampleCount != 3 || first.SuspectCount != 1 {
		t.Fatalf("counts = sample %d suspect %d", first.SampleCount, first.SuspectCount)
	}
	if first.Quality != events.QualitySuspect {
		t.Fatalf("quality = %q, want suspect", first.Quality)
	}
	if first.DOMgLAvg == nil || round2(*first.DOMgLAvg) != 8.2 {
		t.Fatalf("DO avg = %v, want 8.2", first.DOMgLAvg)
	}
	if first.PHAvg != nil {
		t.Fatalf("suspect pH should be excluded from avg, got %v", *first.PHAvg)
	}

	second := buckets[1]
	if !second.BucketStart.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("second bucket start = %s", second.BucketStart)
	}
	if second.DOMgLAvg == nil || *second.DOMgLAvg != 7.5 {
		t.Fatalf("second DO avg = %v", second.DOMgLAvg)
	}
}

func TestBuildTwoMinuteBucketsMarksErrorWhenOnlyErrorSamplesExist(t *testing.T) {
	base := time.Date(2026, 5, 8, 1, 0, 0, 0, time.UTC)
	buckets := BuildTwoMinuteBuckets([]events.SensorReadingPayload{
		reading("tank_1", events.MetricDissolvedOxygen, 0, events.QualityError, base),
	})
	if len(buckets) != 1 {
		t.Fatalf("bucket len = %d, want 1", len(buckets))
	}
	if buckets[0].Quality != events.QualityError {
		t.Fatalf("quality = %q, want error", buckets[0].Quality)
	}
	if buckets[0].DOMgLAvg != nil {
		t.Fatalf("error value should not be averaged: %v", *buckets[0].DOMgLAvg)
	}
}

func reading(tankID, metric string, value float64, quality string, observedAt time.Time) events.SensorReadingPayload {
	return events.SensorReadingPayload{
		ReadingID:  "r_" + metric,
		SensorID:   "sensor_1",
		DeviceID:   "device_1",
		Metric:     metric,
		Value:      &value,
		Unit:       "unit",
		Quality:    quality,
		ObservedAt: observedAt.Format(time.RFC3339Nano),
		Location:   events.Location{TankID: tankID},
	}
}

func round2(v float64) float64 {
	if v < 0 {
		return float64(int(v*100-0.5)) / 100
	}
	return float64(int(v*100+0.5)) / 100
}
