package waterquality

import (
	"fmt"
	"math"
	"time"

	"bluei.kr/edge/internal/events"
)

const (
	DORecoveryToleranceMgL             = 0.1
	ReasonInsufficientWaterQualityData = "insufficient_water_quality_data"
	ReasonDegradedInputQuality         = "degraded_input_quality"
	ReasonDODropComputed               = "do_drop_computed"
	ReasonDONoDropOrImproved           = "do_no_drop_or_improved"
	ReasonDORecoveryObserved           = "do_recovery_observed"
	ReasonDORecoveryNotObserved        = "do_recovery_not_observed"
)

type FeedingImpactAnalysis struct {
	AnalysisID    string
	FeedingID     string
	TankID        string
	FeedAmountG   float64
	FedAt         time.Time
	DOBaselineMgL *float64
	DOMinPostMgL  *float64
	DODropMgL     *float64
	DORecoveryMin *int
	PHDelta       *float64
	TempDeltaC    *float64
	Quality       string
	ReasonCodes   []string
}

// AnalyzeFeedingImpact compares water-quality buckets before and after a
// feeding record. It produces observation/interpretation only; it does not make
// control decisions.
func AnalyzeFeedingImpact(feeding events.FeedingRecordedPayload, buckets []WaterQualityBucket) (FeedingImpactAnalysis, error) {
	fedAt, err := time.Parse(time.RFC3339Nano, feeding.FedAt)
	if err != nil {
		return FeedingImpactAnalysis{}, fmt.Errorf("fed_at: %w", err)
	}
	analysis := FeedingImpactAnalysis{
		AnalysisID:  "feed_wq_" + feeding.FeedingID,
		FeedingID:   feeding.FeedingID,
		TankID:      feeding.TankID,
		FeedAmountG: feeding.FeedAmountG,
		FedAt:       fedAt,
		Quality:     events.QualityOK,
	}

	pre := filterBuckets(buckets, feeding.TankID, fedAt.Add(-30*time.Minute), fedAt, true, true)
	post := filterBuckets(buckets, feeding.TankID, fedAt, fedAt.Add(120*time.Minute), false, true)
	if len(pre) == 0 || len(post) == 0 {
		analysis.Quality = events.QualitySuspect
		analysis.ReasonCodes = append(analysis.ReasonCodes, ReasonInsufficientWaterQualityData)
		return analysis, nil
	}
	if hasDegradedBuckets(pre) || hasDegradedBuckets(post) {
		analysis.Quality = events.QualitySuspect
		analysis.ReasonCodes = append(analysis.ReasonCodes, ReasonDegradedInputQuality)
	}

	analysis.DOBaselineMgL = avgDO(pre)
	analysis.DOMinPostMgL = minDO(post)
	if analysis.DOBaselineMgL != nil && analysis.DOMinPostMgL != nil {
		rawDrop := *analysis.DOBaselineMgL - *analysis.DOMinPostMgL
		drop := rawDrop
		if rawDrop <= 0 {
			drop = 0
			analysis.ReasonCodes = append(analysis.ReasonCodes, ReasonDONoDropOrImproved)
		} else {
			analysis.ReasonCodes = append(analysis.ReasonCodes, ReasonDODropComputed)
		}
		analysis.DODropMgL = &drop
		if recoveredAt := firstDORecoveryAt(post, *analysis.DOBaselineMgL); recoveredAt != nil {
			mins := int(math.Round(recoveredAt.Sub(fedAt).Minutes()))
			analysis.DORecoveryMin = &mins
			analysis.ReasonCodes = append(analysis.ReasonCodes, ReasonDORecoveryObserved)
		} else {
			analysis.ReasonCodes = append(analysis.ReasonCodes, ReasonDORecoveryNotObserved)
		}
	}

	analysis.PHDelta = delta(avgPH(pre), avgPH(post))
	analysis.TempDeltaC = delta(avgTemp(pre), avgTemp(post))
	return analysis, nil
}

func filterBuckets(buckets []WaterQualityBucket, tankID string, start, end time.Time, includeStart, includeEnd bool) []WaterQualityBucket {
	var out []WaterQualityBucket
	for _, b := range buckets {
		if b.TankID != tankID || b.Quality == events.QualityStale || b.Quality == events.QualityError {
			continue
		}
		afterStart := b.BucketStart.After(start) || (includeStart && b.BucketStart.Equal(start))
		beforeEnd := b.BucketStart.Before(end) || (includeEnd && b.BucketStart.Equal(end))
		if afterStart && beforeEnd {
			out = append(out, b)
		}
	}
	return out
}

func hasDegradedBuckets(buckets []WaterQualityBucket) bool {
	for _, b := range buckets {
		if b.Quality != events.QualityOK || b.SuspectCount > 0 {
			return true
		}
	}
	return false
}

func avgDO(buckets []WaterQualityBucket) *float64 {
	return avgMetric(buckets, func(b WaterQualityBucket) *float64 { return b.DOMgLAvg })
}
func avgPH(buckets []WaterQualityBucket) *float64 {
	return avgMetric(buckets, func(b WaterQualityBucket) *float64 { return b.PHAvg })
}
func avgTemp(buckets []WaterQualityBucket) *float64 {
	return avgMetric(buckets, func(b WaterQualityBucket) *float64 { return b.TemperatureCAvg })
}
func minDO(buckets []WaterQualityBucket) *float64 {
	return minMetric(buckets, func(b WaterQualityBucket) *float64 { return b.DOMgLAvg })
}

func avgMetric(buckets []WaterQualityBucket, pick func(WaterQualityBucket) *float64) *float64 {
	var sum float64
	var n int
	for _, b := range buckets {
		if v := pick(b); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	v := sum / float64(n)
	return &v
}

func minMetric(buckets []WaterQualityBucket, pick func(WaterQualityBucket) *float64) *float64 {
	var min float64
	var ok bool
	for _, b := range buckets {
		if v := pick(b); v != nil && (!ok || *v < min) {
			min = *v
			ok = true
		}
	}
	if !ok {
		return nil
	}
	return &min
}

func firstDORecoveryAt(buckets []WaterQualityBucket, baseline float64) *time.Time {
	threshold := baseline - DORecoveryToleranceMgL
	for _, b := range buckets {
		if b.DOMgLAvg != nil && *b.DOMgLAvg >= threshold {
			t := b.BucketStart
			return &t
		}
	}
	return nil
}

func delta(before, after *float64) *float64 {
	if before == nil || after == nil {
		return nil
	}
	v := *after - *before
	return &v
}
