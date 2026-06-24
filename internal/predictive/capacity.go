package predictive

import (
	"context"
	"time"

	st "bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/wtg"
)

const recentWindowMin = 30

// CapacityHeadroom is the D-7 output.
type CapacityHeadroom struct {
	MaxProcessingKgPerH float64 // WTG rated capacity
	ActiveLoadKgPerH    float64 // sum of recent active cycles
	HeadroomKgPerH      float64 // max - active
}

// ComputeHeadroom returns the remaining NH3 processing capacity for a WTG (D-7 light).
// "Active load" is derived from feed_cycles started within the last 30 minutes that
// are not yet completed — treated as a simple NH3 rate proxy using total_amount_g
// and a species-average NH3 factor baked into the WasteEstimator.
//
// For Phase 4 minimal: load is estimated from cycle total_amount_g × default NH3 factor.
// TODO(Phase 4 후속): D-8 LSTM 통합으로 실측 NH3 농도 기반 예측으로 교체.
func ComputeHeadroom(ctx context.Context, store st.Store, group *wtg.Group) (CapacityHeadroom, error) {
	cap := group.Capacity
	result := CapacityHeadroom{
		MaxProcessingKgPerH: cap.NH3ProcessingKgPerH,
	}

	if cap.NH3ProcessingKgPerH == 0 {
		// No capacity configured — headroom is effectively unlimited.
		return result, nil
	}

	// Sum NH3 load from active + recently completed cycles in last 30 min.
	since := time.Now().UTC().Add(-recentWindowMin * time.Minute)
	activeLoad, err := sumRecentNH3Load(ctx, store, group.TankIDs, since)
	if err != nil {
		return result, err
	}

	result.ActiveLoadKgPerH = activeLoad
	result.HeadroomKgPerH = cap.NH3ProcessingKgPerH - activeLoad
	return result, nil
}

// sumRecentNH3Load iterates recent feed cycles across all tanks in the WTG
// and estimates total NH3 load (kg/h) using a default NH3 factor.
//
// Phase 4 minimal: proxy = total_amount_g × defaultNH3FactorKgPerKgFeed / cycleDurationH.
// CycleDurationH assumed 1h when not computable from DB data.
func sumRecentNH3Load(ctx context.Context, store st.Store, tankIDs []string, since time.Time) (float64, error) {
	// Default NH3 factor: 30g NH3 per kg feed × (1-0.85) efficiency = 4.5g/kg → 0.0045 kg/kg
	const nh3FactorKgPerKgFeed = 0.0045 // kg NH3 per kg feed dispensed
	const assumedCycleDurationH = 1.0

	var totalKgPerH float64

	for _, tankID := range tankIDs {
		cycles, err := store.ListRecentFeedCycles(ctx, tankID, 50)
		if err != nil {
			return 0, err
		}
		for _, c := range cycles {
			// include active cycles and cycles started within window
			if c.StartedAt.Before(since) {
				continue
			}
			feedKg := c.TotalAmountG / 1000.0
			nh3Kg := feedKg * nh3FactorKgPerKgFeed
			totalKgPerH += nh3Kg / assumedCycleDurationH
		}
	}
	return totalKgPerH, nil
}
