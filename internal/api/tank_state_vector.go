package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"bluei.kr/edge/internal/baseline"
	"bluei.kr/edge/internal/biomass"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision"
)

// TankStateVector is the canonical "complete state of one tank" representation.
// 핵심 원칙 (docs/29 §2.1):
//   - 한 호출로 한 Cage/Tank의 모든 신호를 통합 반환한다.
//   - 미구현 영역(Phase 1~3 학습/예측/Confidence)은 nil + Notes 로 정직하게 표시.
//   - AI 자율 운영의 모든 결정은 이 vector 를 입력으로 한다.
type TankStateVector struct {
	TankID    string `json:"tank_id"`
	GroupID   string `json:"group_id,omitempty"` // Phase 1 Group domain. 미설정 시 빈 문자열.
	Timestamp string `json:"timestamp"`

	Fish              FishState         `json:"fish"`
	Water             WaterState        `json:"water"`
	Equipment         EquipmentState    `json:"equipment"`
	Feeding           FeedingState      `json:"feeding"`
	BiologicalContext BiologicalContext `json:"biological_context"`
	Confidence        ConfidenceState   `json:"confidence"`
	Anomaly           AnomalyState      `json:"anomaly"`
	Adaptation        AdaptationState   `json:"adaptation"`
	Autonomous        AutonomousState   `json:"autonomous"`
	Decisions         DecisionState     `json:"decisions"`
}

// DecisionState — Phase 4 C-2 AI 결정 라우팅 현재 상태.
// 실제 제어 명령 없음 — 순수 audit + 운영자 UI 지원.
type DecisionState struct {
	LastRoutedAt     string           `json:"last_routed_at,omitempty"`
	LastRoute        string           `json:"last_route,omitempty"`
	LastDecisionKind string           `json:"last_decision_kind,omitempty"`
	LastReasoning    string           `json:"last_reasoning,omitempty"`
	PendingCount     int              `json:"pending_count"`
	PendingItems     []map[string]any `json:"pending_items,omitempty"` // 최대 5건 미리보기
	// C-4: 자동 실행 정책
	AutoExecuteEnabled bool     `json:"auto_execute_enabled"`
	GraceMinutes       int      `json:"grace_minutes"`
	PolicySource       string   `json:"policy_source,omitempty"` // tank_override | system_default
	Notes              []string `json:"notes,omitempty"`
}

// AutonomousState — Phase 4 C-1/C-3 자율 운영 모드 현재 상태.
// 실제 제어 연결은 C-2/C-3. 여기서는 상태 + audit 표시 전용.
type AutonomousState struct {
	Mode              string   `json:"mode"` // off | observation | partial | full
	Reason            string   `json:"reason,omitempty"`
	ChangedAt         string   `json:"changed_at,omitempty"`
	ChangedBy         string   `json:"changed_by,omitempty"`
	AutoDowngraded    bool     `json:"auto_downgraded,omitempty"`     // 마지막 변경이 auto-downgrade 였는지
	LastBlockedAt     string   `json:"last_blocked_at,omitempty"`     // 가장 최근 차단된 자율 시도 시각
	LastBlockedReason string   `json:"last_blocked_reason,omitempty"` // 차단 사유
	Notes             []string `json:"notes,omitempty"`
}

// AdaptationState — Phase 3.5 전환 감지 상태.
// active=true 면 운영자에게 baseline 재학습 권장.
type AdaptationState struct {
	TransitionDetected bool           `json:"transition_detected"`
	Reason             string         `json:"reason,omitempty"`
	DetectedAt         string         `json:"detected_at,omitempty"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	Notes              []string       `json:"notes,omitempty"`
}

// AnomalyState — Cage/Tank baseline (autoencoder) 의 가장 최근 평가 결과.
// HasModel=false 면 학습된 baseline 이 없는 상태 (모델 학습 필요).
// LatestVerdict: normal | warning | anomaly | "" (평가 이력 없음).
type AnomalyState struct {
	HasModel      bool               `json:"has_model"`
	LatestScore   *float64           `json:"latest_score,omitempty"`
	LatestVerdict string             `json:"latest_verdict,omitempty"`
	P95Threshold  *float64           `json:"p95_threshold,omitempty"`
	P99Threshold  *float64           `json:"p99_threshold,omitempty"`
	FeatureDiff   map[string]float64 `json:"feature_diff,omitempty"`
	EvaluatedAt   string             `json:"evaluated_at,omitempty"`
	ModelDir      string             `json:"model_dir,omitempty"`
	ActiveJobID   string             `json:"active_job_id,omitempty"`
	Notes         []string           `json:"notes,omitempty"`
}

// FishState — 카메라/비전 도메인 출력. nil 은 "아직 측정/학습 안 됨" 의미.
type FishState struct {
	EstimatedCount      *int     `json:"estimated_count,omitempty"`
	AvgWeightG          *float64 `json:"avg_weight_g,omitempty"`
	ActivityScore       *float64 `json:"activity_score,omitempty"`
	SurfaceClusterRatio *float64 `json:"surface_cluster_ratio,omitempty"`
	DepthDispersion     *float64 `json:"depth_dispersion,omitempty"`
	LastObservedAt      string   `json:"last_observed_at,omitempty"`
	Quality             string   `json:"quality,omitempty"`
	Notes               []string `json:"notes,omitempty"`
}

// WaterState — 수질 센서 + 향후 예측 (Phase 2).
type WaterState struct {
	Metrics        map[string]*WaterMetric `json:"metrics"`
	LastObservedAt string                  `json:"last_observed_at,omitempty"`
	Predictions    WaterPredictions        `json:"predictions"`
	Notes          []string                `json:"notes,omitempty"`
}

type WaterMetric struct {
	Value   *float64 `json:"value"`
	Unit    string   `json:"unit"`
	Quality string   `json:"quality"`
}

// WaterPredictions — water-forecast 모델의 가장 최근 예측 (Phase 2).
// Available=false 면 모델 미배포 또는 예측 이벤트 없음.
type WaterPredictions struct {
	Available       bool      `json:"available"`
	TargetMetric    string    `json:"target_metric,omitempty"`
	CurrentValue    *float64  `json:"current_value,omitempty"`
	HorizonMinutes  []int     `json:"horizon_minutes,omitempty"`
	PredictedValues []float64 `json:"predicted_values,omitempty"`
	EvaluatedAt     string    `json:"evaluated_at,omitempty"`
	ModelDir        string    `json:"model_dir,omitempty"`
	ActiveJobID     string    `json:"active_job_id,omitempty"`
}

// EquipmentState — 장비 헬스 통합.
type EquipmentState struct {
	Devices       []DeviceSummary `json:"devices"`
	Count         int             `json:"count"`
	HealthSummary string          `json:"health_summary"` // all_healthy | degraded | down | unknown
	Notes         []string        `json:"notes,omitempty"`
}

type DeviceSummary struct {
	DeviceID   string `json:"device_id"`
	DeviceType string `json:"device_type"`
	Status     string `json:"status"`
	Quality    string `json:"quality"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

// FeedingState — 오늘 / 최근 / 추정 FCR.
type FeedingState struct {
	TodayTotalG   float64  `json:"today_total_g"`
	LastFeedingAt string   `json:"last_feeding_at,omitempty"`
	LastFeedingG  *float64 `json:"last_feeding_g,omitempty"`
	EstimatedFCR  *float64 `json:"estimated_fcr,omitempty"` // Phase 5 (결과 피드백) 까지 nil
	Notes         []string `json:"notes,omitempty"`
}

// BiologicalContext — 어종/마릿수/체중. 활성 lifecycle 이 있으면 lifecycle 우선,
// 없으면 TankProfile fallback.
type BiologicalContext struct {
	Species              string   `json:"species,omitempty"`
	GrowthStage          string   `json:"growth_stage,omitempty"`
	FishCount            int      `json:"fish_count,omitempty"`
	AvgWeightG           float64  `json:"avg_weight_g,omitempty"`
	BiomassKg            float64  `json:"biomass_kg,omitempty"`
	SystemType           string   `json:"system_type,omitempty"`
	Source               string   `json:"source"` // lifecycle | tank_profile
	StockingID           string   `json:"stocking_id,omitempty"`
	StockedAt            string   `json:"stocked_at,omitempty"`
	DaysSinceStocking    int      `json:"days_since_stocking,omitempty"`
	TargetHarvestWeightG *float64 `json:"target_harvest_weight_g,omitempty"`
	TargetHarvestDate    string   `json:"target_harvest_date,omitempty"`
	// D-2: 마지막 sampling anchor
	LastSampledAt        string   `json:"last_sampled_at,omitempty"`
	LastSampledAvgWeight float64  `json:"last_sampled_avg_weight_g,omitempty"`
	LastSampledHealth    *int     `json:"last_sampled_health_score,omitempty"`
	LastSampledStdWeight *float64 `json:"last_sampled_std_weight_g,omitempty"`
	DaysSinceSampling    int      `json:"days_since_sampling,omitempty"`
	// D-3: FCR 기반 평균 체중 추정
	EstimatedAvgWeightG float64  `json:"estimated_avg_weight_g,omitempty"`
	EstimationQuality   string   `json:"estimation_quality,omitempty"`
	EstimationAnchor    string   `json:"estimation_anchor,omitempty"` // stocking | sampling
	EstimationFCR       float64  `json:"estimation_fcr,omitempty"`
	EstimationFCRSource string   `json:"estimation_fcr_source,omitempty"` // default | calibrated
	Notes               []string `json:"notes,omitempty"`
}

// ConfidenceState — AI 가 이 Cage/Tank를 얼마나 잘 이해하는지.
// Phase 3 부터 ComputeTankConfidence 로 실제 점수 산정.
type ConfidenceState struct {
	AdaptationLevel     string                         `json:"adaptation_level"` // cold | observation | adapted | autonomous
	TankConfidenceScore *float64                       `json:"tank_confidence_score,omitempty"`
	Components          *baseline.ConfidenceComponents `json:"components,omitempty"` // 운영자가 sub-score 확인 가능
	HasActiveWeights    bool                           `json:"has_active_weights"`
	ActiveAlgorithmID   string                         `json:"active_algorithm_id,omitempty"`
	ActiveJobID         string                         `json:"active_job_id,omitempty"`
	AppliedAt           string                         `json:"applied_at,omitempty"`
	Notes               []string                       `json:"notes,omitempty"`
}

// ── Handler ─────────────────────────────────────────────────────────────

func tankStateVectorIDFromPath(path string) string {
	if !strings.HasPrefix(path, "/v1/tanks/") || !strings.HasSuffix(path, "/state-vector") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/tanks/"), "/state-vector")
	return strings.Trim(id, "/")
}

func (s *Server) handleTankStateVector(w http.ResponseWriter, r *http.Request) {
	tankID := tankStateVectorIDFromPath(r.URL.Path)
	if tankID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"expected /v1/tanks/{tank_id}/state-vector", "")
		return
	}
	v, err := s.buildTankStateVector(r, tankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) buildTankStateVector(r *http.Request, tankID string) (*TankStateVector, error) {
	// 핵심 개선 (2026-05-20): handler 차원 8초 hard cap.
	// 이전: http.Server WriteTimeout 만료 시 connection 만 끊기고 10 sub-goroutine 은
	// 계속 SQLite reader pool 점유 → 좀비 누적 → 점진적 hang.
	// 이후: sub-ctx 가 cancel 되면 QueryContext 가 즉시 unwind → reader 즉시 반환.
	subCtx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	rSub := r.WithContext(subCtx)

	v := &TankStateVector{
		TankID:    tankID,
		Timestamp: common.FormatTime(common.NowUTC()),
	}
	if profile, err := s.store.GetTankProfile(subCtx, tankID); err == nil && profile != nil {
		v.GroupID = profile.GroupID
	}

	// 10 collect 함수 병렬화. sub-ctx 전파로 timeout 시 일괄 unwind.
	// 각 collect 는 v 의 서로 다른 필드에만 write → race 없음.
	type stageResult struct {
		name string
		dur  time.Duration
	}
	results := make(chan stageResult, 10)
	runStage := func(name string, fn func()) {
		t0 := time.Now()
		fn()
		results <- stageResult{name: name, dur: time.Since(t0)}
	}

	overallStart := time.Now()
	var wg sync.WaitGroup
	wg.Add(10)
	go func() { defer wg.Done(); runStage("fish", func() { v.Fish = s.collectFishState(rSub, tankID) }) }()
	go func() { defer wg.Done(); runStage("water", func() { v.Water = s.collectWaterState(rSub, tankID) }) }()
	go func() {
		defer wg.Done()
		runStage("equipment", func() { v.Equipment = s.collectEquipmentState(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("feeding", func() { v.Feeding = s.collectFeedingState(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("bio", func() { v.BiologicalContext = s.collectBiologicalContext(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("confidence", func() { v.Confidence = s.collectConfidenceState(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("anomaly", func() { v.Anomaly = s.collectAnomalyState(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("adaptation", func() { v.Adaptation = s.collectAdaptationState(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("autonomous", func() { v.Autonomous = s.collectAutonomousState(rSub, tankID) })
	}()
	go func() {
		defer wg.Done()
		runStage("decisions", func() { v.Decisions = s.collectDecisionState(rSub, tankID) })
	}()
	wg.Wait()
	close(results)

	// 진단 로깅 — 느린 collect 식별. 1초 초과 collect 가 있으면 WARN.
	timings := make(map[string]int64, 10)
	var slowest string
	var slowestMs int64
	for r := range results {
		ms := r.dur.Milliseconds()
		timings[r.name] = ms
		if ms > slowestMs {
			slowestMs = ms
			slowest = r.name
		}
	}
	totalMs := time.Since(overallStart).Milliseconds()
	level := slog.LevelDebug
	if totalMs > 1000 || subCtx.Err() != nil {
		level = slog.LevelWarn
	}
	slog.Log(subCtx, level, "state_vector built",
		"tank_id", tankID,
		"total_ms", totalMs,
		"slowest", slowest,
		"slowest_ms", slowestMs,
		"timings_ms", timings,
		"ctx_err", subCtx.Err(),
	)

	return v, nil
}

// ── Adaptation — Phase 3.5 전환 감지 상태 ──

// collectAdaptationState — 최근 30일 이내 tank.transition.detected 이벤트를 읽어 상태 반환.
// 감지 연산은 worker 의 몫 — 여기서는 이벤트 조회만 수행.
func (s *Server) collectAdaptationState(r *http.Request, tankID string) AdaptationState {
	out := AdaptationState{Notes: []string{}}
	es, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventTankTransitionDetected,
		Limit:     50,
	})
	if err != nil {
		out.Notes = append(out.Notes, "전환 이벤트 조회 실패: "+err.Error())
		return out
	}

	cutoff30 := time.Now().UTC().Add(-30 * 24 * time.Hour)
	for _, e := range es {
		var p events.TankTransitionDetectedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID {
			continue
		}
		// 가장 최근 (QueryEvents 는 최신 우선 정렬)
		if e.RecordedAt.Before(cutoff30) {
			out.Notes = append(out.Notes,
				"최근 전환 없음 (마지막: "+e.RecordedAt.Format("2006-01-02")+")")
			return out
		}
		out.TransitionDetected = true
		out.Reason = p.Reason
		out.DetectedAt = p.DetectedAt
		out.Evidence = p.Evidence
		return out
	}
	out.Notes = append(out.Notes, "전환 이력 없음")
	return out
}

// ── Anomaly — 활성 baseline 모델 + 가장 최근 score 이벤트 통합 ──

func (s *Server) collectAnomalyState(r *http.Request, tankID string) AnomalyState {
	out := AnomalyState{Notes: []string{}}
	active, err := vision.ActiveTankBaseline(tankID)
	if err == nil && active.ActiveWeightsPath != "" {
		out.HasModel = true
		out.ModelDir = active.ActiveWeightsPath
		out.ActiveJobID = active.ActiveJobID
	} else {
		out.Notes = append(out.Notes,
			"이 Cage/Tank에 학습된 baseline 모델이 없습니다. "+
				"7일 이상 운영 데이터를 모은 뒤 [AI 가르치기 → tank-baseline] 학습이 필요합니다.")
	}
	if last, ok := s.latestBaselineScore(r.Context(), tankID); ok {
		score := last.AnomalyScore
		p95 := last.P95Threshold
		p99 := last.P99Threshold
		out.LatestScore = &score
		out.LatestVerdict = last.Verdict
		out.P95Threshold = &p95
		out.P99Threshold = &p99
		out.FeatureDiff = last.FeatureDiff
		out.EvaluatedAt = last.EvaluatedAt
	} else if out.HasModel {
		out.Notes = append(out.Notes,
			"baseline 모델은 있지만 아직 평가 이벤트가 없습니다. "+
				"POST /v1/tanks/{id}/baseline/score 로 1회 산출 가능합니다.")
	}
	return out
}

// ── Fish — 최근 vision.observation.recorded 에서 활동성/표면군집/분산 추출 ──

func (s *Server) collectFishState(r *http.Request, tankID string) FishState {
	out := FishState{Notes: []string{}}
	es, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionObservationRecorded,
		Limit:     200,
	})
	if err != nil {
		out.Notes = append(out.Notes, "vision observation 조회 실패: "+err.Error())
		return out
	}
	for _, e := range es {
		var p events.VisionObservationPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID != tankID {
			continue
		}
		if v, ok := p.Scores["activity_score"]; ok {
			vc := v
			out.ActivityScore = &vc
		}
		if v, ok := p.Scores["surface_cluster_ratio"]; ok {
			vc := v
			out.SurfaceClusterRatio = &vc
		}
		if v, ok := p.Scores["depth_dispersion"]; ok {
			vc := v
			out.DepthDispersion = &vc
		}
		out.LastObservedAt = p.ObservedAt
		out.Quality = p.Quality
		break // 가장 최근 한 건만
	}
	if out.ActivityScore == nil {
		out.Notes = append(out.Notes,
			"이 Cage/Tank의 vision 관찰 기록이 없습니다 (카메라 미연결 또는 vision 서비스 미배포).")
	}
	if out.EstimatedCount == nil {
		out.Notes = append(out.Notes,
			"개체수 추정은 Phase 1 Cage/Tank baseline 학습 + biomass 추정 모델 후 산출됩니다.")
	}
	return out
}

// ── Water — CurrentTankEnvironmentReading projection 에서 ──

func (s *Server) collectWaterState(r *http.Request, tankID string) WaterState {
	out := WaterState{
		Metrics: map[string]*WaterMetric{},
		Predictions: WaterPredictions{
			Available: false,
		},
		Notes: []string{},
	}
	readings, err := s.store.ListTankEnvironment(r.Context(), tankID)
	if err != nil {
		out.Notes = append(out.Notes, "수질 readings 조회 실패: "+err.Error())
		return out
	}
	latest := ""
	for _, rd := range readings {
		var val *float64
		if rd.Value != nil {
			vc := *rd.Value
			val = &vc
		}
		out.Metrics[rd.Metric] = &WaterMetric{
			Value:   val,
			Unit:    rd.Unit,
			Quality: rd.Quality,
		}
		if rd.ObservedAt > latest {
			latest = rd.ObservedAt
		}
	}
	out.LastObservedAt = latest
	if len(out.Metrics) == 0 {
		out.Notes = append(out.Notes,
			"이 Cage/Tank의 수질 센서 readings 가 없습니다 (LattePanda 게이트웨이 미연결 또는 센서 미설치).")
	}
	// 가장 최근 water.forecast.recorded 이벤트 + 활성 모델 manifest 통합
	active, _ := vision.ActiveTankWaterForecast(tankID)
	if active.ActiveWeightsPath != "" {
		out.Predictions.ModelDir = active.ActiveWeightsPath
		out.Predictions.ActiveJobID = active.ActiveJobID
	}
	if last, ok := s.latestForecastEvent(r.Context(), tankID); ok {
		out.Predictions.Available = true
		out.Predictions.TargetMetric = last.Metric
		out.Predictions.CurrentValue = last.CurrentValue
		out.Predictions.HorizonMinutes = last.HorizonMinutes
		out.Predictions.PredictedValues = last.PredictedValues
		out.Predictions.EvaluatedAt = last.EvaluatedAt
	} else if active.ActiveWeightsPath == "" {
		out.Notes = append(out.Notes,
			"수질 단기 예측 모델이 없습니다. "+
				"7일 이상 데이터 후 [AI 가르치기 → water-forecast] 학습으로 활성화됩니다.")
	} else {
		out.Notes = append(out.Notes,
			"forecast 모델은 있지만 아직 예측 이벤트가 없습니다. baseline_worker 가 켜져 있으면 곧 채워집니다.")
	}
	return out
}

// latestForecastEvent — events 에서 가장 최근 forecast 한 건.
func (s *Server) latestForecastEvent(ctx context.Context, tankID string) (events.WaterForecastRecordedPayload, bool) {
	es, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventWaterForecastRecorded,
		Limit:     200,
	})
	if err != nil {
		return events.WaterForecastRecordedPayload{}, false
	}
	for _, e := range es {
		var p events.WaterForecastRecordedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID {
			return p, true
		}
	}
	return events.WaterForecastRecordedPayload{}, false
}

// ── Equipment — 기존 latestDeviceHealthForTank 재사용 ──

func (s *Server) collectEquipmentState(r *http.Request, tankID string) EquipmentState {
	out := EquipmentState{
		Devices:       []DeviceSummary{},
		HealthSummary: "unknown",
		Notes:         []string{},
	}
	devices, err := s.latestDeviceHealthForTank(r, tankID)
	if err != nil {
		out.Notes = append(out.Notes, "device health 조회 실패: "+err.Error())
		return out
	}
	healthy, degraded, down := 0, 0, 0
	for _, d := range devices {
		summary := DeviceSummary{
			DeviceID:   stringValue(d["device_id"]),
			DeviceType: stringValue(d["device_type"]),
			Status:     stringValue(d["status"]),
			Quality:    stringValue(d["quality"]),
			LastSeenAt: stringValue(d["last_seen_at"]),
		}
		out.Devices = append(out.Devices, summary)
		switch summary.Status {
		case "online":
			healthy++
		case "degraded":
			degraded++
		case "down", "disabled":
			down++
		}
	}
	out.Count = len(out.Devices)
	switch {
	case down > 0:
		out.HealthSummary = "down"
	case degraded > 0:
		out.HealthSummary = "degraded"
	case healthy > 0:
		out.HealthSummary = "all_healthy"
	default:
		out.HealthSummary = "unknown"
		out.Notes = append(out.Notes,
			"이 Cage/Tank에 등록된 장비 헬스 이벤트가 없습니다.")
	}
	return out
}

// ── Feeding — 오늘 누적 + 최근 한 건 ──

func (s *Server) collectFeedingState(r *http.Request, tankID string) FeedingState {
	out := FeedingState{Notes: []string{}}
	loc, err := time.LoadLocation(s.cfg.Site.Timezone)
	if err != nil {
		loc = time.Local
	}
	now := common.NowUTC().In(loc)
	startLocal := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	items, err := s.listFeedingRecords(r, 1000, startLocal.UTC())
	if err != nil {
		out.Notes = append(out.Notes, "feeding 조회 실패: "+err.Error())
		return out
	}
	for _, item := range items {
		payload, ok := item["payload"].(events.FeedingRecordedPayload)
		if !ok || payload.TankID != tankID {
			continue
		}
		out.TodayTotalG += payload.FeedAmountG
		if out.LastFeedingAt == "" {
			// listFeedingRecords 는 최근 → 과거 정렬이므로 첫 매칭이 가장 최근
			out.LastFeedingAt = stringValue(item["recorded_at"])
			amt := payload.FeedAmountG
			out.LastFeedingG = &amt
		}
	}
	out.Notes = append(out.Notes,
		"FCR 추정은 결과 피드백 학습 (Phase 5) 후 산출됩니다. 현재는 누적 급이량만 노출.")
	return out
}

// ── BiologicalContext — 활성 lifecycle 우선, 없으면 TankProfile fallback ──

func (s *Server) collectBiologicalContext(r *http.Request, tankID string) BiologicalContext {
	out := BiologicalContext{Notes: []string{}}

	// D-1: 활성 lifecycle 이 있으면 그 데이터를 anchor 로 사용
	lc, err := s.store.GetTankLifecycle(r.Context(), tankID)
	if err == nil && lc != nil && lc.Status == "active" {
		out.Source = "lifecycle"
		out.Species = lc.Species
		out.GrowthStage = lc.GrowthStage
		out.FishCount = lc.InitialCount
		out.AvgWeightG = lc.InitialAvgWeightG
		out.BiomassKg = float64(lc.InitialCount) * lc.InitialAvgWeightG / 1000.0
		out.StockingID = lc.ActiveStockingID
		out.StockedAt = fmtAPITime(lc.StockedAt)
		days := int(time.Since(lc.StockedAt).Hours() / 24)
		if days < 0 {
			days = 0
		}
		out.DaysSinceStocking = days
		out.TargetHarvestWeightG = lc.TargetHarvestWeightG
		out.TargetHarvestDate = lc.TargetHarvestDate
		out.Notes = append(out.Notes,
			"활성 lineage — stocking_id="+lc.ActiveStockingID)
		s.populateSamplingContext(r.Context(), tankID, &out)
		s.populateWeightProjection(r.Context(), tankID, &out)
		return out
	}

	// 출하 후 다음 입식 대기 상태
	if err == nil && lc != nil && lc.Status == "harvested" {
		out.Notes = append(out.Notes, "마지막 출하 후 다음 입식 대기")
	}

	// fallback: TankProfile
	out.Source = "tank_profile"
	profile, err := s.store.GetTankProfile(r.Context(), tankID)
	if err != nil || profile == nil {
		out.Notes = append(out.Notes,
			"TankProfile 조회 실패 또는 미등록. 입식 메타데이터가 누락되어 있습니다.")
		s.populateSamplingContext(r.Context(), tankID, &out)
		s.populateWeightProjection(r.Context(), tankID, &out)
		return out
	}
	out.Species = profile.Species
	out.FishCount = profile.FishCount
	out.AvgWeightG = profile.AvgWeightG
	out.BiomassKg = profile.BiomassKg
	out.SystemType = profile.SystemType
	out.Notes = append(out.Notes,
		"활성 lifecycle 없음 — TankProfile 기반. POST /stocking 으로 입식하면 lifecycle 기반으로 전환됩니다.")
	s.populateSamplingContext(r.Context(), tankID, &out)
	s.populateWeightProjection(r.Context(), tankID, &out)
	return out
}

// populateSamplingContext — D-2 sampling 최신 데이터를 BiologicalContext 에 채운다.
// sampling 없으면 권장 note 추가.
func (s *Server) populateSamplingContext(ctx context.Context, tankID string, out *BiologicalContext) {
	ts, err := s.store.GetTankSampling(ctx, tankID)
	if err != nil || ts == nil {
		out.Notes = append(out.Notes, "샘플링 이력 없음 — 입식 후 30일 전후 첫 sampling 권장")
		return
	}
	out.LastSampledAt = fmtAPITime(ts.SampledAt)
	out.LastSampledAvgWeight = ts.AvgWeightG
	out.LastSampledHealth = ts.HealthScore
	out.LastSampledStdWeight = ts.StdWeightG
	days := int(time.Since(ts.SampledAt).Hours() / 24)
	if days < 0 {
		days = 0
	}
	out.DaysSinceSampling = days
	// 표준 30일 + 마진 5일 = 35일 초과 시 권장 알림
	if days > 35 {
		out.Notes = append(out.Notes, "샘플링 35일 경과 — 다음 sampling 권장")
	}
}

// populateWeightProjection — D-3 FCR 기반 평균 체중 추정값을 BiologicalContext 에 채운다.
// D-4: FCRSource(default|calibrated) 도 함께 복사.
func (s *Server) populateWeightProjection(ctx context.Context, tankID string, out *BiologicalContext) {
	proj, ok, err := biomass.LoadAndProject(ctx, s.store, tankID)
	if err != nil || !ok {
		// no_lifecycle 케이스 — 활성 lineage 없음 note 는 이미 추가됐으므로 중복 방지
		return
	}
	out.EstimatedAvgWeightG = proj.EstimatedAvgWeightG
	out.EstimationQuality = proj.Quality
	out.EstimationAnchor = proj.AnchorSource
	out.EstimationFCR = proj.ExpectedFCR
	out.EstimationFCRSource = proj.FCRSource
	// projection 의 notes 를 BiologicalContext notes 에 병합
	out.Notes = append(out.Notes, proj.Notes...)
}

// ── Autonomous — Phase 4 C-1 자율 운영 모드 현재 상태 ──

// collectAutonomousState — current_tank_autonomous_mode projection 에서 모드 읽기.
// 행 없으면 기본값 off (첫 부팅 상태).
// C-3: auto_downgrade 이벤트 + blocked 이벤트 최근 1건도 함께 조회.
func (s *Server) collectAutonomousState(r *http.Request, tankID string) AutonomousState {
	out := AutonomousState{Notes: []string{}}
	row, err := s.store.GetTankAutonomousMode(r.Context(), tankID)
	if err != nil {
		out.Mode = "off"
		out.Notes = append(out.Notes, "모드 조회 실패: "+err.Error())
		return out
	}
	if row == nil {
		out.Mode = "off"
		out.Notes = append(out.Notes, "자율 모드 미설정 (기본값 off)")
		return out
	}
	out.Mode = row.Mode
	out.Reason = row.Reason
	out.ChangedAt = fmtAPITime(row.ChangedAt)
	out.ChangedBy = row.ChangedBy
	switch row.Mode {
	case "off":
		out.Notes = append(out.Notes, "자율 X — 모든 결정 운영자 승인")
	case "observation":
		out.Notes = append(out.Notes, "AI 학습만, 자율 결정 X")
	case "partial":
		out.Notes = append(out.Notes, "일부 자율 영역 활성 (C-2 이후 영역 세분화)")
	case "full":
		out.Notes = append(out.Notes, "모든 사후 보고 가능 영역 자율")
	}

	// C-3: auto_downgrade 플래그 — 마지막 mode 변경이 system 에 의해 발생했는지.
	if row.ChangedBy == "system" || row.ChangedBy == "baseline_worker" {
		out.AutoDowngraded = true
	}

	// C-3: 가장 최근 blocked 이벤트 조회.
	if blocked := s.latestBlockedEvent(r.Context(), tankID); blocked != nil {
		out.LastBlockedAt = blocked.BlockedAt
		out.LastBlockedReason = blocked.Reason
	}
	return out
}

// latestBlockedEvent — tank.autonomous_action.blocked 이벤트에서 이 Cage/Tank의 최근 1건.
func (s *Server) latestBlockedEvent(ctx context.Context, tankID string) *events.AutonomousActionBlockedPayload {
	es, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventAutonomousActionBlocked,
		Limit:     50,
	})
	if err != nil {
		return nil
	}
	for _, e := range es {
		var p events.AutonomousActionBlockedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if p.TankID == tankID {
			return &p
		}
	}
	return nil
}

// ── Confidence — Phase 3: ComputeTankConfidence 로 실제 점수 산정 ──

func (s *Server) collectConfidenceState(r *http.Request, tankID string) ConfidenceState {
	out := ConfidenceState{
		AdaptationLevel: "cold",
		Notes:           []string{},
	}
	components, err := baseline.ComputeTankConfidence(r.Context(), s.store, tankID)
	if err != nil {
		out.Notes = append(out.Notes, "confidence 산출 실패: "+err.Error())
		return out
	}
	out.AdaptationLevel = components.AdaptationLevel
	out.HasActiveWeights = components.HasBaseline || components.HasForecast
	out.TankConfidenceScore = &components.Composite
	out.Components = &components
	out.Notes = components.Notes
	return out
}

// ── Decisions — Phase 4 C-2 AI 결정 라우팅 현재 상태 ──

// collectDecisionState — 최근 라우팅 이벤트 + pending 미리보기 + C-4 정책.
func (s *Server) collectDecisionState(r *http.Request, tankID string) DecisionState {
	out := DecisionState{Notes: []string{}}

	// 가장 최근 라우팅 이벤트
	if last, ok := s.lastDecisionRouted(r, tankID); ok {
		out.LastRoutedAt = fmtDecisionTime(last.ProposedAt)
		out.LastRoute = last.Route
		out.LastDecisionKind = last.DecisionKind
		out.LastReasoning = last.Reasoning
	} else {
		out.Notes = append(out.Notes, "아직 AI 결정 라우팅 이벤트 없음")
	}

	// C-4: 자동 실행 정책
	enabled, grace := s.effectiveDecisionPolicy(r.Context(), tankID)
	out.AutoExecuteEnabled = enabled
	out.GraceMinutes = grace
	policy, _ := s.store.GetTankDecisionPolicy(r.Context(), tankID)
	if policy != nil {
		out.PolicySource = "tank_override"
	} else {
		out.PolicySource = "system_default"
	}

	// pending 미리보기 (최대 5건) — pending_notify 에 auto_execute_at 추가
	pending := s.collectPendingDecisions(r, tankID, 5)
	out.PendingCount = len(s.collectPendingDecisions(r, tankID, 200))
	if len(pending) > 0 {
		// pending_notify 결정에 auto_execute_at 계산해서 주입
		for _, item := range pending {
			if item["route"] == "pending_notify" {
				if proposedAt, ok := item["proposed_at"].(string); ok {
					if t, err := time.Parse(time.RFC3339Nano, proposedAt); err == nil {
						autoAt := t.Add(time.Duration(grace) * time.Minute)
						item["auto_execute_at"] = autoAt.UTC().Format(time.RFC3339Nano)
						remaining := time.Until(autoAt)
						if remaining <= 0 {
							item["auto_execute_in_minutes"] = 0
						} else {
							item["auto_execute_in_minutes"] = int(remaining.Minutes()) + 1
						}
					}
				}
			}
		}
		out.PendingItems = pending
	}
	return out
}
