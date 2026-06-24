package baseline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision"
)

// ConfidenceComponents — Cage/Tank에 대한 AI 의 이해도를 정량화한 결과 (docs/29 §2.4).
// 비-actuating: 운영자가 자율 운영 영역을 결정할 때 게이트로만 사용.
type ConfidenceComponents struct {
	ForecastAccuracy  float64  `json:"forecast_accuracy"`  // 0..1
	BaselineStability float64  `json:"baseline_stability"` // 0..1
	TrainingMaturity  float64  `json:"training_maturity"`  // 0..1
	Composite         float64  `json:"composite"`          // 0..1, 가중합
	AdaptationLevel   string   `json:"adaptation_level"`   // cold|observation|adapted|autonomous
	HasBaseline       bool     `json:"has_baseline"`
	HasForecast       bool     `json:"has_forecast"`
	SampleCount       int      `json:"sample_count"` // 계산에 사용된 페어/이벤트 수
	Notes             []string `json:"notes,omitempty"`
}

// ComputeTankConfidence reads recent events and computes the confidence score.
func ComputeTankConfidence(ctx context.Context, store storage.Store, tankID string) (ConfidenceComponents, error) {
	out := ConfidenceComponents{Notes: []string{}}

	// A. ForecastAccuracy (weight 0.4)
	forecastPairs, forecastAcc, forecastNotes, err := computeForecastAccuracy(ctx, store, tankID)
	if err != nil {
		return out, fmt.Errorf("forecast accuracy: %w", err)
	}
	out.ForecastAccuracy = forecastAcc
	out.Notes = append(out.Notes, forecastNotes...)

	// B. BaselineStability (weight 0.3)
	normalCount, stability, stabilityNotes, err := computeBaselineStability(ctx, store, tankID)
	if err != nil {
		return out, fmt.Errorf("baseline stability: %w", err)
	}
	out.BaselineStability = stability
	out.Notes = append(out.Notes, stabilityNotes...)

	// C. TrainingMaturity (weight 0.3)
	maturity, hasBaseline, hasForecast := computeTrainingMaturity(tankID)
	out.TrainingMaturity = maturity
	out.HasBaseline = hasBaseline
	out.HasForecast = hasForecast

	out.SampleCount = forecastPairs + normalCount

	// Composite 가중합
	composite := 0.4*out.ForecastAccuracy + 0.3*out.BaselineStability + 0.3*out.TrainingMaturity

	// Staleness 조정: 최근 24h 이내 평가 이벤트 없으면 0.5 배 패널티.
	if isStale(ctx, store, tankID) {
		composite *= 0.5
		out.Notes = append(out.Notes, "최근 24시간 내 평가 이벤트 없음 — 점수 staleness 적용")
	}

	out.Composite = clamp01(composite)
	out.AdaptationLevel = adaptationLevel(out.Composite)
	return out, nil
}

// computeForecastAccuracy — 최근 30건의 water.forecast.recorded 이벤트와
// 실측 sensor.reading.recorded 를 매핑해 MAE 기반 정확도 산출.
// 반환: (pair 수, 정확도 0..1, notes)
func computeForecastAccuracy(ctx context.Context, store storage.Store, tankID string) (int, float64, []string, error) {
	fes, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventWaterForecastRecorded,
		Limit:     30,
	})
	if err != nil {
		return 0, 0, nil, err
	}

	// 2026-05-20: forecast 자체가 없으면 (또는 이 tank 의 forecast 가 없으면) sensor
	// reading 가져올 필요 없음 — 어차피 페어 0건 → 0점 반환. SensorReadingRecorded 는
	// 297k+ 누적될 수 있어 Limit=500 fetch 가 무거움 (N+1 메모리 필터).
	hasForecastForTank := false
	for _, fe := range fes {
		var fp events.WaterForecastRecordedPayload
		if json.Unmarshal([]byte(fe.PayloadJSON), &fp) == nil && fp.TankID == tankID {
			hasForecastForTank = true
			break
		}
	}
	if !hasForecastForTank {
		return 0, 0, []string{"예측-실측 페어 부족 (0/5건)"}, nil
	}

	// 실측 readings 조회 (Limit 100 — 30분 forecast 의 ±5분 window 매칭에 충분).
	ses, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		Limit:     100,
	})
	if err != nil {
		return 0, 0, nil, err
	}

	var errors []float64
	for _, fe := range fes {
		var fp events.WaterForecastRecordedPayload
		if err := json.Unmarshal([]byte(fe.PayloadJSON), &fp); err != nil {
			continue
		}
		if fp.TankID != tankID {
			continue
		}
		// horizon 30분 인덱스 찾기
		h30idx := -1
		for i, h := range fp.HorizonMinutes {
			if h == 30 {
				h30idx = i
				break
			}
		}
		if h30idx < 0 || h30idx >= len(fp.PredictedValues) {
			continue
		}
		predicted := fp.PredictedValues[h30idx]

		evalAt, err := time.Parse(time.RFC3339Nano, fp.EvaluatedAt)
		if err != nil {
			continue
		}
		targetTime := evalAt.Add(30 * time.Minute)

		// ±5분 내 실측값 탐색
		actual, found := findSensorReading(ses, tankID, fp.Metric, targetTime, 5*time.Minute)
		if !found {
			continue
		}
		errors = append(errors, math.Abs(predicted-actual))
	}

	if len(errors) < 5 {
		note := fmt.Sprintf("예측-실측 페어 부족 (%d/5건)", len(errors))
		return len(errors), 0, []string{note}, nil
	}

	mae := mean(errors)
	// 1.0 mg/L 가 0점 임계
	acc := clamp01(1.0 - mae/1.0)
	return len(errors), acc, nil, nil
}

// findSensorReading — ses 중 tank_id=tankID, metric=metric, observed_at within ±window 인 첫 값 반환.
func findSensorReading(ses []*storage.Event, tankID, metric string, target time.Time, window time.Duration) (float64, bool) {
	for _, se := range ses {
		var sp events.SensorReadingPayload
		if err := json.Unmarshal([]byte(se.PayloadJSON), &sp); err != nil {
			continue
		}
		if sp.Metric != metric || sp.Location.TankID != tankID {
			continue
		}
		if sp.Value == nil {
			continue
		}
		obs, err := time.Parse(time.RFC3339Nano, sp.ObservedAt)
		if err != nil {
			continue
		}
		diff := obs.Sub(target)
		if diff < 0 {
			diff = -diff
		}
		if diff <= window {
			return *sp.Value, true
		}
	}
	return 0, false
}

// computeBaselineStability — 최근 100건 tank.baseline.scored 중 normal verdict 의
// anomaly_score CV (변동계수) 로 안정성 산출.
// 반환: (normal 수, 안정성 0..1, notes)
func computeBaselineStability(ctx context.Context, store storage.Store, tankID string) (int, float64, []string, error) {
	es, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventTankBaselineScored,
		Limit:     100,
	})
	if err != nil {
		return 0, 0, nil, err
	}

	var scores []float64
	for _, e := range es {
		var p events.TankBaselineScoredPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID || p.Verdict != "normal" {
			continue
		}
		scores = append(scores, p.AnomalyScore)
	}

	if len(scores) < 10 {
		note := fmt.Sprintf("정상 표본 부족 (%d/10건)", len(scores))
		return len(scores), 0, []string{note}, nil
	}

	m := mean(scores)
	sd := stddev(scores, m)
	cv := sd / math.Max(m, 1e-9)
	stability := clamp01(1.0 - cv)
	return len(scores), stability, nil, nil
}

// computeTrainingMaturity — manifest 에 등록된 모델 유무로 성숙도 산출.
// baseline 0.5, forecast 0.5.
func computeTrainingMaturity(tankID string) (float64, bool, bool) {
	var maturity float64
	base, _ := vision.ActiveTankBaseline(tankID)
	hasBaseline := base.ActiveWeightsPath != ""
	if hasBaseline {
		maturity += 0.5
	}
	fc, _ := vision.ActiveTankWaterForecast(tankID)
	hasForecast := fc.ActiveWeightsPath != ""
	if hasForecast {
		maturity += 0.5
	}
	return maturity, hasBaseline, hasForecast
}

// isStale — 최근 24h 이내 baseline 또는 forecast 이벤트가 없으면 true.
func isStale(ctx context.Context, store storage.Store, tankID string) bool {
	cutoff := time.Now().UTC().Add(-24 * time.Hour)

	for _, evType := range []string{events.EventTankBaselineScored, events.EventWaterForecastRecorded} {
		es, err := store.QueryEvents(ctx, storage.EventFilter{EventType: evType, Limit: 50})
		if err != nil {
			continue
		}
		for _, e := range es {
			// RecordedAt 은 events 테이블의 삽입 시각
			if e.RecordedAt.After(cutoff) {
				// tank_id 필터
				var tankID2 string
				switch evType {
				case events.EventTankBaselineScored:
					var p events.TankBaselineScoredPayload
					if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil {
						tankID2 = p.TankID
					}
				case events.EventWaterForecastRecorded:
					var p events.WaterForecastRecordedPayload
					if json.Unmarshal([]byte(e.PayloadJSON), &p) == nil {
						tankID2 = p.TankID
					}
				}
				if tankID2 == tankID {
					return false
				}
			}
		}
	}
	return true
}

// AdaptationLevelFromComposite — Composite 점수를 레벨 문자열로 매핑 (docs/29 §3.4).
// 테스트 및 외부 패키지에서 밴드 검증용으로 사용.
func AdaptationLevelFromComposite(composite float64) string {
	switch {
	case composite >= 0.85:
		return "autonomous"
	case composite >= 0.60:
		return "adapted"
	case composite >= 0.30:
		return "observation"
	default:
		return "cold"
	}
}

func adaptationLevel(composite float64) string {
	return AdaptationLevelFromComposite(composite)
}

func clamp01(x float64) float64 {
	return math.Max(0, math.Min(1, x))
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stddev(xs []float64, m float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var s float64
	for _, x := range xs {
		d := x - m
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)))
}
