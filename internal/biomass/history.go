package biomass

import (
	"context"
	"time"

	"bluei.kr/edge/internal/storage"
)

// SnapshotForTank — LoadAndProject 결과를 storage 에 upsert.
// active lifecycle 없으면 skip (ok=false, err=nil).
func SnapshotForTank(ctx context.Context, store storage.Store, tankID string, now time.Time) (bool, error) {
	result, ok, err := LoadAndProject(ctx, store, tankID)
	if err != nil {
		return false, err
	}
	if !ok || result.Quality == "no_lifecycle" {
		return false, nil
	}

	loc, locErr := time.LoadLocation("Asia/Seoul")
	if locErr != nil {
		loc = time.UTC
	}
	nowSeoul := now.In(loc)
	snapshotDate := nowSeoul.Format("2006-01-02")

	snap := &storage.TankWeightSnapshot{
		TankID:              tankID,
		SnapshotDate:        snapshotDate,
		EstimatedAvgWeightG: result.EstimatedAvgWeightG,
		AnchorWeightG:       result.AnchorWeightG,
		AnchorSource:        result.AnchorSource,
		DaysSinceAnchor:     result.DaysSinceAnchor,
		ExpectedFCR:         result.ExpectedFCR,
		FCRSource:           result.FCRSource,
		CumulativeFeedG:     result.CumulativeFeedG,
		Quality:             result.Quality,
		SnapshotAt:          now.UTC(),
	}
	if err := store.UpsertTankWeightSnapshot(ctx, snap); err != nil {
		return false, err
	}
	return true, nil
}
