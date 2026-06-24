package waterquality

import (
	"encoding/json"

	"bluei.kr/edge/internal/storage"
)

func BucketToStorageProjection(b WaterQualityBucket) (*storage.WaterQualityBucketProjection, error) {
	ids, err := json.Marshal(b.SourceReadingIDs)
	if err != nil {
		return nil, err
	}
	return &storage.WaterQualityBucketProjection{
		TankID:           b.TankID,
		BucketStart:      b.BucketStart,
		BucketSec:        b.BucketSec,
		TemperatureCAvg:  b.TemperatureCAvg,
		PHAvg:            b.PHAvg,
		DOMgLAvg:         b.DOMgLAvg,
		Quality:          b.Quality,
		SampleCount:      b.SampleCount,
		SuspectCount:     b.SuspectCount,
		SourceReadingIDs: string(ids),
	}, nil
}

func ImpactToStorageProjection(a FeedingImpactAnalysis) (*storage.FeedingImpactAnalysisProjection, error) {
	reasons, err := json.Marshal(a.ReasonCodes)
	if err != nil {
		return nil, err
	}
	return &storage.FeedingImpactAnalysisProjection{
		AnalysisID:      a.AnalysisID,
		FeedingID:       a.FeedingID,
		TankID:          a.TankID,
		FeedAmountG:     a.FeedAmountG,
		FedAt:           a.FedAt,
		DOBaselineMgL:   a.DOBaselineMgL,
		DOMinPostMgL:    a.DOMinPostMgL,
		DODropMgL:       a.DODropMgL,
		DORecoveryMin:   a.DORecoveryMin,
		PHDelta:         a.PHDelta,
		TempDeltaC:      a.TempDeltaC,
		Quality:         a.Quality,
		ReasonCodesJSON: string(reasons),
	}, nil
}
