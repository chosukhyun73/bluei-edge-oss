package biomass

import (
	"context"
	"encoding/json"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// ProjectionResult — 한 시점의 평균 체중 추정 결과.
type ProjectionResult struct {
	EstimatedAvgWeightG float64   `json:"estimated_avg_weight_g"`
	AnchorWeightG       float64   `json:"anchor_weight_g"` // W₀ 또는 W_sample
	AnchorSource        string    `json:"anchor_source"`   // "stocking" | "sampling"
	AnchorAt            time.Time `json:"anchor_at"`
	AnchorN             int       `json:"anchor_n"`          // N₀ 또는 N_at_sample
	CumulativeFeedG     float64   `json:"cumulative_feed_g"` // anchor 이후 누적 급이
	ExpectedFCR         float64   `json:"expected_fcr"`
	FCRKnown            bool      `json:"fcr_known"`  // 어종/단계 룩업 hit 여부
	FCRSource           string    `json:"fcr_source"` // "default" | "calibrated"
	DaysSinceAnchor     int       `json:"days_since_anchor"`
	Quality             string    `json:"quality"` // ok | stale_sampling | no_lifecycle | low_data
	Notes               []string  `json:"notes,omitempty"`
}

// ProjectionInputs — 외부에서 직접 채워서 ProjectFromInputs 에 전달 가능.
type ProjectionInputs struct {
	Species     string
	GrowthStage string
	Now         time.Time

	// anchor: 샘플링이 있으면 그게 우선. 없으면 stocking.
	AnchorWeightG float64
	AnchorN       int
	AnchorAt      time.Time
	AnchorSource  string

	// anchor 시점 이후 누적 급이 (g)
	CumulativeFeedG float64

	// 마지막 sampling 후 경과 일수 (없으면 -1)
	DaysSinceSampling int

	// Cage/Tank별 보정 FCR. nil 이면 LookupFCR(species, stage) 폴백.
	// D-4 calibrator 가 채움.
	OverrideFCR       *float64
	OverrideFCRSource string // "calibrated" | ""
}

// ProjectFromInputs — 순수 함수 (IO 없음). 테스트 용이.
func ProjectFromInputs(in ProjectionInputs) ProjectionResult {
	out := ProjectionResult{
		AnchorWeightG:   in.AnchorWeightG,
		AnchorSource:    in.AnchorSource,
		AnchorAt:        in.AnchorAt,
		AnchorN:         in.AnchorN,
		CumulativeFeedG: in.CumulativeFeedG,
		Notes:           []string{},
	}

	var fcr float64
	var known bool
	if in.OverrideFCR != nil && *in.OverrideFCR > 0 {
		// D-4 보정값 우선 사용 — 룩업 건너뜀
		fcr = *in.OverrideFCR
		known = true
		src := in.OverrideFCRSource
		if src == "" {
			src = "calibrated"
		}
		out.FCRSource = src
	} else {
		fcr, known = LookupFCR(in.Species, in.GrowthStage)
		out.FCRSource = "default"
		if !known {
			out.Notes = append(out.Notes, "어종/단계 FCR 룩업 미스 — default 1.5 사용")
		}
	}
	out.ExpectedFCR = fcr
	out.FCRKnown = known

	// AnchorN 이 0 이면 추정 불가
	if in.AnchorN <= 0 {
		out.EstimatedAvgWeightG = in.AnchorWeightG
		out.Quality = "low_data"
		out.Notes = append(out.Notes, "anchor 마릿수 0 — 입식 정보 필요")
		return out
	}

	// 비정상 FCR 방어 (테이블 데이터 무결성 체크)
	if fcr <= 0 {
		out.EstimatedAvgWeightG = in.AnchorWeightG
		out.Quality = "low_data"
		out.Notes = append(out.Notes, "FCR 값 비정상 (<=0) — 추정 불가")
		return out
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	daysSinceAnchor := int(now.Sub(in.AnchorAt).Hours() / 24)
	if daysSinceAnchor < 0 {
		daysSinceAnchor = 0
	}
	out.DaysSinceAnchor = daysSinceAnchor

	// ΔBiomassG = 누적급이 / FCR (anchor 이후 누적 증체량, g)
	deltaBiomassG := in.CumulativeFeedG / fcr
	// ΔW = ΔBiomassG / N
	deltaWeightG := deltaBiomassG / float64(in.AnchorN)

	est := in.AnchorWeightG + deltaWeightG
	// 음수 역행 방지 (비정상 입력 방어)
	if est < in.AnchorWeightG {
		est = in.AnchorWeightG
	}
	out.EstimatedAvgWeightG = est

	// Quality 결정
	switch {
	case daysSinceAnchor < 1:
		out.Quality = "low_data"
		out.Notes = append(out.Notes, "anchor 직후 — 추정 신뢰도 낮음")
	case in.DaysSinceSampling > 35:
		out.Quality = "stale_sampling"
	default:
		out.Quality = "ok"
	}

	// FCR 사용 명시
	if out.FCRSource == "calibrated" {
		out.Notes = append(out.Notes, "calibrated FCR 사용 ("+in.Species+"/"+in.GrowthStage+")")
	} else {
		out.Notes = append(out.Notes, "expected FCR 사용 ("+in.Species+"/"+in.GrowthStage+")")
	}

	return out
}

// LoadAndProject — store/lifecycle/sampling/feeding events 에서 입력 자동 수집.
// active lifecycle 없으면 ok=false + Quality="no_lifecycle".
func LoadAndProject(ctx context.Context, store storage.Store, tankID string) (ProjectionResult, bool, error) {
	lc, err := store.GetTankLifecycle(ctx, tankID)
	if err != nil {
		return ProjectionResult{Quality: "no_lifecycle"}, false, err
	}
	if lc == nil || lc.Status != "active" {
		return ProjectionResult{
			Quality: "no_lifecycle",
			Notes:   []string{"추정 미가능 (활성 lineage 없음)"},
		}, false, nil
	}

	in := ProjectionInputs{
		Species:     lc.Species,
		GrowthStage: lc.GrowthStage,
		Now:         time.Now().UTC(),
		// 기본 anchor: stocking
		AnchorWeightG:     lc.InitialAvgWeightG,
		AnchorN:           lc.InitialCount,
		AnchorAt:          lc.StockedAt,
		AnchorSource:      "stocking",
		DaysSinceSampling: -1,
	}

	// D-2 sampling 이 있으면 anchor 교체
	ts, err := store.GetTankSampling(ctx, tankID)
	if err == nil && ts != nil {
		in.AnchorSource = "sampling"
		in.AnchorWeightG = ts.AvgWeightG
		in.AnchorAt = ts.SampledAt
		// mortality 모델 없으므로 N₀ 고수
		in.AnchorN = lc.InitialCount
		days := int(time.Since(ts.SampledAt).Hours() / 24)
		if days < 0 {
			days = 0
		}
		in.DaysSinceSampling = days
	}

	// D-4: Cage/Tank별 보정 FCR 이 있으면 override (같은 stocking cycle 것만 유효)
	if cal, err := store.GetTankFCRCalibration(ctx, tankID); err == nil && cal != nil {
		if cal.StockingID == lc.ActiveStockingID {
			in.OverrideFCR = &cal.CalibratedFCR
			in.OverrideFCRSource = "calibrated"
		}
		// 다른 cycle 의 보정값은 무효 — 사용하지 않고 default 폴백
	}

	// anchor 이후 feeding events 누적 합산 (최대 5000건 안전 제한)
	feedEvts, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeedingRecorded,
		Since:     &in.AnchorAt,
		Limit:     5000,
	})
	if err == nil {
		for _, e := range feedEvts {
			var p events.FeedingRecordedPayload
			if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
				continue
			}
			if p.TankID != tankID {
				continue
			}
			// fed_at >= anchorAt 추가 검증 (QueryEvents Since 가 RecordedAt 기준이므로)
			fedAt, err := time.Parse(time.RFC3339Nano, p.FedAt)
			if err != nil {
				continue
			}
			if fedAt.Before(in.AnchorAt) {
				continue
			}
			in.CumulativeFeedG += p.FeedAmountG
		}
	}

	result := ProjectFromInputs(in)
	return result, true, nil
}
