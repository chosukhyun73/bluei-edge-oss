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
	// Until 은 recorded_at 상한. Since 와 함께 쓰면 idx_events_type_time 의 범위 스캔이
	// 되어, 희소 조건(json_extract)을 전체 행에 돌리는 것을 피한다 — 순수 Go sqlite
	// 드라이버에서 그 전체 스캔은 26만 행에 10초 이상 걸린다(2026-09-17 실측).
	Until    *time.Time
	Limit    int
	AfterSeq int64
	// EvidencePhase 는 payload_json 의 evidence.phase 로 필터한다(예: media.clip.stored
	// 의 "feeding"/"baseline"). 희소 phase(급이)가 다수 phase(상시 baseline)에 파묻히지
	// 않게 DB 단에서 분리 조회하는 용도.
	EvidencePhase string
	// PayloadClipID 는 payload_json 의 clip_id 로 필터한다. 특정 클립을 최근 N건 스캔
	// 윈도우에 의존하지 않고 DB 단에서 직접 조회(급이 클립이 baseline 에 파묻혀도 찾음).
	PayloadClipID string
	// PayloadCycleID 는 payload_json 의 top-level cycle_id 로 필터한다(예: feed.pulse.completed).
	// 펄스 이벤트가 무한 누적돼도 오래된 사이클의 세그먼트를 비파편적으로 조회.
	PayloadCycleID string
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
	TankID           string    `json:"tank_id"`
	BucketStart      time.Time `json:"bucket_start"`
	BucketSec        int       `json:"bucket_sec"`
	TemperatureCAvg  *float64  `json:"temperature_c_avg"`
	PHAvg            *float64  `json:"ph_avg"`
	DOMgLAvg         *float64  `json:"do_mg_l_avg"`
	Quality          string    `json:"quality"`
	SampleCount      int       `json:"sample_count"`
	SuspectCount     int       `json:"suspect_count"`
	SourceReadingIDs string    `json:"source_reading_ids"`
	UpdatedAt        time.Time `json:"updated_at"`
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
	ItemID          string
	Category        string // feed | drug | material
	Name            string
	Unit            string
	OnHandQty       float64
	Spec            string
	Supplier        string
	ReorderLevel    *float64 // nullable
	Notes           string
	CompositionJSON string // 영양성분표(보증성분) JSON, 빈 문자열=미등록 (사료 위주)
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PlantProfile — 가공공장 정보. 허가·등록번호와 인권 정책 유무는 GDST 가공 기록에 쓰인다.
type PlantProfile struct {
	SiteID             string
	BusinessNo         string
	LicenseName        string
	LicenseNo          string
	LicenseAuthority   string
	HACCPCertNo        string
	HumanWelfarePolicy string // "" | none | internal_policy | third_party_audit
	RetentionYears     int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// PlantZone — 공장 안의 구역.
type PlantZone struct {
	ZoneID       string
	SiteID       string
	Name         string
	HygieneClass string // general | clean
	TempClass    string // ambient | chilled | frozen
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PlantEquipment — 설비.
type PlantEquipment struct {
	EquipmentID string
	SiteID      string
	ZoneID      string
	Kind        string
	Name        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PlantSensor — 공정 센서(구역 또는 설비에 붙는다).
type PlantSensor struct {
	SensorID         string
	SiteID           string
	ZoneID           string
	EquipmentID      string
	Metric           string
	Unit             string
	IsMock           bool
	CalibrationDueAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// LocalUser — 현장 서명용 로컬 계정. PIN 은 솔트를 섞은 해시로만 둔다.
type LocalUser struct {
	UserID    string
	SiteID    string
	Name      string
	Role      string // admin | reviewer | monitor
	PinSalt   string
	PinHash   string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlantCertificate — 인증서 원장의 한 줄. 날짜는 YYYY-MM-DD 문자열.
type PlantCertificate struct {
	CertID     string
	SiteID     string
	Scheme     string
	CertNo     string
	Scope      string
	HolderKind string // own | supplier
	HolderName string
	ValidFrom  string
	ValidUntil string // "" = 만료일 없음
	VerifiedAt string
}

// 생산 실행 — docs/54 §26. 시각은 UTC unix 밀리초.

// ProcessReading — 공정 센서 측정값 하나.
type ProcessReading struct {
	SensorID string
	Value    float64
	Source   string // mock | sensor
	At       int64
}

// MockSensorState — 모의 센서에 지정한 값 또는 끊김. UntilAt 이 지나면 무효.
type MockSensorState struct {
	SensorID string
	Mode     string // fixed | outage
	Value    float64
	UntilAt  int64
}

// ProcessLot — 생산 lot 하나. VersionID 의 게시본으로 판정한다.
type ProcessLot struct {
	LotID      string
	SiteID     string
	PlanID     string
	VersionID  string
	LotNo      string
	Status     string // running | closed
	InputsJSON string
	OutputKg   float64
	StartedBy  string
	StartedAt  int64
	ClosedBy   string
	ClosedAt   int64
	ReviewedBy string
	ReviewedAt int64
}

// ProcessLotEvent — lot 의 단계 진입(enter)·이탈(exit).
type ProcessLotEvent struct {
	Kind   string
	StepID string
	LinkID string
	At     int64
}

// ProcessCheck — 사람이 넣은 측정·점검 값.
type ProcessCheck struct {
	LimitID string
	Value   float64
	ByUser  string
	At      int64
}

// ProcessDeviation — 한계기준 이탈 또는 모니터링 불능.
type ProcessDeviation struct {
	DevID            string
	LotID            string
	CCPID            string
	LimitID          string
	Kind             string // deviation | outage
	DetectedAt       int64
	Detail           string
	Status           string // open | closed
	CorrectiveAction string
	Disposition      string
	ClosedBy         string
	ClosedAt         int64
}

// IssuedDocument — 발행 문서 대장의 한 줄.
type IssuedDocument struct {
	DocID    string
	SiteID   string
	Kind     string // plan | lot
	RefID    string
	Title    string
	SHA256   string
	DataHash string
	IssuedBy string
	IssuedAt int64
	Path     string
}

// PlantForm — 공장 기록 양식의 한 판. Content 는 plantform.Form 의 JSON.
type PlantForm struct {
	SiteID    string
	FormID    string
	Revision  int
	Source    string // default | import | edit
	BasePack  string
	BaseID    string
	Content   string
	Note      string
	CreatedBy string
	CreatedAt int64
}

// HACCPPlan — 가공공장의 HACCP 계획(제품군 하나). docs/54.
type HACCPPlan struct {
	PlanID       string
	SiteID       string
	ProductGroup string
	Name         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// HACCPPlanVersion — 계획의 한 버전. 내용은 ContentJSON(haccp.PlanContent) 한 덩어리.
// status: draft | published | retired. 게시 뒤에는 내용이 바뀌지 않는다.
type HACCPPlanVersion struct {
	VersionID         string
	PlanID            string
	VersionNo         int
	Status            string
	TemplateRef       string
	ContentJSON       string
	ContentHash       string
	ChangeReason      string
	CCPChanged        bool
	OnsiteConfirmedBy string
	OnsiteConfirmedAt *time.Time
	PublishedBy       string
	PublishedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
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
	AvgLengthCm      *float64 // optional
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
	ListUnsyncedUnbatchedEvents(ctx context.Context, includeTypes []string, limit int) ([]*Event, error)
	CollapseRedundantReadings(ctx context.Context) (int64, error)
	LatestSensorReadings(ctx context.Context, f LatestReadingFilter) ([]*LatestSensorReading, error)
	UnsyncedCount(ctx context.Context) (int64, error)
	OldestUnsyncedAge(ctx context.Context) (time.Duration, error)
	MarkSynced(ctx context.Context, eventIDs []string) error
	// Retention — 만료 telemetry 백업+제거. SelectEventsOlderThan 은 sequence ASC 로
	// 배치 반환, DeleteEventsOlderThanUpToSeq 는 그 배치(최대 sequence)까지만 삭제한다.
	SelectEventsOlderThan(ctx context.Context, eventType string, filter MetricFilter, cutoff time.Time, limit int) ([]ArchivableEvent, error)
	DeleteEventsOlderThanUpToSeq(ctx context.Context, eventType string, filter MetricFilter, cutoff time.Time, maxSeq int64) (int64, error)
	// 센서 일별 집계 — recorded_at < before 인 sensor.reading.recorded 를 (날짜, sensor_id, metric)
	// 별 min/max/avg/count 로 묶어 반환. 날짜는 tzOffsetSeconds 만큼 보정한 로컬 달력일.
	AggregateSensorReadingsDailyBefore(ctx context.Context, before time.Time, tzOffsetSeconds int) ([]SensorDailyAgg, error)
	// 주어진 event_id 들 중 이미 존재하는 것의 집합 (요약 멱등 처리용).
	FilterExistingEventIDs(ctx context.Context, ids []string) (map[string]bool, error)

	// KV
	KVGet(ctx context.Context, key string) (string, bool, error)
	KVSet(ctx context.Context, key, value string) error
	KVListPrefix(ctx context.Context, prefix string) (map[string]string, error)

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
	SumTankCountAdjustment(ctx context.Context, tankID, stockingID string) (int, error)
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
	SetInventoryComposition(ctx context.Context, itemID, compJSON string) error               // 영양성분표 JSON 갱신

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
	UpsertSiteProcessing(ctx context.Context, sl *site.SiteLand) error
	// GetSiteType returns the site_type of a site, or "" if the site does not exist.
	GetSiteType(ctx context.Context, siteID string) (string, error)

	// Plant setup (가공공장 구성: 공장 정보·구역·설비·공정 센서·로컬 계정) — docs/54.
	UpsertPlantProfile(ctx context.Context, p *PlantProfile) error
	GetPlantProfile(ctx context.Context, siteID string) (*PlantProfile, error) // nil if no row
	UpsertPlantZone(ctx context.Context, z *PlantZone) error
	ListPlantZones(ctx context.Context, siteID string) ([]*PlantZone, error)
	UpsertPlantEquipment(ctx context.Context, e *PlantEquipment) error
	ListPlantEquipment(ctx context.Context, siteID string) ([]*PlantEquipment, error)
	UpsertPlantSensor(ctx context.Context, sn *PlantSensor) error
	ListPlantSensors(ctx context.Context, siteID string) ([]*PlantSensor, error)
	InsertLocalUser(ctx context.Context, u *LocalUser) error
	GetLocalUser(ctx context.Context, userID string) (*LocalUser, error) // nil if no row
	ListLocalUsers(ctx context.Context, siteID string) ([]*LocalUser, error)
	UpsertPlantCertificate(ctx context.Context, c *PlantCertificate) error
	ListPlantCertificates(ctx context.Context, siteID string) ([]*PlantCertificate, error)
	// SetPlantComplianceProfiles replaces the schemes the plant applies.
	SetPlantComplianceProfiles(ctx context.Context, siteID string, profileIDs []string) error
	ListPlantComplianceProfiles(ctx context.Context, siteID string) ([]string, error)

	// 생산 실행 — docs/54 §26.
	InsertProcessReading(ctx context.Context, siteID string, r *ProcessReading) error
	ListProcessReadings(ctx context.Context, sensorID string, from, to int64) ([]ProcessReading, error)
	LatestProcessReading(ctx context.Context, sensorID string) (*ProcessReading, error) // nil if none
	SetMockSensorState(ctx context.Context, st *MockSensorState) error
	ListMockSensorStates(ctx context.Context) ([]MockSensorState, error)
	InsertProcessLot(ctx context.Context, l *ProcessLot) error
	GetProcessLot(ctx context.Context, lotID string) (*ProcessLot, error) // nil if no row
	ListProcessLots(ctx context.Context, siteID string) ([]*ProcessLot, error)
	ListRunningProcessLots(ctx context.Context) ([]*ProcessLot, error)
	CloseProcessLot(ctx context.Context, lotID string, outputKg float64, by string, at int64) error
	ReviewProcessLot(ctx context.Context, lotID, by string, at int64) error
	InsertProcessLotEvent(ctx context.Context, lotID string, e *ProcessLotEvent) error
	ListProcessLotEvents(ctx context.Context, lotID string) ([]ProcessLotEvent, error)
	InsertProcessCheck(ctx context.Context, lotID string, c *ProcessCheck) error
	ListProcessChecks(ctx context.Context, lotID string) ([]ProcessCheck, error)
	// InsertProcessDeviation records a deviation once per (lot, limit, kind). Returns false if it already existed.
	InsertProcessDeviation(ctx context.Context, d *ProcessDeviation) (bool, error)
	ListProcessDeviations(ctx context.Context, lotID string) ([]*ProcessDeviation, error)
	// CloseProcessDeviation closes an open deviation. Returns false if it was not open.
	CloseProcessDeviation(ctx context.Context, devID, action, disposition, by string, at int64) (bool, error)
	InsertIssuedDocument(ctx context.Context, d *IssuedDocument) error
	GetIssuedDocument(ctx context.Context, docID string) (*IssuedDocument, error) // nil if no row
	ListIssuedDocuments(ctx context.Context, siteID string) ([]*IssuedDocument, error)

	// 공장 기록 양식 — docs/56 §18. 판은 쌓기만 한다.
	// InsertPlantForm stores f as the next revision of (site, form) and sets f.Revision.
	InsertPlantForm(ctx context.Context, f *PlantForm) error
	GetPlantForm(ctx context.Context, siteID, formID string) (*PlantForm, error) // 지금 쓰는 판. nil if no row
	ListPlantForms(ctx context.Context, siteID string) ([]*PlantForm, error)     // 양식마다 지금 쓰는 판

	// HACCP plans (가공공장 공정도·CCP) — docs/54.
	CreateHACCPPlan(ctx context.Context, p *HACCPPlan, first *HACCPPlanVersion) error
	GetHACCPPlan(ctx context.Context, planID string) (*HACCPPlan, error) // nil if no row
	ListHACCPPlans(ctx context.Context, siteID string) ([]*HACCPPlan, error)
	ListHACCPPlanVersions(ctx context.Context, planID string) ([]*HACCPPlanVersion, error)
	GetHACCPPlanVersion(ctx context.Context, versionID string) (*HACCPPlanVersion, error) // nil if no row
	InsertHACCPPlanVersion(ctx context.Context, v *HACCPPlanVersion) error
	// UpdateHACCPDraftContent saves a draft's content. Returns false if the version is not a draft.
	UpdateHACCPDraftContent(ctx context.Context, versionID, contentJSON string) (bool, error)
	// PublishHACCPPlanVersion publishes a draft and retires the plan's previous published version.
	// Returns false if the version is not a draft.
	PublishHACCPPlanVersion(ctx context.Context, v *HACCPPlanVersion) (bool, error)
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
	UpsertSiteEnvObservations(ctx context.Context, obs []SiteEnvObservation) error
	ListSiteEnvObservations(ctx context.Context, siteID, metric string, since, until time.Time) ([]SiteEnvObservation, error)
	AggregateSiteEnvDaily(ctx context.Context, siteID string, since time.Time, tzOffsetSeconds int) ([]EnvDailyAgg, error)
	AggregateTankSensorDailyByObserved(ctx context.Context, since time.Time, tzOffsetSeconds int) ([]EnvDailyAgg, error)
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
