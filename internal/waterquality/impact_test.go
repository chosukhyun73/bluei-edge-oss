package waterquality

import (
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
)

func TestAnalyzeFeedingImpactComputesDODropAndRecovery(t *testing.T) {
	fedAt := time.Date(2026, 5, 8, 1, 30, 0, 0, time.UTC)
	buckets := []WaterQualityBucket{
		bucket("tank_1", fedAt.Add(-30*time.Minute), 8.6, 7.7, 12.1),
		bucket("tank_1", fedAt.Add(-2*time.Minute), 8.4, 7.7, 12.1),
		bucket("tank_1", fedAt.Add(2*time.Minute), 8.0, 7.68, 12.1),
		bucket("tank_1", fedAt.Add(20*time.Minute), 8.2, 7.66, 12.2),
		bucket("tank_1", fedAt.Add(42*time.Minute), 8.45, 7.66, 12.2),
	}
	feeding := events.FeedingRecordedPayload{
		FeedingID:   "feeding_1",
		TankID:      "tank_1",
		Source:      events.FeedingSourceManual,
		FeedAmountG: 450,
		FedAt:       fedAt.Format(time.RFC3339Nano),
		Quality:     events.QualityOK,
	}

	got, err := AnalyzeFeedingImpact(feeding, buckets)
	if err != nil {
		t.Fatalf("AnalyzeFeedingImpact error: %v", err)
	}
	if got.Quality != events.QualityOK {
		t.Fatalf("quality = %q", got.Quality)
	}
	if got.DOBaselineMgL == nil || round2(*got.DOBaselineMgL) != 8.5 {
		t.Fatalf("baseline = %v, want 8.5", got.DOBaselineMgL)
	}
	if got.DOMinPostMgL == nil || *got.DOMinPostMgL != 8.0 {
		t.Fatalf("min post = %v, want 8.0", got.DOMinPostMgL)
	}
	if got.DODropMgL == nil || round2(*got.DODropMgL) != 0.5 {
		t.Fatalf("drop = %v, want 0.5", got.DODropMgL)
	}
	if got.DORecoveryMin == nil || *got.DORecoveryMin != 42 {
		t.Fatalf("recovery = %v, want 42", got.DORecoveryMin)
	}
	if got.PHDelta == nil || round2(*got.PHDelta) != -0.03 {
		t.Fatalf("pH delta = %v, want -0.03", got.PHDelta)
	}
	if got.TempDeltaC == nil || round2(*got.TempDeltaC) != 0.07 {
		t.Fatalf("temp delta = %v, want 0.07", got.TempDeltaC)
	}
}

func TestAnalyzeFeedingImpactDegradesWhenDataIsMissing(t *testing.T) {
	fedAt := time.Date(2026, 5, 8, 1, 30, 0, 0, time.UTC)
	feeding := events.FeedingRecordedPayload{
		FeedingID:   "feeding_1",
		TankID:      "tank_1",
		Source:      events.FeedingSourceManual,
		FeedAmountG: 450,
		FedAt:       fedAt.Format(time.RFC3339Nano),
		Quality:     events.QualityOK,
	}

	got, err := AnalyzeFeedingImpact(feeding, nil)
	if err != nil {
		t.Fatalf("AnalyzeFeedingImpact error: %v", err)
	}
	if got.Quality != events.QualitySuspect {
		t.Fatalf("quality = %q, want suspect", got.Quality)
	}
	if len(got.ReasonCodes) == 0 || got.ReasonCodes[0] != ReasonInsufficientWaterQualityData {
		t.Fatalf("reason codes = %#v", got.ReasonCodes)
	}
}

func TestAnalyzeFeedingImpactSeparatesNoDropAndDegradedInput(t *testing.T) {
	fedAt := time.Date(2026, 5, 8, 1, 30, 0, 0, time.UTC)
	pre := bucket("tank_1", fedAt.Add(-2*time.Minute), 8.0, 7.7, 12.1)
	post := bucket("tank_1", fedAt.Add(2*time.Minute), 8.2, 7.7, 12.1)
	post.Quality = events.QualitySuspect
	post.SuspectCount = 1
	feeding := events.FeedingRecordedPayload{
		FeedingID:   "feeding_1",
		TankID:      "tank_1",
		Source:      events.FeedingSourceManual,
		FeedAmountG: 450,
		FedAt:       fedAt.Format(time.RFC3339Nano),
		Quality:     events.QualityOK,
	}

	got, err := AnalyzeFeedingImpact(feeding, []WaterQualityBucket{pre, post})
	if err != nil {
		t.Fatalf("AnalyzeFeedingImpact error: %v", err)
	}
	if got.Quality != events.QualitySuspect {
		t.Fatalf("quality = %q, want suspect", got.Quality)
	}
	if !hasReason(got.ReasonCodes, ReasonDegradedInputQuality) {
		t.Fatalf("missing degraded reason: %#v", got.ReasonCodes)
	}
	if !hasReason(got.ReasonCodes, ReasonDONoDropOrImproved) {
		t.Fatalf("missing no-drop reason: %#v", got.ReasonCodes)
	}
	if got.DODropMgL == nil || *got.DODropMgL != 0 {
		t.Fatalf("drop = %v, want 0", got.DODropMgL)
	}
}

func hasReason(reasons []string, want string) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func bucket(tankID string, start time.Time, do, ph, temp float64) WaterQualityBucket {
	return WaterQualityBucket{
		TankID:          tankID,
		BucketStart:     start,
		BucketSec:       120,
		DOMgLAvg:        &do,
		PHAvg:           &ph,
		TemperatureCAvg: &temp,
		Quality:         events.QualityOK,
		SampleCount:     3,
	}
}
