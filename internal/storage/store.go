package storage

import (
	"context"
	"time"

	"bluei.kr/edge/internal/actuator"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/controller"
	"bluei.kr/edge/internal/farm"
	"bluei.kr/edge/internal/sensor"
	"bluei.kr/edge/internal/site"
	"bluei.kr/edge/internal/species"
	"bluei.kr/edge/internal/wtg"
)

// Event is the canonical event envelope persisted in the events table.
type Event struct {
	Sequence      int64
	EventID       string
	EventType     string
	SchemaVersion string
	SiteID        string
	EdgeID        string
	RecordedAt    time.Time
	SourceModule  string
	SourceAdapter string
	SourceDevice  string
	PayloadJSON   string
	EventJSON     string
	CorrelationID string
	CausationID   string
	SyncedAt      *time.Time
}

// ArchivableEvent is the minimal projection of an event needed by retention
// archiving — just enough to write the backup line and bound the delete.
type ArchivableEvent struct {
	Sequence   int64
	RecordedAt time.Time
	EventJSON  string
}

// EventFilter restricts event queries.
type EventFilter struct {
	EventType string
	DeviceID  string
	Since     *time.Time
	Limit     int
	AfterSeq  int64
}

// LatestReadingFilter restricts latest sensor reading queries.
type LatestReadingFilter struct {
	TankID   string
	SensorID string
	Metric   string
	Limit    int
	MaxScan  int
}

// LatestSensorReading is the newest reading for a metric/sensor scope.
type LatestSensorReading struct {
	Sequence   int64          `json:"sequence"`
	EventID    string         `json:"event_id"`
	SensorID   string         `json:"sensor_id"`
	DeviceID   string         `json:"device_id"`
	Metric     string         `json:"metric"`
	Value      *float64       `json:"value"`
	Unit       string         `json:"unit"`
	Quality    string         `json:"quality"`
	ObservedAt string         `json:"observed_at"`
	Location   map[string]any `json:"location"`
	Payload    map[string]any `json:"payload"`
}

// SensorDailyAgg is one (local-day, sensor, metric) rollup of raw readings —
// the min/max/avg kept when raw sensor.reading events are pruned.
type SensorDailyAgg struct {
	Date     string // 로컬 달력일 YYYY-MM-DD
	SensorID string
	DeviceID string
	Metric   string
	Unit     string
	Count    int64
	Min      float64
	Max      float64
	Avg      float64
}

// CurrentTankEnvironmentReading is a row from current_tank_environment.
type CurrentTankEnvironmentReading struct {
	TankID      string         `json:"tank_id"`
	Metric      string         `json:"metric"`
	Value       *float64       `json:"value"`
	Unit        string         `json:"unit"`
	Quality     string         `json:"quality"`
	SensorID    string         `json:"sensor_id"`
	DeviceID    string         `json:"device_id"`
	LastEventID string         `json:"last_event_id"`
	ObservedAt  string         `json:"observed_at"`
	UpdatedAt   string         `json:"updated_at"`
	Payload     map[string]any `json:"payload"`
}

// CameraProfile is the local registry entry for a field camera.
type CameraProfile struct {
	CameraID          string         `json:"camera_id"`
	TankID            string         `json:"tank_id,omitempty"`
	DisplayName       string         `json:"display_name"`
	Vendor            string         `json:"vendor,omitempty"`
	Host              string         `json:"host,omitempty"`
	RTSPPort          int            `json:"rtsp_port,omitempty"`
	HTTPPort          int            `json:"http_port,omitempty"`
	Username          string         `json:"username,omitempty"`
	PasswordSecretRef string         `json:"password_secret_ref,omitempty"`
	Position          string         `json:"position,omitempty"`
	Purpose           []string       `json:"purpose,omitempty"`
	StreamProfiles    map[string]any `json:"stream_profiles,omitempty"`
	ClipPolicy        map[string]any `json:"clip_policy,omitempty"`
	Status            string         `json:"status"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	UpdatedAt         string         `json:"updated_at,omitempty"`
	// C-11 camera model library — 인스턴스 ↔ 모델 link + 설치 정보.
	ModelID          string   `json:"model_id,omitempty"`
	MountingHeightM  *float64 `json:"mounting_height_m,omitempty"`
	UnderwaterDepthM *float64 `json:"underwater_depth_m,omitempty"`
	// C-12 — 카메라 메타 정정. 기존 Position 의 의미 혼란 해소.
	// MountLocation : 어디 위에 설치 (feeder_zone/water_intake/water_outlet/tank_center/tank_side/other)
	// ViewAngle     : 어떤 구도로 보는가 — AI 알고리즘 선택의 핵심.
	//                  top_down=마릿수, oblique_top=급이반응, side_horizontal=행동,
	//                  underwater_top=어체형상, underwater_side=크기측정(듀얼)
	// HeightFromWaterM : 수면 기준 (양수=수면 위, 음수=수중)
	// TiltDeg          : 0=수평, 90=수직 아래
	MountLocation    string   `json:"mount_location,omitempty"`
	ViewAngle        string   `json:"view_angle,omitempty"`
	HeightFromWaterM *float64 `json:"height_from_water_m,omitempty"`
	TiltDeg          *float64 `json:"tilt_deg,omitempty"`
}

// CameraModel is the camera model library entry — vendor/lens/특성.
// camera_profiles.model_id 가 이 row 를 참조.
// LensType=dual 일 때만 BaselineMM / StereoCalibrationJSON 의미.
type CameraModel struct {
	ModelID               string   `json:"model_id"`
	Vendor                string   `json:"vendor"`
	ProductCode           string   `json:"product_code"`
	DisplayName           string   `json:"display_name"`
	LensType              string   `json:"lens_type"`
	BaselineMM            *float64 `json:"baseline_mm,omitempty"`
	StereoCalibrationJSON string   `json:"stereo_calibration_json,omitempty"`
	ResolutionW           *int     `json:"resolution_w,omitempty"`
	ResolutionH           *int     `json:"resolution_h,omitempty"`
	FOVDeg                *float64 `json:"fov_deg,omitempty"`
	FPS                   *int     `json:"fps,omitempty"`
	NightMode             bool     `json:"night_mode"`
	Protocols             []string `json:"protocols,omitempty"`
	Notes                 string   `json:"notes,omitempty"`
	CreatedAt             string   `json:"created_at,omitempty"`
	UpdatedAt             string   `json:"updated_at,omitempty"`
}

// SensorModel is the sensor model library entry (C-13a).
// sensors.model_id references this row.
// CHECK constraints in migration 024 enforce measurement_type / protocol / wet_dry enums.
type SensorModel struct {
	ModelID                 string   `json:"model_id"`
	Vendor                  string   `json:"vendor"`
	ProductCode             string   `json:"product_code"`
	DisplayName             string   `json:"display_name"`
	MeasurementType         string   `json:"measurement_type"`
	Unit                    string   `json:"unit"`
	RangeMin                *float64 `json:"range_min,omitempty"`
	RangeMax                *float64 `json:"range_max,omitempty"`
	AccuracyValue           *float64 `json:"accuracy_value,omitempty"`
	AccuracyUnit            string   `json:"accuracy_unit,omitempty"`
	ResponseTimeS           *float64 `json:"response_time_s,omitempty"`
	Protocol                string   `json:"protocol,omitempty"`
	CalibrationIntervalDays *int     `json:"calibration_interval_days,omitempty"`
	WetDry                  string   `json:"wet_dry,omitempty"`
	Notes                   string   `json:"notes,omitempty"`
	CreatedAt               string   `json:"created_at,omitempty"`
	UpdatedAt               string   `json:"updated_at,omitempty"`
}

// CurrentCameraStatus is the latest projected health status for one camera.
type CurrentCameraStatus struct {
	CameraID       string         `json:"camera_id"`
	TankID         string         `json:"tank_id,omitempty"`
	Status         string         `json:"status"`
	IngestFPS      float64        `json:"ingest_fps"`
	LastEventID    string         `json:"last_event_id"`
	LastFrameAt    string         `json:"last_frame_at,omitempty"`
	ReconnectCount int            `json:"reconnect_count"`
	DroppedFrames  int            `json:"dropped_frames"`
	UpdatedAt      string         `json:"updated_at"`
	Details        map[string]any `json:"details"`
}

// OpenAlert is a row from open_alerts.
type OpenAlert struct {
	AlertID        string
	AlertDedupeKey string
	AlertType      string
	Severity       string
	SubjectKind    string
	SubjectID      string
	RuleID         string
	Status         string
	RaisedAt       time.Time
	UpdatedAt      time.Time
	PayloadJSON    string
}

// ControlCommand is a row from control_commands.
type ControlCommand struct {
	CommandID      string
	IdempotencyKey string
	TargetDeviceID string
	CommandType    string
	Status         string
	RequestedAt    time.Time
	ExpiresAt      time.Time
	LastEventID    string
	PayloadJSON    string
}

// WaterQualityBucketProjection is a derived two-minute water-quality bucket.
// It is replayable from events and must not be treated as source of truth.
type WaterQualityBucketProjection struct {
	TankID           string
	BucketStart      time.Time
	BucketSec        int
	TemperatureCAvg  *float64
	PHAvg            *float64
	DOMgLAvg         *float64
	Quality          string
	SampleCount      int
	SuspectCount     int
	SourceReadingIDs string
	UpdatedAt        time.Time
}

// FeedingImpactAnalysisProjection is a derived feeding water-quality analysis.
// It is replayable from feeding and water-quality events/projections.
type FeedingImpactAnalysisProjection struct {
	AnalysisID      string
	FeedingID       string
	TankID          string
	FeedAmountG     float64
	FedAt           time.Time
	DOBaselineMgL   *float64
	DOMinPostMgL    *float64
	DODropMgL       *float64
	DORecoveryMin   *int
	PHDelta         *float64
	TempDeltaC      *float64
	Quality         string
	ReasonCodesJSON string
	UpdatedAt       time.Time
}

// SyncBatch is a row from sync_batches.
type SyncBatch struct {
	BatchID        string
	FromSequence   int64
	ToSequence     int64
	Status         string
	Attempt        int
	NextRetryAt    *time.Time
	CreatedAt      time.Time
	SentAt         *time.Time
	AcknowledgedAt *time.Time
	RemoteAckID    string
	ErrorJSON      string
}

// TankDecisionPolicy — Cage/Tank별 pending_notify 자동 실행 정책 projection (Phase 4 C-4).
// 행 없으면 시스템 기본값(Config.DecisionPolicy) 으로 fallback.
type TankDecisionPolicy struct {
	TankID             string    `json:"tank_id"`
	AutoExecuteEnabled bool      `json:"auto_execute_enabled"`
	GraceMinutes       int       `json:"grace_minutes"`
	UpdatedAt          time.Time `json:"updated_at"`
	UpdatedBy          string    `json:"updated_by"`
}

// TankAutonomousMode — Cage/Tank별 자율 운영 모드 projection (Phase 4 C-1).
// 실제 제어 명령 없음 — 순수 상태 + audit. AI 가 읽어 자율 결정 여부를 판단 (C-2/C-3).
type TankAutonomousMode struct {
	TankID    string    `json:"tank_id"`
	Mode      string    `json:"mode"`
	Reason    string    `json:"reason,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
	ChangedBy string    `json:"changed_by"`
}

// TankLifecycle — Cage/Tank 입식/출하 lifecycle projection (D-1).
// active_stocking_id 로 현재 사이클을 추적. 출하 후 status=harvested 로 변경.
type TankLifecycle struct {
	TankID               string
	ActiveStockingID     string
	Species              string
	GrowthStage          string
	InitialCount         int
	InitialAvgWeightG    float64
	TargetHarvestWeightG *float64 // nullable
	TargetHarvestDate    string   // optional
	SourceHatchery       string
	StockedAt            time.Time
	Status               string // active | harvested | transferred
	UpdatedAt            time.Time
	LotNo                string // GDST 추적 lot (입식 시점 immutable)
	ParentLotNo          string // transfer/grading 파생 시 상위 lot
}

// TankDocument — GDST 증빙 서류 메타데이터 projection. blob 은 data_dir/documents/ 에.
type TankDocument struct {
	DocumentID  string
	TankID      string
	LotNo       string
	CTEType     string
	DocType     string
	EventRef    string
	Filename    string
	MimeType    string
	SizeBytes   int64
	SHA256      string
	StoredPath  string // data_dir 기준 상대 경로
	Notes       string
	UploadedBy  string
	UploadedAt  time.Time
	SubjectType string // tank | inventory_purchase
	SubjectID   string
}

// InventoryItem — 재고 품목 + 현재고. 구매/사용 이벤트로 on_hand 증감.
type InventoryItem struct {
	ItemID       string
	Category     string // feed | drug | material
	Name         string
	Unit         string
	OnHandQty    float64
	Spec         string
	Supplier     string
	ReorderLevel *float64 // nullable
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Partner — 거래처(공급처/구매처) 마스터. site 소속.
type Partner struct {
	PartnerID   string
	PartnerType string // hatchery | feed_supplier | drug_supplier | buyer | other
	Name        string
	BusinessNo  string
	LicenseNo   string
	Contact     string
	Address     string
	GLN         string
	Notes       string
	SiteID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SiteStocking — 사업자(site) 입식 거래(배치). 수조 분배는 AllocationsJSON.
type SiteStocking struct {
	SiteStockingID  string
	SiteID          string
	SupplierID      string
	SupplierName    string
	Species         string
	GrowthStage     string
	SourceHatchery  string
	BatchLotNo      string
	TotalCount      int
	TotalAvgWeightG float64
	TotalBiomassKg  float64
	AllocationsJSON string // [{tank_id,count,avg_weight_g,lot_no,stocking_id}]
	StockedAt       time.Time
	OperatorID      string
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SiteHarvest — 사업자(site) 출하 거래(건). 수조 line item 은 LinesJSON.
type SiteHarvest struct {
	SiteHarvestID  string
	SiteID         string
	BuyerID        string
	BuyerName      string
	TotalCount     int
	TotalBiomassKg float64
	LinesJSON      string // [{tank_id,lot_no,count,avg_weight_g,full_close,harvest_id}]
	VehicleInfo    string
	HarvestedAt    time.Time
	OperatorID     string
	Notes          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TankSampling — Cage/Tank sampling projection (D-2). Cage/Tank당 최신 1행.
// history 는 events 테이블의 tank.sampling.recorded 이벤트로 재현.
type TankSampling struct {
	TankID           string
	LatestSamplingID string
	StockingID       string // 비어있을 수 있음
	SampledCount     int
	AvgWeightG       float64
	StdWeightG       *float64 // optional
	MinWeightG       *float64
	MaxWeightG       *float64
	HealthScore      *int
	HealthNotes      string
	AbnormalCount    *int
	SampledAt        time.Time
	RecordedBy       string
	UpdatedAt        time.Time
}

// TankWeightSnapshot — 일일 추정 체중 스냅샷 (D-5).
// tank_weight_history 테이블에서 (tank_id, snapshot_date) PK 로 UPSERT.
type TankWeightSnapshot struct {
	TankID              string
	SnapshotDate        string // YYYY-MM-DD
	EstimatedAvgWeightG float64
	AnchorWeightG       float64
	AnchorSource        string
	DaysSinceAnchor     int
	ExpectedFCR         float64
	FCRSource           string
	CumulativeFeedG     float64
	Quality             string
	SnapshotAt          time.Time
}

// TankFCRCalibration — Cage/Tank별 FCR 자동 보정 projection (D-4).
// sampling 시점에 산출. active stocking 기간 동안만 유효 (stocking_id 로 검증).
type TankFCRCalibration struct {
	TankID          string
	StockingID      string
	SamplingID      string
	DefaultFCR      float64
	ObservedFCR     float64
	CalibratedFCR   float64
	DeviationPct    float64
	CumulativeFeedG float64
	DeltaBiomassG   float64
	CalibratedAt    time.Time
}

// GroupProfile — Group (양식동/순환시스템) 프로필. config.GroupProfile alias.
type GroupProfile = config.GroupProfile

// PredictiveBlock is a row in predictive_blocks — audit of cycles blocked by C-3p gate.
type PredictiveBlock struct {
	BlockID        string
	WTGID          string
	TankID         string
	CycleID        string
	Reason         string
	PredictedValue float64
	ThresholdValue float64
	ForecastAt     time.Time
	BlockedAt      time.Time
}

// ArbiterDecision is an audit record of each arbiter decision (Phase 5).
type ArbiterDecision struct {
	DecisionID       string
	TankID           string
	Source           string
	Priority         string
	Accepted         bool
	RejectionReason  string
	ExistingCycleID  string
	ResultingCycleID string
	IntentID         string
	SubmittedAt      time.Time
	DecidedAt        time.Time
	PreemptedCycleID string // 선점된 사이클 ID (Phase 6, 허용 시 선점 발생한 경우에만 설정)
}

// OperatorIntent is an operator memo explaining a feeding decision (Phase 5).
type OperatorIntent struct {
	IntentID          string
	OperatorID        string
	TankID            string
	RelatedCycleID    string
	RelatedDecisionID string
	IntentType        string // 'feed_now' | 'skip_cycle' | 'change_pattern' | 'general_note'
	Reason            string
	ContextJSON       string
	RecordedAt        time.Time
}

// Store is the interface for all local storage operations.
type Store interface {
	// Lifecycle
	Close() error
	Ping(ctx context.Context) error

	// Events
	AppendEvent(ctx context.Context, e *Event) (int64, error)
	QueryEvents(ctx context.Context, f EventFilter) ([]*Event, error)
	ListUnsyncedUnbatchedEvents(ctx context.Context, limit int) ([]*Event, error)
	LatestSensorReadings(ctx context.Context, f LatestReadingFilter) ([]*LatestSensorReading, error)
	UnsyncedCount(ctx context.Context) (int64, error)
	OldestUnsyncedAge(ctx context.Context) (time.Duration, error)
	MarkSynced(ctx context.Context, eventIDs []string) error
	// Retention — 만료 telemetry 백업+제거. SelectEventsOlderThan 은 sequence ASC 로
	// 배치 반환, DeleteEventsOlderThanUpToSeq 는 그 배치(최대 sequence)까지만 삭제한다.
	SelectEventsOlderThan(ctx context.Context, eventType string, cutoff time.Time, limit int) ([]ArchivableEvent, error)
	DeleteEventsOlderThanUpToSeq(ctx context.Context, eventType string, cutoff time.Time, maxSeq int64) (int64, error)
	// 센서 일별 집계 — recorded_at < before 인 sensor.reading.recorded 를 (날짜, sensor_id, metric)
	// 별 min/max/avg/count 로 묶어 반환. 날짜는 tzOffsetSeconds 만큼 보정한 로컬 달력일.
	AggregateSensorReadingsDailyBefore(ctx context.Context, before time.Time, tzOffsetSeconds int) ([]SensorDailyAgg, error)
	// 주어진 event_id 들 중 이미 존재하는 것의 집합 (요약 멱등 처리용).
	FilterExistingEventIDs(ctx context.Context, ids []string) (map[string]bool, error)

	// KV
	KVGet(ctx context.Context, key string) (string, bool, error)
	KVSet(ctx context.Context, key, value string) error

	// Tank profiles
	UpsertTankProfile(ctx context.Context, profile *TankProfile) error
	GetTankProfile(ctx context.Context, tankID string) (*TankProfile, error)
	ListTankProfiles(ctx context.Context) ([]*TankProfile, error)
	DeleteTankProfile(ctx context.Context, tankID string) error
	CountActiveFeedCyclesForTank(ctx context.Context, tankID string) (int, error)

	// Group profiles (Phase 1 Group domain)
	UpsertGroupProfile(ctx context.Context, p *GroupProfile) error
	GetGroupProfile(ctx context.Context, groupID string) (*GroupProfile, error)
	ListGroupProfiles(ctx context.Context) ([]*GroupProfile, error)
	DeleteGroupProfile(ctx context.Context, groupID string) error
	ListTanksByGroup(ctx context.Context, groupID string) ([]*TankProfile, error)

	// Feeding policy resolver (Phase 1b) — group default + tank override 조합.
	// AI worker (Phase 1c) 가 이 함수를 통해 effective policy 조회.
	GetEffectiveFeedingPolicy(ctx context.Context, tankID string) (*FeedingPolicy, error)

	// Autonomous mode (Phase 4 C-1)
	GetTankAutonomousMode(ctx context.Context, tankID string) (*TankAutonomousMode, error)
	UpsertTankAutonomousMode(ctx context.Context, mode *TankAutonomousMode) error
	ListTankAutonomousModes(ctx context.Context) ([]*TankAutonomousMode, error)

	// Decision policy (Phase 4 C-4)
	GetTankDecisionPolicy(ctx context.Context, tankID string) (*TankDecisionPolicy, error)
	UpsertTankDecisionPolicy(ctx context.Context, p *TankDecisionPolicy) error

	// Lifecycle (D-1)
	GetTankLifecycle(ctx context.Context, tankID string) (*TankLifecycle, error) // nil if no row
	UpsertTankLifecycle(ctx context.Context, lifecycle *TankLifecycle) error

	// GDST 증빙 서류
	InsertTankDocument(ctx context.Context, d *TankDocument) error
	GetTankDocument(ctx context.Context, documentID string) (*TankDocument, error) // nil if no row
	ListTankDocuments(ctx context.Context, tankID string) ([]*TankDocument, error)
	ListDocumentsBySubject(ctx context.Context, subjectType, subjectID string) ([]*TankDocument, error)

	// 재고관리
	UpsertInventoryItem(ctx context.Context, item *InventoryItem) error
	GetInventoryItem(ctx context.Context, itemID string) (*InventoryItem, error) // nil if no row
	ListInventoryItems(ctx context.Context, category string) ([]*InventoryItem, error)
	AdjustInventoryOnHand(ctx context.Context, itemID string, delta float64) (float64, error) // returns new on_hand

	// 거래처(공급처/구매처)
	UpsertPartner(ctx context.Context, p *Partner) error
	GetPartner(ctx context.Context, partnerID string) (*Partner, error) // nil if no row
	ListPartners(ctx context.Context, partnerType, siteID string) ([]*Partner, error)

	// site 단위 거래 (입식 배치 / 출하 건)
	UpsertSiteStocking(ctx context.Context, st *SiteStocking) error
	GetSiteStocking(ctx context.Context, id string) (*SiteStocking, error) // nil if no row
	ListSiteStockings(ctx context.Context, siteID string) ([]*SiteStocking, error)
	UpsertSiteHarvest(ctx context.Context, h *SiteHarvest) error
	GetSiteHarvest(ctx context.Context, id string) (*SiteHarvest, error) // nil if no row
	ListSiteHarvests(ctx context.Context, siteID string) ([]*SiteHarvest, error)

	// Sampling (D-2)
	GetTankSampling(ctx context.Context, tankID string) (*TankSampling, error) // nil if no row
	UpsertTankSampling(ctx context.Context, s *TankSampling) error

	// FCR calibration (D-4)
	GetTankFCRCalibration(ctx context.Context, tankID string) (*TankFCRCalibration, error) // nil if no row
	UpsertTankFCRCalibration(ctx context.Context, c *TankFCRCalibration) error

	// Weight history (D-5)
	UpsertTankWeightSnapshot(ctx context.Context, s *TankWeightSnapshot) error
	ListTankWeightHistory(ctx context.Context, tankID string, days int) ([]TankWeightSnapshot, error)

	// Camera profiles
	UpsertCameraProfile(ctx context.Context, profile *CameraProfile) error
	GetCameraProfile(ctx context.Context, cameraID string) (*CameraProfile, error)
	ListCameraProfiles(ctx context.Context, tankID string) ([]*CameraProfile, error)
	DeleteCameraProfile(ctx context.Context, cameraID string) error
	CountCameraProfilesForModel(ctx context.Context, modelID string) (int, error)

	// Camera model library (C-11)
	UpsertCameraModel(ctx context.Context, model *CameraModel) error
	GetCameraModel(ctx context.Context, modelID string) (*CameraModel, error)
	ListCameraModels(ctx context.Context) ([]*CameraModel, error)
	DeleteCameraModel(ctx context.Context, modelID string) error

	// Current state projections
	UpsertDeviceStatus(ctx context.Context, deviceID, deviceType, status, health, lastEventID string, lastSeenAt *time.Time, detailsJSON string) error
	ListDeviceStatuses(ctx context.Context) ([]map[string]any, error)
	UpsertTankEnvironmentReading(ctx context.Context, reading *CurrentTankEnvironmentReading, payloadJSON string) error
	ListTankEnvironment(ctx context.Context, tankID string) ([]*CurrentTankEnvironmentReading, error)
	UpsertCameraStatus(ctx context.Context, camera *CurrentCameraStatus, detailsJSON string) error
	ListCameraStatuses(ctx context.Context, tankID string) ([]*CurrentCameraStatus, error)
	UpsertWaterQualityBucket(ctx context.Context, bucket *WaterQualityBucketProjection) error
	ListWaterQualityBuckets(ctx context.Context, tankID string, since, until *time.Time, limit int) ([]*WaterQualityBucketProjection, error)
	UpsertFeedingImpactAnalysis(ctx context.Context, analysis *FeedingImpactAnalysisProjection) error
	GetFeedingImpactAnalysis(ctx context.Context, feedingID string) (*FeedingImpactAnalysisProjection, error)

	// Alerts
	UpsertAlert(ctx context.Context, a *OpenAlert) (bool, error) // created=true if new
	ClearAlert(ctx context.Context, dedupeKey string) error
	// ClearAlertByID — 운영자 명시적 close API 용. (rowsAffected, error)
	ClearAlertByID(ctx context.Context, alertID string) (int64, error)
	GetOpenAlertByID(ctx context.Context, alertID string) (*OpenAlert, error)
	GetOpenAlert(ctx context.Context, dedupeKey string) (*OpenAlert, error)
	ListOpenAlerts(ctx context.Context) ([]*OpenAlert, error)

	// Commands
	InsertCommand(ctx context.Context, cmd *ControlCommand) error
	GetCommandByIdempotencyKey(ctx context.Context, key string) (*ControlCommand, error)
	GetCommand(ctx context.Context, commandID string) (*ControlCommand, error)
	UpdateCommandStatus(ctx context.Context, commandID, status, lastEventID string) error
	ListNextCommandForDevices(ctx context.Context, deviceIDs []string, now time.Time) (*ControlCommand, error)
	ListExpiredCommands(ctx context.Context, now time.Time) ([]*ControlCommand, error)

	// Domain registry (Phase 1 multi-tank)
	UpsertFarm(ctx context.Context, f *farm.Farm) error
	ListFarms(ctx context.Context) ([]*farm.Farm, error)
	GetFarm(ctx context.Context, farmID string) (*farm.Farm, error)
	DeleteFarm(ctx context.Context, farmID string) error
	CountSitesForFarm(ctx context.Context, farmID string) (int, error)
	UpsertSiteLand(ctx context.Context, sl *site.SiteLand) error
	UpsertSiteMarine(ctx context.Context, sm *site.SiteMarine) error
	ListSites(ctx context.Context, farmID string) ([]map[string]any, error)
	DeleteSite(ctx context.Context, siteID string) error
	SiteExists(ctx context.Context, siteID string) (bool, error)
	CountWTGsForSite(ctx context.Context, siteID string) (int, error)
	CountTanksForSite(ctx context.Context, siteID string) (int, error)
	UpsertWTG(ctx context.Context, g *wtg.Group) error
	ListWTGs(ctx context.Context, siteID string) ([]*wtg.Group, error)
	DeleteWTG(ctx context.Context, wtgID string) error
	WTGExists(ctx context.Context, wtgID string) (bool, error)
	CountTanksForWTG(ctx context.Context, wtgID string) (int, error)
	UpsertController(ctx context.Context, c *controller.Controller) error
	GetControllerByMAC(ctx context.Context, mac string) (*controller.Controller, error)
	GetController(ctx context.Context, controllerID string) (*controller.Controller, error)
	ListControllers(ctx context.Context, status string) ([]*controller.Controller, error)
	DeleteController(ctx context.Context, controllerID string) error
	CountActuatorsForController(ctx context.Context, controllerID string) (int, error)
	UpsertActuator(ctx context.Context, a *actuator.Actuator) error
	ListActuators(ctx context.Context, tankID, siteID, wtgID string) ([]*actuator.Actuator, error)
	DeleteActuator(ctx context.Context, deviceID string) error
	ActuatorExists(ctx context.Context, deviceID string) (bool, error)
	UpsertSensor(ctx context.Context, sen *sensor.Sensor) error
	ListSensors(ctx context.Context, tankID, siteID, wtgID string) ([]*sensor.Sensor, error)
	DeleteSensor(ctx context.Context, sensorID string) error
	SensorExists(ctx context.Context, sensorID string) (bool, error)
	UpsertSpeciesProfile(ctx context.Context, key string, p *species.Profile) error
	ListSpeciesProfiles(ctx context.Context) ([]map[string]any, error)
	DeleteSpeciesProfile(ctx context.Context, key string) error
	SpeciesProfileExists(ctx context.Context, key string) (bool, error)
	CountTanksForSpecies(ctx context.Context, key string) (int, error)

	// Feeding schedules (Phase 5)
	UpsertSchedule(ctx context.Context, sched *FeedingSchedule) error
	ListEnabledSchedules(ctx context.Context) ([]*FeedingSchedule, error)
	ListAllSchedules(ctx context.Context) ([]*FeedingSchedule, error)
	GetSchedule(ctx context.Context, scheduleID string) (*FeedingSchedule, error)
	DeleteSchedule(ctx context.Context, scheduleID string) error
	SetScheduleEnabled(ctx context.Context, scheduleID string, enabled bool) error

	// Feed cycles (Phase 3)
	InsertFeedCycle(ctx context.Context, c *FeedCycle) error
	UpdateFeedCycleProgress(ctx context.Context, cycleID string, pulsesExecuted int, totalAmountG float64) error
	CompleteFeedCycle(ctx context.Context, cycleID string, pulsesExecuted int, totalAmountG float64, terminationReason string, completedAt time.Time) error
	GetFeedCycle(ctx context.Context, cycleID string) (*FeedCycle, error)
	ListActiveFeedCycles(ctx context.Context) ([]*FeedCycle, error)
	ListRecentFeedCycles(ctx context.Context, tankID string, limit int) ([]*FeedCycle, error)
	// AbortOrphanFeedCycles — completed_at IS NULL 인 cycle 들을 강제 종료 마킹.
	// backend 재가동 시 worker 메모리 잃은 orphan cycle 정리용.
	AbortOrphanFeedCycles(ctx context.Context, terminationReason string, abortedAt time.Time) (int, error)
	// Phase 5 (load cell) — HX711 weight event 누적 + silo depletion flag.
	UpdateFeedCycleActualTotal(ctx context.Context, cycleID string, actualTotalG float64) error
	SetFeedCycleSiloDepletionWarned(ctx context.Context, cycleID string) error

	// Predictive blocks (Phase 4 C-3p)
	InsertPredictiveBlock(ctx context.Context, b *PredictiveBlock) error
	ListPredictiveBlocks(ctx context.Context, limit int) ([]*PredictiveBlock, error)

	// Operator disputes + learned rules (Phase 4 C-3l)
	InsertOperatorDispute(ctx context.Context, d *OperatorDispute) error
	ListOperatorDisputes(ctx context.Context, limit int) ([]*OperatorDispute, error)
	InsertLearnedRule(ctx context.Context, r *LearnedRule) error
	ListLearnedRules(ctx context.Context, onlyEnabled bool) ([]*LearnedRule, error)
	SetLearnedRuleEnabled(ctx context.Context, ruleID string, enabled bool) error
	IncrementLearnedRuleHit(ctx context.Context, ruleID string, matchedAt time.Time) error

	// Environmental snapshots (Phase 4 C-3w)
	InsertEnvironmentalSnapshot(ctx context.Context, snap *EnvironmentalSnapshot) error
	ListRecentEnvironmentalSnapshots(ctx context.Context, siteID string, limit int) ([]*EnvironmentalSnapshot, error)

	// Arbiter decisions (Phase 5)
	InsertArbiterDecision(ctx context.Context, d *ArbiterDecision) error
	ListArbiterDecisions(ctx context.Context, tankID string, limit int) ([]*ArbiterDecision, error)
	GetArbiterDecisionByCycleID(ctx context.Context, cycleID string) (*ArbiterDecision, error)

	// Operator intents (Phase 5)
	InsertOperatorIntent(ctx context.Context, intent *OperatorIntent) error
	ListOperatorIntents(ctx context.Context, tankID string, limit int) ([]*OperatorIntent, error)
	GetOperatorIntent(ctx context.Context, intentID string) (*OperatorIntent, error)

	// Sync batches
	InsertSyncBatch(ctx context.Context, b *SyncBatch, eventSeqs []int64) error
	UpdateSyncBatchStatus(ctx context.Context, batchID, status string, sentAt, ackAt *time.Time, remoteAckID, errorJSON string) error
	GetPendingSyncBatch(ctx context.Context) (*SyncBatch, error)
	GetSyncBatchEvents(ctx context.Context, batchID string) ([]*Event, error)
	CountPendingBatches(ctx context.Context) (int, error)
	CountFailedBatches(ctx context.Context) (int, error)

	// Inbound (DOWN) sync — platform-originated events pulled onto local state
	ApplyInboundEvent(ctx context.Context, eventID, eventType, tankID string, reduceCount int, payloadJSON, siteID, edgeID string) (bool, error)

	// Sensor model library (C-13a)
	UpsertSensorModel(ctx context.Context, model *SensorModel) error
	GetSensorModel(ctx context.Context, modelID string) (*SensorModel, error)
	ListSensorModels(ctx context.Context) ([]*SensorModel, error)
	DeleteSensorModel(ctx context.Context, modelID string) error
	CountSensorsForModel(ctx context.Context, modelID string) (int, error)

	// Actuator model library (C-13b)
	UpsertActuatorModel(ctx context.Context, model *ActuatorModel) error
	GetActuatorModel(ctx context.Context, modelID string) (*ActuatorModel, error)
	ListActuatorModels(ctx context.Context) ([]*ActuatorModel, error)
	DeleteActuatorModel(ctx context.Context, modelID string) error
	CountActuatorsForModel(ctx context.Context, modelID string) (int, error)

	// Hatchery — broodstock cohorts (037)
	UpsertBroodstockCohort(ctx context.Context, c *BroodstockCohort) error
	GetBroodstockCohort(ctx context.Context, cohortID string) (*BroodstockCohort, error)
	ListBroodstockByGroup(ctx context.Context, groupID string) ([]*BroodstockCohort, error)
	DeleteBroodstockCohort(ctx context.Context, cohortID string) error

	// Hatchery — spawn batches / egg-seed lots (038)
	UpsertSpawnBatch(ctx context.Context, b *SpawnBatch) error
	GetSpawnBatch(ctx context.Context, batchID string) (*SpawnBatch, error)
	GetSpawnBatchByLotCode(ctx context.Context, lotCode string) (*SpawnBatch, error)
	ListSpawnBatchesByGroup(ctx context.Context, groupID string) ([]*SpawnBatch, error)
	DeleteSpawnBatch(ctx context.Context, batchID string) error

	// Hatchery — larval batches (039)
	UpsertLarvalBatch(ctx context.Context, b *LarvalBatch) error
	GetLarvalBatch(ctx context.Context, batchID string) (*LarvalBatch, error)
	ListLarvalBatchesByGroup(ctx context.Context, groupID string) ([]*LarvalBatch, error)
	DeleteLarvalBatch(ctx context.Context, batchID string) error

	// Hatchery — live feed cultures (039)
	UpsertLiveFeedCulture(ctx context.Context, c *LiveFeedCulture) error
	GetLiveFeedCulture(ctx context.Context, cultureID string) (*LiveFeedCulture, error)
	ListLiveFeedByGroup(ctx context.Context, groupID string) ([]*LiveFeedCulture, error)
	DeleteLiveFeedCulture(ctx context.Context, cultureID string) error

	// Hatchery — treatments / 처치 CTE (040)
	UpsertHatcheryTreatment(ctx context.Context, t *HatcheryTreatment) error
	GetHatcheryTreatment(ctx context.Context, treatmentID string) (*HatcheryTreatment, error)
	ListHatcheryTreatmentsByGroup(ctx context.Context, groupID string) ([]*HatcheryTreatment, error)
	DeleteHatcheryTreatment(ctx context.Context, treatmentID string) error
}
