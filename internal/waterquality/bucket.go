package waterquality

import (
	"sort"
	"time"

	"bluei.kr/edge/internal/events"
)

const CanonicalBucketSize = 2 * time.Minute

type WaterQualityBucket struct {
	TankID           string
	BucketStart      time.Time
	BucketSec        int
	TemperatureCAvg  *float64
	PHAvg            *float64
	DOMgLAvg         *float64
	Quality          string
	SampleCount      int
	SuspectCount     int
	SourceReadingIDs []string
}

// BuildTwoMinuteBuckets groups readings by tank and two-minute UTC bucket.
// Only OK values contribute to metric averages; non-OK values are preserved in
// counts and degrade bucket quality without being silently discarded upstream.
func BuildTwoMinuteBuckets(readings []events.SensorReadingPayload) []WaterQualityBucket {
	type agg struct {
		bucket WaterQualityBucket
		sums   map[string]float64
		counts map[string]int
	}

	byKey := map[string]*agg{}
	for _, r := range readings {
		observed, err := time.Parse(time.RFC3339Nano, r.ObservedAt)
		if err != nil {
			continue
		}
		start := observed.UTC().Truncate(CanonicalBucketSize)
		tankID := r.Location.TankID
		if tankID == "" {
			tankID = "tank_unknown"
		}
		key := tankID + "|" + start.Format(time.RFC3339Nano)
		a := byKey[key]
		if a == nil {
			a = &agg{
				bucket: WaterQualityBucket{
					TankID:      tankID,
					BucketStart: start,
					BucketSec:   int(CanonicalBucketSize.Seconds()),
					Quality:     events.QualityOK,
				},
				sums:   map[string]float64{},
				counts: map[string]int{},
			}
			byKey[key] = a
		}
		a.bucket.SampleCount++
		a.bucket.SourceReadingIDs = append(a.bucket.SourceReadingIDs, r.ReadingID)
		if r.Quality == events.QualitySuspect {
			a.bucket.SuspectCount++
		}
		a.bucket.Quality = mergeBucketQuality(a.bucket.Quality, r.Quality)
		if r.Quality != events.QualityOK || r.Value == nil {
			continue
		}
		a.sums[r.Metric] += *r.Value
		a.counts[r.Metric]++
	}

	out := make([]WaterQualityBucket, 0, len(byKey))
	for _, a := range byKey {
		a.bucket.TemperatureCAvg = avgPtr(a.sums, a.counts, events.MetricWaterTemperature)
		a.bucket.PHAvg = avgPtr(a.sums, a.counts, events.MetricPH)
		a.bucket.DOMgLAvg = avgPtr(a.sums, a.counts, events.MetricDissolvedOxygen)
		out = append(out, a.bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TankID != out[j].TankID {
			return out[i].TankID < out[j].TankID
		}
		return out[i].BucketStart.Before(out[j].BucketStart)
	})
	return out
}

func avgPtr(sums map[string]float64, counts map[string]int, metric string) *float64 {
	if counts[metric] == 0 {
		return nil
	}
	v := sums[metric] / float64(counts[metric])
	return &v
}

func mergeBucketQuality(current, incoming string) string {
	if current == "" {
		return incoming
	}
	if qualityRank(incoming) > qualityRank(current) {
		return incoming
	}
	return current
}

func qualityRank(q string) int {
	switch q {
	case events.QualityOK:
		return 0
	case events.QualitySuspect:
		return 1
	case events.QualityStale:
		return 2
	case events.QualityMissing, events.QualityError:
		return 3
	default:
		return 1
	}
}
