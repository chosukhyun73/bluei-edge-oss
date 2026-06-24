package baseline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 체중 임계: 이 값을 순서대로 통과하면 새 성장 단계로 판단.
var weightThresholds = []float64{100, 300, 1000}

// TransitionResult — 전환 감지 결과.
type TransitionResult struct {
	Detected   bool
	Reason     string // weight_threshold_passed | anomaly_drift_detected | ""
	DetectedAt time.Time
	Evidence   map[string]any
	Notes      []string
}

// DetectGrowthTransition reads recent events + tank profile and returns whether
// a distribution shift has occurred for this tank.
//
// Signal B (weight) 가 우선. Signal A (anomaly drift) 는 B 미감지 시 평가.
// 두 신호 모두 미감지이면 Detected=false 와 Notes 반환.
func DetectGrowthTransition(ctx context.Context, store storage.Store, tankID string) (TransitionResult, error) {
	// Signal B: 체중 임계 통과
	res, err := detectWeightThreshold(ctx, store, tankID)
	if err != nil {
		return TransitionResult{}, fmt.Errorf("weight threshold check: %w", err)
	}
	if res.Detected {
		return res, nil
	}

	// Signal A: anomaly drift
	driftRes, err := detectAnomalyDrift(ctx, store, tankID)
	if err != nil {
		return TransitionResult{}, fmt.Errorf("anomaly drift check: %w", err)
	}
	if driftRes.Detected {
		return driftRes, nil
	}

	// 두 신호 모두 미감지 — notes 병합
	notes := append(res.Notes, driftRes.Notes...)
	if len(notes) == 0 {
		notes = []string{"최근 14일 정상 비율 안정적, 체중 임계 미통과"}
	}
	return TransitionResult{Notes: notes}, nil
}

// detectWeightThreshold — Signal B: profile.AvgWeightG 가 새 임계를 처음 통과했는지 확인.
func detectWeightThreshold(ctx context.Context, store storage.Store, tankID string) (TransitionResult, error) {
	profile, err := store.GetTankProfile(ctx, tankID)
	if err != nil {
		return TransitionResult{Notes: []string{"tank profile 조회 실패: " + err.Error()}}, nil
	}
	if profile == nil || profile.AvgWeightG <= 0 {
		return TransitionResult{Notes: []string{"체중 정보 없음 — weight signal 건너뜀"}}, nil
	}
	currentWeight := profile.AvgWeightG

	// 직전 체중 기반 전환 이벤트에서 weight_at_detection 추출
	priorWeight, err := lastWeightAtTransition(ctx, store, tankID)
	if err != nil {
		return TransitionResult{}, err
	}

	// currentWeight 이하이면서 priorWeight 초과인 가장 큰 임계
	var crossed float64
	for _, t := range weightThresholds {
		if t <= currentWeight && t > priorWeight {
			crossed = t
		}
	}
	if crossed == 0 {
		return TransitionResult{Notes: []string{fmt.Sprintf("체중 %.0fg — 아직 미통과 임계 없음 (직전 %.0fg)", currentWeight, priorWeight)}}, nil
	}

	return TransitionResult{
		Detected:   true,
		Reason:     "weight_threshold_passed",
		DetectedAt: time.Now().UTC(),
		Evidence: map[string]any{
			"current_weight":      currentWeight,
			"prior_weight":        priorWeight,
			"threshold_passed":    crossed,
			"weight_at_detection": currentWeight,
		},
	}, nil
}

// lastWeightAtTransition — 가장 최근 weight_threshold_passed 전환 이벤트의 weight_at_detection.
// 이벤트 없으면 0 반환.
func lastWeightAtTransition(ctx context.Context, store storage.Store, tankID string) (float64, error) {
	es, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankTransitionDetected,
		Limit:     50,
	})
	if err != nil {
		return 0, fmt.Errorf("query transition events: %w", err)
	}
	for _, e := range es {
		var p events.TankTransitionDetectedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID || p.Reason != "weight_threshold_passed" {
			continue
		}
		if p.Evidence != nil {
			if v, ok := p.Evidence["weight_at_detection"]; ok {
				switch w := v.(type) {
				case float64:
					return w, nil
				case json.Number:
					f, _ := w.Float64()
					return f, nil
				}
			}
		}
		return 0, nil // evidence 없으면 0
	}
	return 0, nil
}

// driftRow is an internal helper for anomaly drift computation.
type driftRow struct {
	verdict string
	score   float64
}

// detectAnomalyDrift — Signal A: 최근/과거 7일 정상 비율 + 평균 score 이동 감지.
func detectAnomalyDrift(ctx context.Context, store storage.Store, tankID string) (TransitionResult, error) {
	es, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankBaselineScored,
		Limit:     200,
	})
	if err != nil {
		return TransitionResult{}, fmt.Errorf("query baseline events: %w", err)
	}

	now := time.Now().UTC()
	cutoff14 := now.Add(-14 * 24 * time.Hour)
	cut7 := now.Add(-7 * 24 * time.Hour)

	var recent, older []driftRow // recent=지난7일, older=8~14일전

	for _, e := range es {
		var p events.TankBaselineScoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID {
			continue
		}
		t := e.RecordedAt
		if t.Before(cutoff14) {
			continue
		}
		if t.After(cut7) {
			recent = append(recent, driftRow{p.Verdict, p.AnomalyScore})
		} else {
			older = append(older, driftRow{p.Verdict, p.AnomalyScore})
		}
	}

	total := len(recent) + len(older)
	if total < 30 {
		return TransitionResult{
			Notes: []string{fmt.Sprintf("데이터 부족 — 14일 / 30 events 필요 (현재 %d건)", total)},
		}, nil
	}
	if len(recent) < 10 || len(older) < 10 {
		return TransitionResult{
			Notes: []string{fmt.Sprintf("분할 샘플 부족 — recent=%d older=%d (각 10건 필요)", len(recent), len(older))},
		}, nil
	}

	recentNormalRate := driftNormalRate(recent)
	olderNormalRate := driftNormalRate(older)
	recentMean := mean(driftScores(recent))
	olderMean := mean(driftScores(older))

	rateDrop := olderNormalRate - recentNormalRate
	scoreMult := recentMean / max64(olderMean, 1e-9)

	if rateDrop > 0.20 && scoreMult > 1.5 {
		return TransitionResult{
			Detected:   true,
			Reason:     "anomaly_drift_detected",
			DetectedAt: time.Now().UTC(),
			Evidence: map[string]any{
				"recent_normal_rate": recentNormalRate,
				"older_normal_rate":  olderNormalRate,
				"recent_mean_score":  recentMean,
				"older_mean_score":   olderMean,
				"recent_count":       len(recent),
				"older_count":        len(older),
			},
		}, nil
	}

	return TransitionResult{
		Notes: []string{fmt.Sprintf(
			"anomaly drift 기준 미충족 — 정상비율 %.0f%%→%.0f%% (차이 %.1f%%p), 점수 배율 %.2f",
			olderNormalRate*100, recentNormalRate*100, rateDrop*100, scoreMult,
		)},
	}, nil
}

func driftNormalRate(rows []driftRow) float64 {
	if len(rows) == 0 {
		return 0
	}
	n := 0
	for _, r := range rows {
		if r.verdict == "normal" {
			n++
		}
	}
	return float64(n) / float64(len(rows))
}

func driftScores(rows []driftRow) []float64 {
	out := make([]float64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.score)
	}
	return out
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
