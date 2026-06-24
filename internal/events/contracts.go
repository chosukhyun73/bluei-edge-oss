package events

import (
	"errors"
	"fmt"
	"time"
)

const SchemaVersion = "1.0"

const (
	EventEdgeHeartbeatRecorded        = "edge.heartbeat.recorded"
	EventSensorReadingRecorded        = "sensor.reading.recorded"
	EventDeviceHealthUpdated          = "device.health.updated"
	EventCameraHealthUpdated          = "camera.health.updated"
	EventCameraProfileUpserted        = "camera.profile.upserted"
	EventFeedingRecorded              = "feeding.recorded"
	EventFeedingRecommendationCreated = "feeding.recommendation.created"
	EventVisionObservationRecorded    = "vision.observation.recorded"
	EventVisionObservationDisputed    = "vision.observation.disputed"
	EventVisionTrainingJobUpdate      = "vision.training.job.update"
	EventVisionAlgorithmApplied       = "vision.algorithm.applied"
	EventVisionBootstrapLabelRecorded = "vision.bootstrap.label.recorded"
	EventTankBaselineScored           = "tank.baseline.scored"
	EventTankTransitionDetected       = "tank.transition.detected"
	EventWaterForecastRecorded        = "water.forecast.recorded"
	EventMediaClipStored              = "media.clip.stored"
	EventMediaClipExcluded            = "media.clip.excluded"
	EventAlertRaised                  = "alert.raised"
	EventAlertUpdated                 = "alert.updated"
	EventTankAutonomousModeChanged    = "tank.autonomous_mode.changed"
	EventTankDecisionRouted           = "tank.decision.routed"
	EventTankDecisionResolved         = "tank.decision.resolved"
	EventAutonomousActionExecuted     = "tank.autonomous_action.executed"
	EventAutonomousActionBlocked      = "tank.autonomous_action.blocked"
	EventAutonomousModeAutoDowngrade  = "tank.autonomous_mode.auto_downgrade"
	EventTankStockingRecorded         = "tank.stocking.recorded"
	EventTankHarvestRecorded          = "tank.harvest.recorded"
	EventTankSamplingRecorded         = "tank.sampling.recorded"
	EventTankFCRCalibrated            = "tank.fcr.calibrated"
	// GDST/ASC first-mile 생산 CTE — docs/49-gdst-traceability-contract.md
	EventTankTreatmentRecorded = "tank.treatment.recorded"
	EventTankMortalityRecorded = "tank.mortality.recorded"
	EventTankTransferRecorded  = "tank.transfer.recorded"
	EventTankDocumentAttached  = "tank.document.attached"
	// 재고관리 — 구매(입고) / 사용(출고)
	EventInventoryPurchaseRecorded    = "inventory.purchase.recorded"
	EventInventoryConsumptionRecorded = "inventory.consumption.recorded"
	// site 단위 거래 — 입식 배치(→다중 수조 분배) / 출하 건(→다중 수조·부분 line item)
	EventSiteStockingRecorded = "site.stocking.recorded"
	EventSiteHarvestRecorded  = "site.harvest.recorded"

	// Feed cycle events (Phase 3)
	EventFeedCycleStarted   = "feed.cycle.started"
	EventFeedCycleCompleted = "feed.cycle.completed"
	EventFeedPulseStarted   = "feed.pulse.started"
	EventFeedPulseCompleted = "feed.pulse.completed"
	EventFeedGapStarted     = "feed.gap.started"
	EventFeedGapCompleted   = "feed.gap.completed"

	// Arbiter preemption events (Phase 6)
	EventFeedCyclePreempted = "feed.cycle.preempted"

	// Load cell events (Phase 5)
	EventFeedPulseWeight      = "feed.pulse.weight"
	EventFeedSiloDepletion    = "feed.silo.depletion"
	EventFeedOverflowDetected = "feed.overflow.detected"
)

const (
	MetricWaterTemperature     = "water_temperature" // field label: watertemp
	MetricDissolvedOxygen      = "dissolved_oxygen"  // field label: DO
	MetricPH                   = "ph"
	MetricSalinity             = "salinity"
	MetricNitrite              = "nitrite"                // field label: NO2
	MetricNitrate              = "nitrate"                // field label: NO3
	MetricCarbonDioxide        = "carbon_dioxide"         // field label: CO2
	MetricTotalSuspendedSolids = "total_suspended_solids" // field label: TTS/TSS
	MetricUnionizedAmmonia     = "unionized_ammonia"      // field label: NH3
	MetricTurbidity            = "turbidity"
	MetricORP                  = "orp"
	MetricAmmonia              = "ammonia" // generic ammonia, use unionized_ammonia for NH3 when available
	MetricWaterLevel           = "water_level"
	MetricFlowRate             = "flow_rate"
	MetricPumpPressure         = "pump_pressure"
	MetricFeedWeight           = "feed_weight"
	MetricLightIntensity       = "light_intensity"
	MetricUnknown              = "unknown"
)

const (
	QualityOK      = "ok"
	QualitySuspect = "suspect"
	QualityStale   = "stale"
	QualityMissing = "missing"
	QualityError   = "error"
)

const (
	DeviceTypeWaterQualitySensor = "water_quality_sensor"
	DeviceTypeLocalGateway       = "local_gateway"
	DeviceTypeCamera             = "camera"
	DeviceTypeEdgeNode           = "edge_node"
	DeviceTypeFeeder             = "feeder"
	DeviceTypePump               = "pump"
	DeviceTypeOxygenSupplier     = "oxygen_supplier"
	DeviceTypeAerator            = "aerator"
	DeviceTypeUVSterilizer       = "uv_sterilizer"
	DeviceTypeOzoneGenerator     = "ozone_generator"
	DeviceTypeBiofilter          = "biofilter"
	DeviceTypePhotoperiodLight   = "photoperiod_light"
	DeviceTypeUnknown            = "unknown"
)

const (
	DeviceStatusOnline   = "online"
	DeviceStatusDegraded = "degraded"
	DeviceStatusDown     = "down"
	DeviceStatusDisabled = "disabled"
	DeviceStatusUnknown  = "unknown"
)

const (
	FeedingSourceManual         = "manual"
	FeedingSourceFeederCommand  = "feeder_command"
	FeedingSourceRecommendation = "recommendation"
)

const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

const (
	AlertStatusOpen         = "open"
	AlertStatusAcknowledged = "acknowledged"
	AlertStatusResolved     = "resolved"
	AlertStatusClosed       = "closed"
)

// Location identifies where a reading, device, or camera belongs in the field.
type Location struct {
	AreaID         string   `json:"area_id,omitempty"`
	TankID         string   `json:"tank_id,omitempty"`
	PlatformTankID string   `json:"platform_tank_id,omitempty"`
	Lat            *float64 `json:"lat,omitempty"`
	Lon            *float64 `json:"lon,omitempty"`
}

// SensorReadingPayload is the canonical payload for sensor.reading.recorded.
type SensorReadingPayload struct {
	ReadingID  string         `json:"reading_id"`
	SensorID   string         `json:"sensor_id"`
	DeviceID   string         `json:"device_id"`
	Metric     string         `json:"metric"`
	Value      *float64       `json:"value"`
	Unit       string         `json:"unit"`
	Quality    string         `json:"quality"`
	ObservedAt string         `json:"observed_at"`
	Location   Location       `json:"location"`
	Raw        map[string]any `json:"raw,omitempty"`
}

func (p SensorReadingPayload) Validate() error {
	if p.ReadingID == "" {
		return errors.New("reading_id is required")
	}
	if p.SensorID == "" {
		return errors.New("sensor_id is required")
	}
	if p.DeviceID == "" {
		return errors.New("device_id is required")
	}
	if !ValidMetric(p.Metric) {
		return fmt.Errorf("invalid metric: %s", p.Metric)
	}
	if p.Unit == "" {
		return errors.New("unit is required")
	}
	if !ValidQuality(p.Quality) {
		return fmt.Errorf("invalid quality: %s", p.Quality)
	}
	if p.Quality == QualityOK || p.Quality == QualitySuspect {
		if p.Value == nil {
			return errors.New("value is required when quality is ok or suspect")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ObservedAt); err != nil {
		return fmt.Errorf("observed_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// DeviceHealthPayload is the canonical payload for device.health.updated.
type DeviceHealthPayload struct {
	DeviceID   string         `json:"device_id"`
	DeviceType string         `json:"device_type"`
	TankID     string         `json:"tank_id,omitempty"`
	Status     string         `json:"status"`
	Quality    string         `json:"quality"`
	LastSeenAt string         `json:"last_seen_at,omitempty"`
	ErrorCode  *string        `json:"error_code"`
	Details    map[string]any `json:"details,omitempty"`
}

func (p DeviceHealthPayload) Validate() error {
	if p.DeviceID == "" {
		return errors.New("device_id is required")
	}
	if p.DeviceType == "" {
		return errors.New("device_type is required")
	}
	if !ValidDeviceStatus(p.Status) {
		return fmt.Errorf("invalid status: %s", p.Status)
	}
	if !ValidQuality(p.Quality) {
		return fmt.Errorf("invalid quality: %s", p.Quality)
	}
	if p.LastSeenAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.LastSeenAt); err != nil {
			return fmt.Errorf("last_seen_at must be RFC3339/RFC3339Nano: %w", err)
		}
	}
	return nil
}

// CameraHealthPayload is the canonical payload for camera.health.updated.
type CameraHealthPayload struct {
	CameraID       string  `json:"camera_id"`
	TankID         string  `json:"tank_id,omitempty"`
	Status         string  `json:"status"`
	IngestFPS      float64 `json:"ingest_fps"`
	LastFrameAt    string  `json:"last_frame_at,omitempty"`
	ReconnectCount int     `json:"reconnect_count"`
	DroppedFrames  int     `json:"dropped_frames"`
}

func (p CameraHealthPayload) Validate() error {
	if p.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if !ValidDeviceStatus(p.Status) {
		return fmt.Errorf("invalid status: %s", p.Status)
	}
	if p.IngestFPS < 0 {
		return errors.New("ingest_fps cannot be negative")
	}
	if p.ReconnectCount < 0 {
		return errors.New("reconnect_count cannot be negative")
	}
	if p.DroppedFrames < 0 {
		return errors.New("dropped_frames cannot be negative")
	}
	if p.LastFrameAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.LastFrameAt); err != nil {
			return fmt.Errorf("last_frame_at must be RFC3339/RFC3339Nano: %w", err)
		}
	}
	return nil
}

// FeedingRecordedPayload is the canonical payload for feeding.recorded.
// It records actual feed supplied or operator-entered feeding history.
type FeedingRecordedPayload struct {
	FeedingID              string         `json:"feeding_id"`
	TankID                 string         `json:"tank_id"`
	FeederID               string         `json:"feeder_id,omitempty"`
	Source                 string         `json:"source"`
	FeedAmountG            float64        `json:"feed_amount_g"`
	FeedType               string         `json:"feed_type,omitempty"`
	FeedLot                string         `json:"feed_lot,omitempty"`      // GDST KDE: 사료 lot/batch
	FeedSupplier           string         `json:"feed_supplier,omitempty"` // GDST KDE: 사료 공급사
	ItemID                 string         `json:"item_id,omitempty"`       // 재고 차감 대상 품목
	ConsumedQty            float64        `json:"consumed_qty,omitempty"`  // 품목 단위 사용량 (재고 −)
	FedAt                  string         `json:"fed_at"`
	RecordedBy             string         `json:"recorded_by,omitempty"`
	LinkedRecommendationID string         `json:"linked_recommendation_id,omitempty"`
	Quality                string         `json:"quality"`
	Evidence               map[string]any `json:"evidence,omitempty"`
}

func (p FeedingRecordedPayload) Validate() error {
	if p.FeedingID == "" {
		return errors.New("feeding_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if !ValidFeedingSource(p.Source) {
		return fmt.Errorf("invalid feeding source: %s", p.Source)
	}
	if p.FeedAmountG < 0 {
		return errors.New("feed_amount_g cannot be negative")
	}
	if !ValidQuality(p.Quality) {
		return fmt.Errorf("invalid quality: %s", p.Quality)
	}
	if _, err := time.Parse(time.RFC3339Nano, p.FedAt); err != nil {
		return fmt.Errorf("fed_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// FeedingRecommendationPayload is the canonical payload for feeding.recommendation.created.
// It is advisory; direct equipment control must remain separately gated and audited.
type FeedingRecommendationPayload struct {
	RecommendationID   string         `json:"recommendation_id"`
	TankID             string         `json:"tank_id"`
	RecommendedAmountG float64        `json:"recommended_amount_g"`
	RecommendedAt      string         `json:"recommended_at"`
	ValidUntil         string         `json:"valid_until,omitempty"`
	Confidence         float64        `json:"confidence"`
	ReasonCodes        []string       `json:"reason_codes"`
	Inputs             map[string]any `json:"inputs,omitempty"`
}

func (p FeedingRecommendationPayload) Validate() error {
	if p.RecommendationID == "" {
		return errors.New("recommendation_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.RecommendedAmountG < 0 {
		return errors.New("recommended_amount_g cannot be negative")
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.RecommendedAt); err != nil {
		return fmt.Errorf("recommended_at must be RFC3339/RFC3339Nano: %w", err)
	}
	if p.ValidUntil != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.ValidUntil); err != nil {
			return fmt.Errorf("valid_until must be RFC3339/RFC3339Nano: %w", err)
		}
	}
	return nil
}

// VisionObservationPayload is a non-actuating camera/AI observation.
// It can trigger enhanced analysis or operator review, but must not directly control equipment.
type VisionObservationPayload struct {
	ObservationID string             `json:"observation_id"`
	CameraID      string             `json:"camera_id"`
	TankID        string             `json:"tank_id,omitempty"`
	Mode          string             `json:"mode"`  // lightweight | enhanced
	Phase         string             `json:"phase"` // normal | feeding_start | feeding_stop | post_feeding | anomaly
	ObservedAt    string             `json:"observed_at"`
	FrameTS       string             `json:"frame_ts,omitempty"`
	FrameRef      string             `json:"frame_ref,omitempty"`
	ClipRef       string             `json:"clip_ref,omitempty"`
	ModelVersion  string             `json:"model_version,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Scores        map[string]float64 `json:"scores,omitempty"`
	Candidates    []string           `json:"candidates,omitempty"`
	Evidence      map[string]any     `json:"evidence,omitempty"`
	Quality       string             `json:"quality"`
}

func (p VisionObservationPayload) Validate() error {
	if p.ObservationID == "" {
		return errors.New("observation_id is required")
	}
	if p.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if p.Mode != "lightweight" && p.Mode != "enhanced" {
		return fmt.Errorf("invalid vision mode: %s", p.Mode)
	}
	if p.Phase == "" {
		return errors.New("phase is required")
	}
	if !ValidQuality(p.Quality) {
		return fmt.Errorf("invalid quality: %s", p.Quality)
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ObservedAt); err != nil {
		return fmt.Errorf("observed_at must be RFC3339/RFC3339Nano: %w", err)
	}
	if p.FrameTS != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.FrameTS); err != nil {
			return fmt.Errorf("frame_ts must be RFC3339/RFC3339Nano: %w", err)
		}
	}
	if p.Quality == QualityOK || p.Quality == QualitySuspect {
		if p.ModelVersion == "" {
			return errors.New("model_version is required when quality is ok or suspect")
		}
		if p.Confidence == nil {
			return errors.New("confidence is required when quality is ok or suspect")
		}
	}
	if p.Confidence != nil && (*p.Confidence < 0 || *p.Confidence > 1) {
		return errors.New("confidence must be between 0 and 1")
	}
	for k, v := range p.Scores {
		if k == "" {
			return errors.New("score key cannot be empty")
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("score %s must be between 0 and 1", k)
		}
	}
	return nil
}

// VisionObservationDisputedPayload records operator disagreement/correction for trust accumulation.
//
// 점수 라벨 (3 채널, backward compatible):
//   - OperatorScore — G-4 cycle 종료 dispute auto-modal 의 단일 0~1 슬라이더 (legacy)
//   - PreScore      — R5: 사료급이 *전* 7초 영상의 안정성 0~1 (시점별 분리)
//   - DuringScore   — R5: 사료급이 *도중* 7초 영상의 사료 반응 0~1
//   - ClipRef       — R5: 라벨이 가리키는 mp4 path (observation 의 clip_ref 와 일관)
//
// nil 이면 verdict 만 기록. fc2 fine-tune 의 정밀 학습 자료.
type VisionObservationDisputedPayload struct {
	DisputeID     string   `json:"dispute_id"`
	ObservationID string   `json:"observation_id"`
	CameraID      string   `json:"camera_id"`
	TankID        string   `json:"tank_id,omitempty"`
	OperatorID    string   `json:"operator_id"`
	Verdict       string   `json:"verdict"` // correct | wrong | unsure
	Reason        string   `json:"reason,omitempty"`
	OperatorScore *float64 `json:"operator_score,omitempty"` // G-4: 단일 슬라이더 (legacy)
	PreScore      *float64 `json:"pre_score,omitempty"`      // R5: 급이 전 안정성 0~1
	DuringScore   *float64 `json:"during_score,omitempty"`   // R5: 급이 도중 반응 0~1
	ClipRef       string   `json:"clip_ref,omitempty"`       // R5: 라벨 영상 mp4 path (observation 정합)
	AlgorithmID   string   `json:"algorithm_id,omitempty"`   // R11: 어느 LRCN 모델용 라벨인지 추적
	DisputedAt    string   `json:"disputed_at"`
}

func (p VisionObservationDisputedPayload) Validate() error {
	if p.DisputeID == "" {
		return errors.New("dispute_id is required")
	}
	if p.ObservationID == "" {
		return errors.New("observation_id is required")
	}
	if p.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if p.Verdict != "correct" && p.Verdict != "wrong" && p.Verdict != "unsure" {
		return fmt.Errorf("invalid verdict: %s", p.Verdict)
	}
	if p.OperatorScore != nil && (*p.OperatorScore < 0 || *p.OperatorScore > 1) {
		return errors.New("operator_score must be between 0 and 1")
	}
	if p.PreScore != nil && (*p.PreScore < 0 || *p.PreScore > 1) {
		return errors.New("pre_score must be between 0 and 1")
	}
	if p.DuringScore != nil && (*p.DuringScore < 0 || *p.DuringScore > 1) {
		return errors.New("during_score must be between 0 and 1")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.DisputedAt); err != nil {
		return fmt.Errorf("disputed_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// VisionTrainingJobUpdatePayload records lifecycle of a single AI training job.
// Status transitions: started -> progress* -> completed | failed | canceled.
type VisionTrainingJobUpdatePayload struct {
	JobID         string         `json:"job_id"`
	AlgorithmID   string         `json:"algorithm_id"`
	Status        string         `json:"status"`                // started | progress | completed | failed | canceled
	StageLabel    string         `json:"stage_label,omitempty"` // 한글, 운영자 노출용 (예: "사진 분석 중")
	ProgressPct   *float64       `json:"progress_pct,omitempty"`
	CandidatePath string         `json:"candidate_path,omitempty"` // 학습 결과 weights 위치
	Metrics       map[string]any `json:"metrics,omitempty"`        // accuracy / latency / correlation 등
	Error         string         `json:"error,omitempty"`
	UpdatedAt     string         `json:"updated_at"`
}

func (p VisionTrainingJobUpdatePayload) Validate() error {
	if p.JobID == "" {
		return errors.New("job_id is required")
	}
	if p.AlgorithmID == "" {
		return errors.New("algorithm_id is required")
	}
	switch p.Status {
	case "started", "progress", "completed", "failed", "canceled":
	default:
		return fmt.Errorf("invalid training status: %s", p.Status)
	}
	if p.ProgressPct != nil && (*p.ProgressPct < 0 || *p.ProgressPct > 100) {
		return errors.New("progress_pct must be between 0 and 100")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.UpdatedAt); err != nil {
		return fmt.Errorf("updated_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// VisionBootstrapBox is a single annotation drawn by the operator on a frame.
// Coordinates are normalized to [0,1] so the same label is portable across
// resolutions/snapshots.
type VisionBootstrapBox struct {
	X     float64 `json:"x"`     // 0..1, 좌측 상단 기준
	Y     float64 `json:"y"`     // 0..1
	W     float64 `json:"w"`     // 0..1
	H     float64 `json:"h"`     // 0..1
	Class string  `json:"class"` // fish | food | exclude
}

// VisionBootstrapLabelRecordedPayload captures a "first day" labeling session
// where an operator marks fish positions on a snapshot before any trained
// detector exists. Used as YOLO fine-tune seed data.
type VisionBootstrapLabelRecordedPayload struct {
	LabelID     string               `json:"label_id"`
	CameraID    string               `json:"camera_id"`
	TankID      string               `json:"tank_id,omitempty"`
	SnapshotRef string               `json:"snapshot_ref,omitempty"` // 캡처 시각의 frame 식별자
	OperatorID  string               `json:"operator_id"`
	Boxes       []VisionBootstrapBox `json:"boxes"`
	RecordedAt  string               `json:"recorded_at"`
}

func (p VisionBootstrapLabelRecordedPayload) Validate() error {
	if p.LabelID == "" {
		return errors.New("label_id is required")
	}
	if p.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if len(p.Boxes) == 0 {
		return errors.New("at least one box is required")
	}
	for i, b := range p.Boxes {
		if b.W <= 0 || b.H <= 0 {
			return fmt.Errorf("box %d: width and height must be > 0", i)
		}
		if b.X < 0 || b.Y < 0 || b.X+b.W > 1.001 || b.Y+b.H > 1.001 {
			return fmt.Errorf("box %d: coords out of [0,1] range", i)
		}
		switch b.Class {
		case "fish", "food", "exclude":
		default:
			return fmt.Errorf("box %d: invalid class %q (allowed: fish|food|exclude)", i, b.Class)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, p.RecordedAt); err != nil {
		return fmt.Errorf("recorded_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// TankBaselineScoredPayload — Cage/Tank baseline autoencoder 의 한 시점 평가 결과.
// docs/29 §3.0 의 "각 수조는 독립" 원칙에 따라 tank_id 단위로 산출/기록한다.
// verdict 는 학습 시 산출된 분포의 percentile 임계 (p95/p99) 기준으로 결정.
// 이 이벤트는 비-actuating — rules engine 의 안전 결정에 직접 연결되지 않음.
type TankBaselineScoredPayload struct {
	TankID       string             `json:"tank_id"`
	ModelDir     string             `json:"model_dir"`
	JobID        string             `json:"job_id,omitempty"`
	AnomalyScore float64            `json:"anomaly_score"`
	P95Threshold float64            `json:"p95_threshold"`
	P99Threshold float64            `json:"p99_threshold"`
	Verdict      string             `json:"verdict"` // normal | warning | anomaly
	FeatureDiff  map[string]float64 `json:"feature_diff,omitempty"`
	EvaluatedAt  string             `json:"evaluated_at"`
}

func (p TankBaselineScoredPayload) Validate() error {
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.ModelDir == "" {
		return errors.New("model_dir is required")
	}
	switch p.Verdict {
	case "normal", "warning", "anomaly":
	default:
		return fmt.Errorf("invalid verdict: %s (allowed: normal|warning|anomaly)", p.Verdict)
	}
	if _, err := time.Parse(time.RFC3339Nano, p.EvaluatedAt); err != nil {
		return fmt.Errorf("evaluated_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// TankTransitionDetectedPayload — Cage/Tank 데이터 분포 전환 감지 (docs/29 §3.5).
// 비-actuating: rules engine 의 안전 결정에 직접 연결되지 않음. 운영자가 baseline 재학습을
// 트리거하기 위한 신호 + audit 기록.
type TankTransitionDetectedPayload struct {
	TankID     string         `json:"tank_id"`
	Reason     string         `json:"reason"` // weight_threshold_passed | anomaly_drift_detected
	DetectedAt string         `json:"detected_at"`
	Evidence   map[string]any `json:"evidence,omitempty"`
}

func (p TankTransitionDetectedPayload) Validate() error {
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	switch p.Reason {
	case "weight_threshold_passed", "anomaly_drift_detected":
	default:
		return fmt.Errorf("invalid reason: %s (allowed: weight_threshold_passed|anomaly_drift_detected)", p.Reason)
	}
	if p.DetectedAt == "" {
		return errors.New("detected_at is required")
	}
	if _, err := time.Parse(time.RFC3339, p.DetectedAt); err != nil {
		if _, err2 := time.Parse(time.RFC3339Nano, p.DetectedAt); err2 != nil {
			return fmt.Errorf("detected_at must be RFC3339: %w", err)
		}
	}
	return nil
}

// WaterForecastRecordedPayload — 단기 수질 예측 (Phase 2). 비-actuating, AI 보조 신호.
// 한 번의 forecast 가 여러 horizon 의 예측을 묶어 하나의 이벤트로 적재된다
// (t+10/30/60/120 분 등). horizon_minutes 의 길이와 predicted_values 의 길이가 같다.
type WaterForecastRecordedPayload struct {
	TankID          string         `json:"tank_id"`
	ModelDir        string         `json:"model_dir"`
	JobID           string         `json:"job_id,omitempty"`
	Metric          string         `json:"metric"` // dissolved_oxygen | water_temperature | ph
	HorizonMinutes  []int          `json:"horizon_minutes"`
	PredictedValues []float64      `json:"predicted_values"`
	BaselineRMSE    float64        `json:"baseline_rmse,omitempty"` // 학습 시 측정된 RMSE
	CurrentValue    *float64       `json:"current_value,omitempty"`
	Evidence        map[string]any `json:"evidence,omitempty"`
	EvaluatedAt     string         `json:"evaluated_at"`
}

func (p WaterForecastRecordedPayload) Validate() error {
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.ModelDir == "" {
		return errors.New("model_dir is required")
	}
	if p.Metric == "" {
		return errors.New("metric is required")
	}
	if len(p.HorizonMinutes) == 0 {
		return errors.New("horizon_minutes must be non-empty")
	}
	if len(p.HorizonMinutes) != len(p.PredictedValues) {
		return fmt.Errorf("horizon_minutes len %d != predicted_values len %d",
			len(p.HorizonMinutes), len(p.PredictedValues))
	}
	for _, h := range p.HorizonMinutes {
		if h <= 0 {
			return fmt.Errorf("horizon must be > 0, got %d", h)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, p.EvaluatedAt); err != nil {
		return fmt.Errorf("evaluated_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// VisionAlgorithmAppliedPayload records operator decision to promote a candidate
// algorithm to the active runtime, or to roll back to a previous one.
type VisionAlgorithmAppliedPayload struct {
	Action              string `json:"action"` // promote | rollback
	AlgorithmID         string `json:"algorithm_id"`
	PreviousAlgorithmID string `json:"previous_algorithm_id,omitempty"`
	JobID               string `json:"job_id,omitempty"` // promote 의 경우, 어느 학습 결과를 적용했는지
	OperatorID          string `json:"operator_id"`
	AppliedAt           string `json:"applied_at"`
}

func (p VisionAlgorithmAppliedPayload) Validate() error {
	if p.Action != "promote" && p.Action != "rollback" {
		return fmt.Errorf("invalid action: %s", p.Action)
	}
	if p.AlgorithmID == "" {
		return errors.New("algorithm_id is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.AppliedAt); err != nil {
		return fmt.Errorf("applied_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// MediaClipExcludedPayload — R8. 학습 데이터로 부적합한 영상(전체 가림/어둠 등)
// 격리 기록. 실제 파일은 captures/excluded/<reason>/ 로 이동, training-pool 에서 제외.
type MediaClipExcludedPayload struct {
	ClipID      string `json:"clip_id"`
	Reason      string `json:"reason"` // occlusion | low_visibility | other
	Memo        string `json:"memo,omitempty"`
	OperatorID  string `json:"operator_id"`
	OriginalURI string `json:"original_uri"`
	NewURI      string `json:"new_uri"`
	ExcludedAt  string `json:"excluded_at"`
}

func (p MediaClipExcludedPayload) Validate() error {
	if p.ClipID == "" {
		return errors.New("clip_id is required")
	}
	if p.Reason != "occlusion" && p.Reason != "low_visibility" && p.Reason != "other" {
		return fmt.Errorf("invalid reason: %s (must be occlusion|low_visibility|other)", p.Reason)
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if p.OriginalURI == "" {
		return errors.New("original_uri is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ExcludedAt); err != nil {
		return fmt.Errorf("excluded_at must be RFC3339/RFC3339Nano: %w", err)
	}
	return nil
}

// MediaClipStoredPayload records evidence media captured from a camera stream.
type MediaClipStoredPayload struct {
	ClipID     string         `json:"clip_id"`
	CameraID   string         `json:"camera_id"`
	TankID     string         `json:"tank_id,omitempty"`
	Reason     string         `json:"reason"`
	StartedAt  string         `json:"started_at"`
	EndedAt    string         `json:"ended_at"`
	URI        string         `json:"uri"`
	MimeType   string         `json:"mime_type"`
	SizeBytes  int64          `json:"size_bytes"`
	FrameCount int            `json:"frame_count"`
	Evidence   map[string]any `json:"evidence,omitempty"`
}

func (p MediaClipStoredPayload) Validate() error {
	if p.ClipID == "" {
		return errors.New("clip_id is required")
	}
	if p.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if p.Reason == "" {
		return errors.New("reason is required")
	}
	startedAt, err := time.Parse(time.RFC3339Nano, p.StartedAt)
	if err != nil {
		return fmt.Errorf("started_at must be RFC3339/RFC3339Nano: %w", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, p.EndedAt)
	if err != nil {
		return fmt.Errorf("ended_at must be RFC3339/RFC3339Nano: %w", err)
	}
	if endedAt.Before(startedAt) {
		return errors.New("ended_at cannot be before started_at")
	}
	if p.URI == "" {
		return errors.New("uri is required")
	}
	if p.MimeType == "" {
		return errors.New("mime_type is required")
	}
	if p.SizeBytes < 0 {
		return errors.New("size_bytes cannot be negative")
	}
	if p.FrameCount < 0 {
		return errors.New("frame_count cannot be negative")
	}
	return nil
}

// AlertPayload is the canonical payload for alert.raised and alert.updated.
type AlertPayload struct {
	AlertID   string         `json:"alert_id"`
	AlertType string         `json:"alert_type"`
	Severity  string         `json:"severity"`
	Status    string         `json:"status"`
	Subject   AlertSubject   `json:"subject"`
	RuleID    string         `json:"rule_id,omitempty"`
	Message   string         `json:"message"`
	Evidence  map[string]any `json:"evidence,omitempty"`
	RaisedAt  string         `json:"raised_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

type AlertSubject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (p AlertPayload) Validate() error {
	if p.AlertID == "" {
		return errors.New("alert_id is required")
	}
	if p.AlertType == "" {
		return errors.New("alert_type is required")
	}
	if !ValidSeverity(p.Severity) {
		return fmt.Errorf("invalid severity: %s", p.Severity)
	}
	if !ValidAlertStatus(p.Status) {
		return fmt.Errorf("invalid status: %s", p.Status)
	}
	if p.Subject.Kind == "" || p.Subject.ID == "" {
		return errors.New("subject.kind and subject.id are required")
	}
	if p.Message == "" {
		return errors.New("message is required")
	}
	if p.RaisedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.RaisedAt); err != nil {
			return fmt.Errorf("raised_at must be RFC3339/RFC3339Nano: %w", err)
		}
	}
	if p.UpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.UpdatedAt); err != nil {
			return fmt.Errorf("updated_at must be RFC3339/RFC3339Nano: %w", err)
		}
	}
	return nil
}

// TankAutonomousModeChangedPayload — Cage/Tank별 자율 운영 모드 변경 audit (Phase 4 C-1).
// 비-actuating: 단순 audit. AI 가 이 모드를 읽어 자율 결정 여부를 결정 (C-2/C-3).
type TankAutonomousModeChangedPayload struct {
	TankID       string `json:"tank_id"`
	PreviousMode string `json:"previous_mode,omitempty"` // 첫 변경이면 빈 문자열
	NewMode      string `json:"new_mode"`
	OperatorID   string `json:"operator_id"`
	Reason       string `json:"reason,omitempty"`
	ChangedAt    string `json:"changed_at"`
}

func (p TankAutonomousModeChangedPayload) Validate() error {
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if !ValidAutonomousMode(p.NewMode) {
		return fmt.Errorf("invalid new_mode: %s (allowed: off|observation|partial|full)", p.NewMode)
	}
	if p.ChangedAt == "" {
		return errors.New("changed_at is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ChangedAt); err != nil {
		return fmt.Errorf("changed_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// TankDecisionRoutedPayload — AI 결정 라우팅 audit (Phase 4 C-2).
// 비-actuating: 라우팅 결정만 기록. 실제 control command 발행은 C-3.
type TankDecisionRoutedPayload struct {
	DecisionID      string         `json:"decision_id"`
	TankID          string         `json:"tank_id"`
	DecisionKind    string         `json:"decision_kind"` // feeding | oxygen_supply | water_exchange | pump_adjust | monitoring
	DecisionData    map[string]any `json:"decision_data,omitempty"`
	ProposingSource string         `json:"proposing_source"` // 어떤 AI/룰이 제안했는지
	Confidence      float64        `json:"confidence"`       // Tank Confidence Composite (0..1)
	AutonomousMode  string         `json:"autonomous_mode"`  // 라우팅 시점의 mode
	Route           string         `json:"route"`            // auto_executed | pending_notify | pending_approval | advisory_only | rejected
	Reasoning       string         `json:"reasoning,omitempty"`
	ProposedAt      string         `json:"proposed_at"`
}

func (p TankDecisionRoutedPayload) Validate() error {
	if p.DecisionID == "" {
		return errors.New("decision_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	switch p.DecisionKind {
	case "feeding", "oxygen_supply", "water_exchange", "pump_adjust", "monitoring":
	default:
		return fmt.Errorf("invalid decision_kind: %s (allowed: feeding|oxygen_supply|water_exchange|pump_adjust|monitoring)", p.DecisionKind)
	}
	if p.ProposingSource == "" {
		return errors.New("proposing_source is required")
	}
	if !ValidDecisionRoute(p.Route) {
		return fmt.Errorf("invalid route: %s", p.Route)
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	if !ValidAutonomousMode(p.AutonomousMode) {
		return fmt.Errorf("invalid autonomous_mode: %s", p.AutonomousMode)
	}
	if p.ProposedAt == "" {
		return errors.New("proposed_at is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ProposedAt); err != nil {
		return fmt.Errorf("proposed_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// TankDecisionResolvedPayload — pending_approval / pending_notify 결정의 운영자 처리 결과.
type TankDecisionResolvedPayload struct {
	DecisionID string `json:"decision_id"`
	TankID     string `json:"tank_id"`
	Resolution string `json:"resolution"` // approved | rejected | timed_out_executed | timed_out_skipped
	OperatorID string `json:"operator_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
	ResolvedAt string `json:"resolved_at"`
}

func (p TankDecisionResolvedPayload) Validate() error {
	if p.DecisionID == "" {
		return errors.New("decision_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	switch p.Resolution {
	case "approved", "rejected", "timed_out_executed", "timed_out_skipped":
	default:
		return fmt.Errorf("invalid resolution: %s (allowed: approved|rejected|timed_out_executed|timed_out_skipped)", p.Resolution)
	}
	if p.ResolvedAt == "" {
		return errors.New("resolved_at is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ResolvedAt); err != nil {
		return fmt.Errorf("resolved_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// AutonomousActionExecutedPayload — auto_executed 또는 approved 가 실제 control command 로 이어진 audit.
// 비-feeding kind 자율 실행은 범위 외 (C-3 narrow scope) — 실제 명령이 발행됐다는 기록.
type AutonomousActionExecutedPayload struct {
	DecisionID    string         `json:"decision_id"`
	TankID        string         `json:"tank_id"`
	DecisionKind  string         `json:"decision_kind"`
	CommandID     string         `json:"command_id"`     // control.CommandResult.CommandID
	CommandStatus string         `json:"command_status"` // control.CommandResult.Status
	ExecutedAt    string         `json:"executed_at"`
	Trigger       string         `json:"trigger"`            // auto_executed | approved
	Evidence      map[string]any `json:"evidence,omitempty"` // 결정 데이터 + 라우팅 이유
}

func (p AutonomousActionExecutedPayload) Validate() error {
	if p.DecisionID == "" {
		return errors.New("decision_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.DecisionKind == "" {
		return errors.New("decision_kind is required")
	}
	if p.CommandID == "" {
		return errors.New("command_id is required")
	}
	if p.ExecutedAt == "" {
		return errors.New("executed_at is required")
	}
	if p.Trigger != "auto_executed" && p.Trigger != "approved" {
		return fmt.Errorf("invalid trigger: %s (allowed: auto_executed|approved)", p.Trigger)
	}
	return nil
}

// AutonomousActionBlockedPayload — 안전 게이트 또는 미지원 kind 로 차단된 결정 기록.
type AutonomousActionBlockedPayload struct {
	DecisionID   string         `json:"decision_id"`
	TankID       string         `json:"tank_id"`
	DecisionKind string         `json:"decision_kind"`
	Reason       string         `json:"reason"` // safety_gate | control_not_wired | submission_failed
	Detail       string         `json:"detail,omitempty"`
	BlockedAt    string         `json:"blocked_at"`
	Evidence     map[string]any `json:"evidence,omitempty"`
}

func (p AutonomousActionBlockedPayload) Validate() error {
	if p.DecisionID == "" {
		return errors.New("decision_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.DecisionKind == "" {
		return errors.New("decision_kind is required")
	}
	switch p.Reason {
	case "safety_gate", "control_not_wired", "submission_failed":
	default:
		return fmt.Errorf("invalid reason: %s (allowed: safety_gate|control_not_wired|submission_failed)", p.Reason)
	}
	if p.BlockedAt == "" {
		return errors.New("blocked_at is required")
	}
	return nil
}

// AutonomousModeAutoDowngradePayload — 안전 트리거로 mode 가 자동 다운그레이드된 audit.
type AutonomousModeAutoDowngradePayload struct {
	TankID       string `json:"tank_id"`
	PreviousMode string `json:"previous_mode"`
	NewMode      string `json:"new_mode"`
	Reason       string `json:"reason"` // transition_detected | control_failure | safety_gate_breach
	Detail       string `json:"detail,omitempty"`
	DowngradedAt string `json:"downgraded_at"`
}

func (p AutonomousModeAutoDowngradePayload) Validate() error {
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.PreviousMode == "" {
		return errors.New("previous_mode is required")
	}
	if p.NewMode == "" {
		return errors.New("new_mode is required")
	}
	switch p.Reason {
	case "transition_detected", "control_failure", "safety_gate_breach":
	default:
		return fmt.Errorf("invalid reason: %s (allowed: transition_detected|control_failure|safety_gate_breach)", p.Reason)
	}
	if p.DowngradedAt == "" {
		return errors.New("downgraded_at is required")
	}
	return nil
}

// TankStockingRecordedPayload — 입식 anchor (D-1).
// 평균 체중 추정의 t=0. AI 자율 모드는 입식 시 자동 off (운영자가 다시 켜야 함).
type TankStockingRecordedPayload struct {
	StockingID            string  `json:"stocking_id"`
	TankID                string  `json:"tank_id"`
	LotNo                 string  `json:"lot_no"` // GDST 추적 lot — 입식 시점 immutable anchor
	Species               string  `json:"species"`
	GrowthStage           string  `json:"growth_stage"` // fry | juvenile | growout | broodstock
	InitialCount          int     `json:"initial_count"`
	InitialAvgWeightG     float64 `json:"initial_avg_weight_g"`
	InitialTotalBiomassKg float64 `json:"initial_total_biomass_kg,omitempty"`
	TargetHarvestWeightG  float64 `json:"target_harvest_weight_g,omitempty"`
	TargetHarvestDate     string  `json:"target_harvest_date,omitempty"`
	SourceHatchery        string  `json:"source_hatchery,omitempty"`
	SupplierID            string  `json:"supplier_id,omitempty"` // 거래처(부화장) partner_id
	StockedAt             string  `json:"stocked_at"`
	OperatorID            string  `json:"operator_id"`
}

func (p TankStockingRecordedPayload) Validate() error {
	if p.StockingID == "" {
		return errors.New("stocking_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.Species == "" {
		return errors.New("species is required")
	}
	if !ValidGrowthStage(p.GrowthStage) {
		return fmt.Errorf("invalid growth_stage: %s (allowed: fry|juvenile|growout|broodstock)", p.GrowthStage)
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if p.InitialCount <= 0 {
		return errors.New("initial_count must be > 0")
	}
	if p.InitialAvgWeightG <= 0 {
		return errors.New("initial_avg_weight_g must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.StockedAt); err != nil {
		return fmt.Errorf("stocked_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// TankHarvestRecordedPayload — 출하, lifecycle 마감.
type TankHarvestRecordedPayload struct {
	HarvestID      string  `json:"harvest_id"`
	StockingID     string  `json:"stocking_id"`      // 어떤 stocking 의 마감인지
	LotNo          string  `json:"lot_no,omitempty"` // GDST 추적 lot (출력 product lot = stocking lot)
	TankID         string  `json:"tank_id"`
	HarvestedCount int     `json:"harvested_count"`
	AvgWeightG     float64 `json:"avg_weight_g"`
	TotalBiomassKg float64 `json:"total_biomass_kg"`
	CycleFCR       float64 `json:"cycle_fcr,omitempty"` // 운영자 입력 또는 D-3 후 자동 산출
	HarvestedAt    string  `json:"harvested_at"`
	OperatorID     string  `json:"operator_id"`
	Notes          string  `json:"notes,omitempty"`
}

func (p TankHarvestRecordedPayload) Validate() error {
	if p.HarvestID == "" {
		return errors.New("harvest_id is required")
	}
	if p.StockingID == "" {
		return errors.New("stocking_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if p.HarvestedCount <= 0 {
		return errors.New("harvested_count must be > 0")
	}
	if p.AvgWeightG <= 0 {
		return errors.New("avg_weight_g must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.HarvestedAt); err != nil {
		return fmt.Errorf("harvested_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// TankSamplingRecordedPayload — 운영자 월 1회 샘플링 실측 (D-2).
// 평균 체중 모델의 anchor. D-4 (FCR auto-calibrator) 가 사용.
type TankSamplingRecordedPayload struct {
	SamplingID    string  `json:"sampling_id"`
	TankID        string  `json:"tank_id"`
	StockingID    string  `json:"stocking_id,omitempty"` // 활성 lineage 의 stocking_id (있으면)
	SampledCount  int     `json:"sampled_count"`         // 샘플로 잡은 마릿수
	AvgWeightG    float64 `json:"avg_weight_g"`
	StdWeightG    float64 `json:"std_weight_g,omitempty"`
	MinWeightG    float64 `json:"min_weight_g,omitempty"`
	MaxWeightG    float64 `json:"max_weight_g,omitempty"`
	HealthScore   int     `json:"health_score,omitempty"` // 0~10 (해부 결과)
	HealthNotes   string  `json:"health_notes,omitempty"`
	AbnormalCount int     `json:"abnormal_count,omitempty"` // 이상 개체 수
	SampledAt     string  `json:"sampled_at"`
	RecordedBy    string  `json:"recorded_by"`
}

func (p TankSamplingRecordedPayload) Validate() error {
	if p.SamplingID == "" {
		return errors.New("sampling_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.RecordedBy == "" {
		return errors.New("recorded_by is required")
	}
	t, err := time.Parse(time.RFC3339Nano, p.SampledAt)
	if err != nil {
		return fmt.Errorf("sampled_at must be RFC3339Nano: %w", err)
	}
	// 시계 오차 마진 1h 허용, 그 이상 미래는 거부
	if t.After(time.Now().UTC().Add(time.Hour)) {
		return errors.New("sampled_at is too far in the future (max +1h)")
	}
	if p.SampledCount <= 0 {
		return errors.New("sampled_count must be > 0")
	}
	if p.AvgWeightG <= 0 {
		return errors.New("avg_weight_g must be > 0")
	}
	if p.HealthScore != 0 && (p.HealthScore < 0 || p.HealthScore > 10) {
		return errors.New("health_score must be 0~10")
	}
	if p.MinWeightG > 0 && p.MinWeightG > p.AvgWeightG {
		return errors.New("min_weight_g must be <= avg_weight_g")
	}
	if p.MaxWeightG > 0 && p.MaxWeightG < p.AvgWeightG {
		return errors.New("max_weight_g must be >= avg_weight_g")
	}
	if p.AbnormalCount < 0 {
		return errors.New("abnormal_count must be >= 0")
	}
	return nil
}

// TankTreatmentRecordedPayload — 투약/약품 처리 CTE (GDST·ASC 식품안전 KDE).
type TankTreatmentRecordedPayload struct {
	TreatmentID     string  `json:"treatment_id"`
	TankID          string  `json:"tank_id"`
	StockingID      string  `json:"stocking_id,omitempty"`
	LotNo           string  `json:"lot_no,omitempty"`
	TreatmentType   string  `json:"treatment_type"` // antibiotic | vaccine | chemical | probiotic | anesthetic | other
	Substance       string  `json:"substance"`      // 약품/물질명
	Dose            float64 `json:"dose,omitempty"`
	DoseUnit        string  `json:"dose_unit,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	WithdrawalUntil string  `json:"withdrawal_until,omitempty"` // 휴약기간 종료(출하금지 해제) — RFC3339Nano
	ItemID          string  `json:"item_id,omitempty"`          // 재고 차감 대상 약품 품목
	ConsumedQty     float64 `json:"consumed_qty,omitempty"`     // 품목 단위 사용량 (재고 −)
	AdministeredAt  string  `json:"administered_at"`
	OperatorID      string  `json:"operator_id"`
	Notes           string  `json:"notes,omitempty"`
}

func (p TankTreatmentRecordedPayload) Validate() error {
	if p.TreatmentID == "" {
		return errors.New("treatment_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if !ValidTreatmentType(p.TreatmentType) {
		return fmt.Errorf("invalid treatment_type: %s (allowed: antibiotic|vaccine|chemical|probiotic|anesthetic|other)", p.TreatmentType)
	}
	if p.Substance == "" {
		return errors.New("substance is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.AdministeredAt); err != nil {
		return fmt.Errorf("administered_at must be RFC3339Nano: %w", err)
	}
	if p.WithdrawalUntil != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.WithdrawalUntil); err != nil {
			return fmt.Errorf("withdrawal_until must be RFC3339Nano: %w", err)
		}
	}
	return nil
}

// TankMortalityRecordedPayload — 폐사 기록 CTE (생산 정산 + 복지 KDE).
type TankMortalityRecordedPayload struct {
	MortalityID    string `json:"mortality_id"`
	TankID         string `json:"tank_id"`
	StockingID     string `json:"stocking_id,omitempty"`
	LotNo          string `json:"lot_no,omitempty"`
	DeadCount      int    `json:"dead_count"`
	EstimatedCause string `json:"estimated_cause,omitempty"`
	ObservedAt     string `json:"observed_at"`
	OperatorID     string `json:"operator_id"`
	Notes          string `json:"notes,omitempty"`
}

func (p TankMortalityRecordedPayload) Validate() error {
	if p.MortalityID == "" {
		return errors.New("mortality_id is required")
	}
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if p.DeadCount <= 0 {
		return errors.New("dead_count must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ObservedAt); err != nil {
		return fmt.Errorf("observed_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// TankTransferRecordedPayload — 탱크간 이동/선별(split·merge) + 외부 판매/출하(sale) CTE.
// 수조내 이동: EPCIS TransformationEvent (input lot → output lot). sale: 첫 거래(shipping) CTE.
type TankTransferRecordedPayload struct {
	TransferID     string `json:"transfer_id"`
	TransferType   string `json:"transfer_type"` // move | split | merge | sale
	FromTankID     string `json:"from_tank_id"`
	FromStockingID string `json:"from_stocking_id,omitempty"`
	FromLotNo      string `json:"from_lot_no,omitempty"`
	ToTankID       string `json:"to_tank_id,omitempty"` // 수조내 이동 시 필수, sale 시 비움
	ToStockingID   string `json:"to_stocking_id,omitempty"`
	ToLotNo        string `json:"to_lot_no,omitempty"`
	// sale (외부 판매/출하) 전용 — 도착 농장/거래처 + 운송.
	DestinationName string  `json:"destination_name,omitempty"` // 이동 농장/거래처 사이트명
	VehicleInfo     string  `json:"vehicle_info,omitempty"`     // 이동 차량 정보
	MovedCount      int     `json:"moved_count"`
	AvgWeightG      float64 `json:"avg_weight_g,omitempty"`
	TotalBiomassKg  float64 `json:"total_biomass_kg,omitempty"`
	TransferredAt   string  `json:"transferred_at"`
	OperatorID      string  `json:"operator_id"`
	Notes           string  `json:"notes,omitempty"`
}

func (p TankTransferRecordedPayload) Validate() error {
	if p.TransferID == "" {
		return errors.New("transfer_id is required")
	}
	if !ValidTransferType(p.TransferType) {
		return fmt.Errorf("invalid transfer_type: %s (allowed: move|split|merge|sale)", p.TransferType)
	}
	if p.FromTankID == "" {
		return errors.New("from_tank_id is required")
	}
	if p.TransferType == "sale" {
		// 외부 판매: 도착처 명시, 수조 lineage 없음.
		if p.DestinationName == "" {
			return errors.New("destination_name is required for sale")
		}
	} else {
		// 수조내 이동: 도착 수조 + 새 lineage 필요.
		if p.ToTankID == "" {
			return errors.New("to_tank_id is required")
		}
		if p.ToStockingID == "" {
			return errors.New("to_stocking_id is required")
		}
	}
	if p.OperatorID == "" {
		return errors.New("operator_id is required")
	}
	if p.MovedCount <= 0 {
		return errors.New("moved_count must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.TransferredAt); err != nil {
		return fmt.Errorf("transferred_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// TankDocumentAttachedPayload — CTE 증빙 서류 첨부 (GDST provenance).
type TankDocumentAttachedPayload struct {
	DocumentID  string `json:"document_id"`
	TankID      string `json:"tank_id,omitempty"`
	LotNo       string `json:"lot_no,omitempty"`
	CTEType     string `json:"cte_type"` // stocking|feeding|treatment|mortality|transfer|sale|harvest|other
	DocType     string `json:"doc_type"` // prescription|diagnosis_certificate|transaction_statement|...
	EventRef    string `json:"event_ref,omitempty"`
	SubjectType string `json:"subject_type,omitempty"` // tank | inventory_purchase | partner
	SubjectID   string `json:"subject_id,omitempty"`
	Filename    string `json:"filename"`
	MimeType    string `json:"mime_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	StoredPath  string `json:"stored_path"`
	Notes       string `json:"notes,omitempty"`
	UploadedBy  string `json:"uploaded_by"`
	UploadedAt  string `json:"uploaded_at"`
}

func (p TankDocumentAttachedPayload) Validate() error {
	if p.DocumentID == "" {
		return errors.New("document_id is required")
	}
	// tank 서류는 tank_id, 그 외 subject 서류는 subject_id 필요.
	if p.SubjectType == "" || p.SubjectType == "tank" {
		if p.TankID == "" {
			return errors.New("tank_id is required")
		}
	} else if p.SubjectID == "" {
		return errors.New("subject_id is required")
	}
	if !ValidCTEType(p.CTEType) {
		return fmt.Errorf("invalid cte_type: %s", p.CTEType)
	}
	if !ValidDocType(p.DocType) {
		return fmt.Errorf("invalid doc_type: %s", p.DocType)
	}
	if p.Filename == "" {
		return errors.New("filename is required")
	}
	if p.SHA256 == "" {
		return errors.New("sha256 is required")
	}
	if p.StoredPath == "" {
		return errors.New("stored_path is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.UploadedAt); err != nil {
		return fmt.Errorf("uploaded_at must be RFC3339Nano: %w", err)
	}
	return nil
}

func ValidCTEType(v string) bool {
	switch v {
	case "stocking", "feeding", "treatment", "mortality", "transfer", "sale", "harvest", "other":
		return true
	}
	return false
}

func ValidDocType(v string) bool {
	switch v {
	case "broodstock_info", "producer_license", "transaction_statement", "tax_invoice",
		"feed_purchase_statement", "product_spec", "certification",
		"prescription", "drug_purchase_statement",
		"diagnosis_certificate", "vehicle_doc", "other":
		return true
	}
	return false
}

func ValidInventoryCategory(v string) bool {
	switch v {
	case "feed", "drug", "material":
		return true
	}
	return false
}

func ValidPartnerType(v string) bool {
	switch v {
	case "hatchery", "feed_supplier", "drug_supplier", "buyer", "other":
		return true
	}
	return false
}

// SiteStockingAllocation — 입식 배치의 수조별 분배 한 건.
type SiteStockingAllocation struct {
	TankID     string  `json:"tank_id"`
	Count      int     `json:"count"`
	AvgWeightG float64 `json:"avg_weight_g"`
	LotNo      string  `json:"lot_no,omitempty"`
	StockingID string  `json:"stocking_id,omitempty"` // 파생된 tank stocking_id
}

// SiteStockingRecordedPayload — 사업자(site) 입식 거래(배치). 공급처에서 받아 여러 수조에 분배.
type SiteStockingRecordedPayload struct {
	SiteStockingID string                   `json:"site_stocking_id"`
	SiteID         string                   `json:"site_id"`
	SupplierID     string                   `json:"supplier_id,omitempty"`
	SupplierName   string                   `json:"supplier_name,omitempty"`
	Species        string                   `json:"species"`
	GrowthStage    string                   `json:"growth_stage"`
	SourceHatchery string                   `json:"source_hatchery,omitempty"`
	BatchLotNo     string                   `json:"batch_lot_no,omitempty"`
	TotalCount     int                      `json:"total_count"`
	Allocations    []SiteStockingAllocation `json:"allocations"`
	StockedAt      string                   `json:"stocked_at"`
	OperatorID     string                   `json:"operator_id"`
	Notes          string                   `json:"notes,omitempty"`
}

func (p SiteStockingRecordedPayload) Validate() error {
	if p.SiteStockingID == "" {
		return errors.New("site_stocking_id is required")
	}
	if p.SiteID == "" {
		return errors.New("site_id is required")
	}
	if p.Species == "" {
		return errors.New("species is required")
	}
	if !ValidGrowthStage(p.GrowthStage) {
		return fmt.Errorf("invalid growth_stage: %s", p.GrowthStage)
	}
	if len(p.Allocations) == 0 {
		return errors.New("allocations must not be empty")
	}
	for i, a := range p.Allocations {
		if a.TankID == "" {
			return fmt.Errorf("allocations[%d].tank_id is required", i)
		}
		if a.Count <= 0 {
			return fmt.Errorf("allocations[%d].count must be > 0", i)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, p.StockedAt); err != nil {
		return fmt.Errorf("stocked_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// SiteHarvestLine — 출하 건의 수조별 출하 한 건 (부분 출하 가능).
type SiteHarvestLine struct {
	TankID     string  `json:"tank_id"`
	LotNo      string  `json:"lot_no,omitempty"`
	Count      int     `json:"count"`
	AvgWeightG float64 `json:"avg_weight_g,omitempty"`
	FullClose  bool    `json:"full_close"`           // true 면 수조 lifecycle 마감
	HarvestID  string  `json:"harvest_id,omitempty"` // 파생된 tank harvest_id (full_close 시)
}

// SiteHarvestRecordedPayload — 사업자(site) 출하 거래(건). 한 거래처로 여러 수조/부분 출하.
type SiteHarvestRecordedPayload struct {
	SiteHarvestID string            `json:"site_harvest_id"`
	SiteID        string            `json:"site_id"`
	BuyerID       string            `json:"buyer_id,omitempty"`
	BuyerName     string            `json:"buyer_name,omitempty"`
	TotalCount    int               `json:"total_count"`
	Lines         []SiteHarvestLine `json:"lines"`
	VehicleInfo   string            `json:"vehicle_info,omitempty"`
	HarvestedAt   string            `json:"harvested_at"`
	OperatorID    string            `json:"operator_id"`
	Notes         string            `json:"notes,omitempty"`
}

func (p SiteHarvestRecordedPayload) Validate() error {
	if p.SiteHarvestID == "" {
		return errors.New("site_harvest_id is required")
	}
	if p.SiteID == "" {
		return errors.New("site_id is required")
	}
	if len(p.Lines) == 0 {
		return errors.New("lines must not be empty")
	}
	for i, l := range p.Lines {
		if l.TankID == "" {
			return fmt.Errorf("lines[%d].tank_id is required", i)
		}
		if l.Count <= 0 {
			return fmt.Errorf("lines[%d].count must be > 0", i)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, p.HarvestedAt); err != nil {
		return fmt.Errorf("harvested_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// InventoryPurchaseRecordedPayload — 구매(입고). 재고 +qty.
type InventoryPurchaseRecordedPayload struct {
	PurchaseID  string  `json:"purchase_id"`
	ItemID      string  `json:"item_id"`
	Category    string  `json:"category"` // feed | drug | material
	Name        string  `json:"name"`
	Unit        string  `json:"unit"`
	Qty         float64 `json:"qty"`
	UnitPrice   float64 `json:"unit_price,omitempty"`
	TotalPrice  float64 `json:"total_price,omitempty"`
	Supplier    string  `json:"supplier,omitempty"`
	Lot         string  `json:"lot,omitempty"`
	PurchasedAt string  `json:"purchased_at"`
	OperatorID  string  `json:"operator_id"`
	Notes       string  `json:"notes,omitempty"`
}

func (p InventoryPurchaseRecordedPayload) Validate() error {
	if p.PurchaseID == "" {
		return errors.New("purchase_id is required")
	}
	if p.ItemID == "" {
		return errors.New("item_id is required")
	}
	if !ValidInventoryCategory(p.Category) {
		return fmt.Errorf("invalid category: %s (allowed: feed|drug|material)", p.Category)
	}
	if p.Qty <= 0 {
		return errors.New("qty must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.PurchasedAt); err != nil {
		return fmt.Errorf("purchased_at must be RFC3339Nano: %w", err)
	}
	return nil
}

// InventoryConsumptionRecordedPayload — 사용(출고). 재고 −qty.
type InventoryConsumptionRecordedPayload struct {
	ConsumptionID string  `json:"consumption_id"`
	ItemID        string  `json:"item_id"`
	Category      string  `json:"category,omitempty"`
	Qty           float64 `json:"qty"`
	Unit          string  `json:"unit,omitempty"`
	Reason        string  `json:"reason"` // feeding | treatment | material_use | adjustment
	RefEvent      string  `json:"ref_event,omitempty"`
	TankID        string  `json:"tank_id,omitempty"`
	ConsumedAt    string  `json:"consumed_at"`
	OperatorID    string  `json:"operator_id"`
	Notes         string  `json:"notes,omitempty"`
}

func (p InventoryConsumptionRecordedPayload) Validate() error {
	if p.ConsumptionID == "" {
		return errors.New("consumption_id is required")
	}
	if p.ItemID == "" {
		return errors.New("item_id is required")
	}
	if p.Reason == "" {
		return errors.New("reason is required")
	}
	if p.Qty <= 0 {
		return errors.New("qty must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.ConsumedAt); err != nil {
		return fmt.Errorf("consumed_at must be RFC3339Nano: %w", err)
	}
	return nil
}

func ValidTreatmentType(v string) bool {
	switch v {
	case "antibiotic", "vaccine", "chemical", "probiotic", "anesthetic", "other",
		"sex_reversal", "disinfection": // 종묘장: MT 성전환·소독
		return true
	}
	return false
}

func ValidTransferType(v string) bool {
	switch v {
	case "move", "split", "merge", "sale":
		return true
	}
	return false
}

func ValidGrowthStage(v string) bool {
	switch v {
	case "fry", "juvenile", "growout", "broodstock":
		return true
	}
	return false
}

func ValidDecisionRoute(v string) bool {
	switch v {
	case "auto_executed", "pending_notify", "pending_approval", "advisory_only", "rejected":
		return true
	}
	return false
}

func ValidAutonomousMode(v string) bool {
	switch v {
	case "off", "observation", "partial", "full":
		return true
	}
	return false
}

func ValidFeedingSource(v string) bool {
	switch v {
	case FeedingSourceManual,
		FeedingSourceFeederCommand,
		FeedingSourceRecommendation:
		return true
	default:
		return false
	}
}

func ValidMetric(v string) bool {
	switch v {
	case MetricWaterTemperature,
		MetricDissolvedOxygen,
		MetricPH,
		MetricSalinity,
		MetricNitrite,
		MetricNitrate,
		MetricCarbonDioxide,
		MetricTotalSuspendedSolids,
		MetricUnionizedAmmonia,
		MetricTurbidity,
		MetricORP,
		MetricAmmonia,
		MetricWaterLevel,
		MetricFlowRate,
		MetricPumpPressure,
		MetricFeedWeight,
		MetricLightIntensity,
		MetricUnknown:
		return true
	}
	return false
}

func ValidQuality(v string) bool {
	switch v {
	case QualityOK, QualitySuspect, QualityStale, QualityMissing, QualityError:
		return true
	}
	return false
}

func ValidDeviceStatus(v string) bool {
	switch v {
	case DeviceStatusOnline, DeviceStatusDegraded, DeviceStatusDown, DeviceStatusDisabled, DeviceStatusUnknown:
		return true
	}
	return false
}

func ValidSeverity(v string) bool {
	switch v {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	}
	return false
}

func ValidAlertStatus(v string) bool {
	switch v {
	case AlertStatusOpen, AlertStatusAcknowledged, AlertStatusResolved, AlertStatusClosed:
		return true
	}
	return false
}

// TankFCRCalibratedPayload — sampling 시점 보정 audit (D-4).
// 운영자 수동 입력 X — sampling 등록 시 자동 산출.
type TankFCRCalibratedPayload struct {
	TankID          string  `json:"tank_id"`
	StockingID      string  `json:"stocking_id"`
	SamplingID      string  `json:"sampling_id"`
	DefaultFCR      float64 `json:"default_fcr"`
	ObservedFCR     float64 `json:"observed_fcr"`
	CalibratedFCR   float64 `json:"calibrated_fcr"`
	DeviationPct    float64 `json:"deviation_pct"`
	CumulativeFeedG float64 `json:"cumulative_feed_g"`
	DeltaBiomassG   float64 `json:"delta_biomass_g"`
	CalibratedAt    string  `json:"calibrated_at"`
	Notes           string  `json:"notes,omitempty"`
}

func (p TankFCRCalibratedPayload) Validate() error {
	if p.TankID == "" {
		return errors.New("tank_id is required")
	}
	if p.StockingID == "" {
		return errors.New("stocking_id is required")
	}
	if p.SamplingID == "" {
		return errors.New("sampling_id is required")
	}
	if p.CalibratedAt == "" {
		return errors.New("calibrated_at is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.CalibratedAt); err != nil {
		return fmt.Errorf("calibrated_at must be RFC3339Nano: %w", err)
	}
	if p.DefaultFCR <= 0 {
		return errors.New("default_fcr must be > 0")
	}
	if p.ObservedFCR <= 0 {
		return errors.New("observed_fcr must be > 0")
	}
	if p.CalibratedFCR <= 0 {
		return errors.New("calibrated_fcr must be > 0")
	}
	if p.CumulativeFeedG <= 0 {
		return errors.New("cumulative_feed_g must be > 0")
	}
	if p.DeltaBiomassG <= 0 {
		return errors.New("delta_biomass_g must be > 0")
	}
	return nil
}
