package biomass

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 합리적 FCR 범위 (양식 산업 표준).
const (
	MinReasonableFCR = 0.5
	MaxReasonableFCR = 3.0
)

// 보정 가능 최소 운영 기간 (입식 → sampling).
const MinCalibrationPeriodDays = 7

// CalibrationResult — 보정 산출 결과. Performed=false 면 사유는 Reason.
type CalibrationResult struct {
	Performed         bool     `json:"performed"`
	Reason            string   `json:"reason,omitempty"` // 미수행 사유
	CalibratedFCR     float64  `json:"calibrated_fcr,omitempty"`
	DefaultFCR        float64  `json:"default_fcr,omitempty"`
	ObservedFCR       float64  `json:"observed_fcr,omitempty"`
	DeviationPct      float64  `json:"deviation_pct,omitempty"`
	DeltaBiomassG     float64  `json:"delta_biomass_g,omitempty"`
	CumulativeFeedG   float64  `json:"cumulative_feed_g,omitempty"`
	DaysSinceStocking int      `json:"days_since_stocking,omitempty"`
	Notes             []string `json:"notes,omitempty"`
}

// CalibrateFromSampling — store 에서 lifecycle/sampling/feeding 읽어
// observed FCR 산출. 합리적이면 Performed=true.
// 호출자가 result 받아 storage upsert + audit event 적재.
func CalibrateFromSampling(ctx context.Context, store storage.Store, tankID, samplingID string) (CalibrationResult, error) {
	// 1. 활성 lifecycle 조회
	lc, err := store.GetTankLifecycle(ctx, tankID)
	if err != nil {
		return CalibrationResult{Performed: false, Reason: "no_active_lifecycle"}, nil
	}
	if lc == nil || lc.Status != "active" {
		return CalibrationResult{Performed: false, Reason: "no_active_lifecycle"}, nil
	}

	// 2. 최신 sampling 조회 + sampling_id 일치 확인
	ts, err := store.GetTankSampling(ctx, tankID)
	if err != nil || ts == nil {
		return CalibrationResult{Performed: false, Reason: "no_sampling"}, nil
	}
	if ts.LatestSamplingID != samplingID {
		// race condition 방지 — latest 와 다른 sampling_id 는 처리 거부
		return CalibrationResult{Performed: false, Reason: "sampling_id_mismatch"}, nil
	}

	// 3. 운영 기간 검증
	daysSinceStocking := int(ts.SampledAt.Sub(lc.StockedAt).Hours() / 24)
	if daysSinceStocking < 0 {
		daysSinceStocking = 0
	}
	if daysSinceStocking < MinCalibrationPeriodDays {
		return CalibrationResult{
			Performed:         false,
			Reason:            "period_too_short",
			DaysSinceStocking: daysSinceStocking,
		}, nil
	}

	// 4. 증체량 산출
	deltaBiomass := (ts.AvgWeightG - lc.InitialAvgWeightG) * float64(lc.InitialCount)
	if deltaBiomass <= 0 {
		return CalibrationResult{
			Performed:         false,
			Reason:            "non_positive_growth",
			DaysSinceStocking: daysSinceStocking,
		}, nil
	}

	// 5. 입식 이후 sampling 시점까지 누적 급이 합산
	feedEvts, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeedingRecorded,
		Since:     &lc.StockedAt,
		Limit:     10000,
	})
	var cumulativeFeed float64
	if err == nil {
		for _, e := range feedEvts {
			var p events.FeedingRecordedPayload
			if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
				continue
			}
			if p.TankID != tankID {
				continue
			}
			fedAt, err := time.Parse(time.RFC3339Nano, p.FedAt)
			if err != nil {
				continue
			}
			// 입식 이후 ~ sampling 시점까지만 집계
			if fedAt.Before(lc.StockedAt) || fedAt.After(ts.SampledAt) {
				continue
			}
			cumulativeFeed += p.FeedAmountG
		}
	}
	if cumulativeFeed <= 0 {
		return CalibrationResult{
			Performed:         false,
			Reason:            "no_feeding_in_period",
			DaysSinceStocking: daysSinceStocking,
		}, nil
	}

	// 6. observed FCR 산출 + 범위 검증
	observedFCR := cumulativeFeed / deltaBiomass
	if observedFCR < MinReasonableFCR || observedFCR > MaxReasonableFCR {
		return CalibrationResult{
			Performed:         false,
			Reason:            fmt.Sprintf("fcr_out_of_range (%.4f)", observedFCR),
			DaysSinceStocking: daysSinceStocking,
		}, nil
	}

	// 7. 테이블 default FCR 조회
	defaultFCR, _ := LookupFCR(lc.Species, lc.GrowthStage)

	// 8. 편차율 계산
	deviationPct := (observedFCR - defaultFCR) / defaultFCR * 100.0

	// 9. 결과 조합
	result := CalibrationResult{
		Performed:         true,
		CalibratedFCR:     observedFCR,
		DefaultFCR:        defaultFCR,
		ObservedFCR:       observedFCR,
		DeviationPct:      deviationPct,
		DeltaBiomassG:     deltaBiomass,
		CumulativeFeedG:   cumulativeFeed,
		DaysSinceStocking: daysSinceStocking,
		Notes:             []string{},
	}

	// 10. 노트 — 편차 과다 경고
	if deviationPct > 30 || deviationPct < -30 {
		result.Notes = append(result.Notes,
			"정상 편차 범위 (±30%) 초과 — sampling 정확도 재확인 권장")
	} else {
		result.Notes = append(result.Notes, "보정 적용됨")
	}

	return result, nil
}
