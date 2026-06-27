package config

// Config is the root configuration structure loaded from edge.yaml.
type Config struct {
	Site                SiteConfig                `yaml:"site"`
	Edge                EdgeConfig                `yaml:"edge"`
	Runtime             RuntimeConfig             `yaml:"runtime"`
	API                 APIConfig                 `yaml:"api"`
	Storage             StorageConfig             `yaml:"storage"`
	Tanks               TanksRef                  `yaml:"tanks"`
	Groups              GroupsRef                 `yaml:"groups"`
	Devices             DevicesRef                `yaml:"devices"`
	Logging             LoggingRef                `yaml:"logging"`
	Collector           CollectorConfig           `yaml:"collector"`
	Rules               RulesRef                  `yaml:"rules"`
	Control             ControlConfig             `yaml:"control"`
	Sync                SyncConfig                `yaml:"sync"`
	Inference           InferenceConfig           `yaml:"inference"`
	BaselineWorker      BaselineWorkerConfig      `yaml:"baseline_worker"`
	DecisionPolicy      DecisionPolicyConfig      `yaml:"decision_policy"`
	WeightHistoryWorker WeightHistoryWorkerConfig `yaml:"weight_history_worker"`
	// Phase 1 multi-tank domain registry refs (모두 optional — 비어있으면 로드 skip)
	Farms                FarmsRef                  `yaml:"farms"`
	SitesLand            SitesRef                  `yaml:"sites_land"`
	SitesMarine          SitesRef                  `yaml:"sites_marine"`
	WaterTreatmentGroups WTGRef                    `yaml:"water_treatment_groups"`
	Actuators            ActuatorsRef              `yaml:"actuators"`
	Sensors              SensorsRef                `yaml:"sensors"`
	Controllers          ControllersRef            `yaml:"controllers"`
	SpeciesProfiles      SpeciesProfilesRef        `yaml:"species_profiles"`
	Cameras              CamerasRef                `yaml:"cameras"`
	FeedCycle            FeedCycleConfig           `yaml:"feed_cycle"`
	Capture              CaptureConfig             `yaml:"capture"`
	PredictiveSafety     PredictiveSafetyConfig    `yaml:"predictive_safety"`
	LearnedSafety        LearnedSafetyConfig       `yaml:"learned_safety"`
	EnvironmentalSafety  EnvironmentalSafetyConfig `yaml:"environmental_safety"`
	Schedule             ScheduleConfig            `yaml:"schedule"`
	LLM                  LLMConfig                 `yaml:"llm"`
	Knowledge            KnowledgeConfig           `yaml:"knowledge"`
	Arbiter              ArbiterConfig             `yaml:"arbiter"`
	Retention            RetentionConfig           `yaml:"retention"`
}

// KnowledgeConfig — 어시스턴트 RAG 지식팩 설정.
// 인덱스(rag-index.jsonl)는 별도 게이팅 배포로 기기에 설치되며, 여기서 경로만 가리킨다.
// enabled=false 또는 인덱스 파일이 없으면 RAG 없이 동작(기존 어시스턴트 그대로).
type KnowledgeConfig struct {
	Enabled    bool   `yaml:"enabled"`     // false 면 RAG 비활성
	IndexPath  string `yaml:"index_path"`  // 예) /var/lib/bluei-edge/knowledge/rag-index.jsonl
	EmbedModel string `yaml:"embed_model"` // 예) bge-m3 (다국어). 비어있으면 bge-m3
	TopK       int    `yaml:"top_k"`       // 주입할 청크 수. 0 → 4
}

// RetentionConfig — 고용량 telemetry 이벤트의 보관기간 정책.
// 만료된 이벤트는 월별 백업 파일로 export 된 뒤 live DB 에서 제거된다.
// 백업 파일은 자동 삭제하지 않는다 — 저장/삭제는 운영자가 직접 관리한다.
// Rules 에 없는 event_type 은 보존된다 (운영·감사 이벤트는 무기한 유지).
type RetentionConfig struct {
	Enabled         bool                  `yaml:"enabled"`
	IntervalSec     int                   `yaml:"interval_sec"`      // 점검 주기(초). 0 → 86400 (하루)
	InitialDelaySec int                   `yaml:"initial_delay_sec"` // 부팅 후 첫 실행 지연(초). 0 → 120
	ArchiveDir      string                `yaml:"archive_dir"`       // 비어있으면 sqlite 경로 디렉터리/archive 사용
	Rules           []RetentionRuleConfig `yaml:"rules"`
}

// RetentionRuleConfig — 한 event_type 의 보관기간(일).
type RetentionRuleConfig struct {
	EventType string `yaml:"event_type"`
	KeepDays  int    `yaml:"keep_days"`
	// AggregateDaily — true 면 prune 전에 일별 min/max/avg 요약을 남긴다 (센서 측정값용).
	// cutoff 가 로컬 달력일 경계로 정렬되어 최근 24~48h raw 는 보존된다.
	AggregateDaily bool `yaml:"aggregate_daily"`
}

// ArbiterConfig — Phase G Arbiter 환경 safety gate 설정.
// 모든 cycle source (운영자, schedule, AI, force) 가 거치는 단일 진입점.
type ArbiterConfig struct {
	SafetyGate ArbiterSafetyGateConfig `yaml:"safety_gate"`
}

// ArbiterSafetyGateConfig — 환경 임계 검사 파라미터.
// enabled=false 면 게이트 검사 skip (기존 동작 유지).
type ArbiterSafetyGateConfig struct {
	Enabled           bool    `yaml:"enabled"`
	SensorMaxStaleSec int     `yaml:"sensor_max_stale_sec"`
	TempMinC          float64 `yaml:"temp_min_c"`
	TempMaxC          float64 `yaml:"temp_max_c"`
	DOMinMgL          float64 `yaml:"do_min_mg_l"`
}

// LLMConfig — Phase F.1 operator-intent 종합 판단용 로컬 LLM 설정.
// 본 섹션은 Inference.Ollama (advisory analyzer) 와 분리. operator_intents POST hook 전용.
type LLMConfig struct {
	Endpoint      string `yaml:"endpoint"`       // 예) http://localhost:11435
	AuthToken     string `yaml:"auth_token"`     // proxy bearer token (optional)
	PrimaryModel  string `yaml:"primary_model"`  // 예) gemma4:e4b
	FallbackModel string `yaml:"fallback_model"` // 예) gemma4:26b
	TimeoutSec    int    `yaml:"timeout_sec"`    // primary 모델 1회 호출 timeout (default 15)
	Enabled       bool   `yaml:"enabled"`        // false 이면 client wiring skip
	// AssistantModel — 운영자 가이드 채팅용 모델 (/v1/assistant/chat). 위 primary/fallback 과
	// 별개의 자유텍스트 어시스턴트. 비어있으면 "bluei-edge-assistant".
	AssistantModel string `yaml:"assistant_model"` // 예) bluei-edge-assistant
	// AssistantKeepAlive — 어시스턴트 모델을 GPU 에 상주시키는 시간 (ollama keep_alive).
	// 비어있으면 "30m". "-1" 이면 무기한 상주. 콜드 재로딩 지연을 없애기 위함.
	AssistantKeepAlive string `yaml:"assistant_keep_alive"`
}

// LearnedSafetyConfig — Phase 4 C-3l 학습 안전 게이트 설정.
// 기본값 false — 충분한 dispute 데이터가 쌓인 후 명시 활성화.
type LearnedSafetyConfig struct {
	Enabled         bool `yaml:"enabled"`           // default false
	MineIntervalSec int  `yaml:"mine_interval_sec"` // default 3600
	StalenessMaxSec int  `yaml:"staleness_max_sec"` // default 300 — 이 시간(초)보다 오래된 센서값은 규칙 평가에서 skip
}

// EnvironmentalSafetyConfig — Phase 4 C-3w 환경 안전 게이트 설정.
// 해상 케이지 전용 — 육상 RAS 에는 영향 없음. 기본값 false.
type EnvironmentalSafetyConfig struct {
	Enabled            bool    `yaml:"enabled"`                 // default false
	Source             string  `yaml:"source"`                  // "mock" | "http"
	HTTPEndpoint       string  `yaml:"http_endpoint,omitempty"` // http source 전용
	RefreshIntervalSec int     `yaml:"refresh_interval_sec"`    // 캐시 갱신 주기(초). 0 → 600
	WindMaxMS          float64 `yaml:"wind_max_ms"`             // default 12 m/s
	WaveMaxM           float64 `yaml:"wave_max_m"`              // default 2.0 m
}

// ScheduleConfig — Phase 5 feeding schedule worker settings.
type ScheduleConfig struct {
	Enabled     bool `yaml:"enabled"`
	IntervalSec int  `yaml:"interval_sec"`
}

// PredictiveSafetyConfig — Phase 4 D-6 + C-3p predictive safety gate settings.
// Off by default — must be explicitly enabled after validating waste model inputs.
type PredictiveSafetyConfig struct {
	// Enabled gates the predictive safety check. Default false (safe default).
	Enabled bool `yaml:"enabled" json:"enabled"`
	// NH3CautionRatio is the fraction of WTG capacity at which Conservative is returned.
	// Default 0.7 (70%). Must be in (0,1).
	NH3CautionRatio float64 `yaml:"nh3_caution_ratio" json:"nh3_caution_ratio"`
	// DOMinMargin is reserved for future DO threshold logic (D-8).
	DOMinMargin float64 `yaml:"do_min_margin" json:"do_min_margin,omitempty"`
}

// FarmsRef — farms.yaml 경로 참조.
type FarmsRef struct {
	ConfigPath string `yaml:"config_path"`
}

// SitesRef — sites_land.yaml 또는 sites_marine.yaml 경로 참조.
type SitesRef struct {
	ConfigPath string `yaml:"config_path"`
}

// WTGRef — water_treatment_groups.yaml 경로 참조.
type WTGRef struct {
	ConfigPath string `yaml:"config_path"`
}

// ActuatorsRef — actuators.yaml 경로 참조.
type ActuatorsRef struct {
	ConfigPath string `yaml:"config_path"`
}

// SensorsRef — sensors.yaml 경로 참조.
type SensorsRef struct {
	ConfigPath string `yaml:"config_path"`
}

// ControllersRef — controllers.yaml 경로 참조.
type ControllersRef struct {
	ConfigPath string `yaml:"config_path"`
}

// CamerasRef — cameras.yaml 경로 참조 (boot 시 camera_profiles upsert).
type CamerasRef struct {
	ConfigPath string `yaml:"config_path"`
}

// SpeciesProfilesRef — species_profiles.yaml 경로 참조.
type SpeciesProfilesRef struct {
	ConfigPath string `yaml:"config_path"`
}

// DecisionPolicyConfig — Cage/Tank별 정책 미설정 시 시스템 fallback.
// Cage/Tank별 override 는 current_tank_decision_policy 테이블 (C-4).
type DecisionPolicyConfig struct {
	// AutoExecuteEnabled — 기본값 false (가장 안전). Cage/Tank별 명시 enable 필요.
	AutoExecuteEnabled bool `yaml:"auto_execute_enabled" json:"auto_execute_enabled"`
	// GraceMinutes — 알림 후 자동 실행까지 대기 시간. 1 이상이어야 함.
	GraceMinutes int `yaml:"grace_minutes" json:"grace_minutes"`
}

// BaselineWorkerConfig — Cage/Tank baseline 자동 주기 평가 (docs/29 Phase 1.5).
// Worker 가 비활성이면 운영자가 [지금 평가하기] 로만 score 가 산출된다.
type BaselineWorkerConfig struct {
	Enabled         bool `yaml:"enabled" json:"enabled"`
	IntervalSec     int  `yaml:"interval_sec" json:"interval_sec"`
	InitialDelaySec int  `yaml:"initial_delay_sec" json:"initial_delay_sec"`
}

// FeedCycleConfig — feed cycle state machine worker 설정 (Phase 3).
type FeedCycleConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	DefaultGapMs            int  `yaml:"default_gap_ms" json:"default_gap_ms"`                         // 기본 관찰 gap (ms). 0 → 60000.
	DefaultPulseDurationMs  int  `yaml:"default_pulse_duration_ms" json:"default_pulse_duration_ms"`   // 기본 1 pulse 지속 시간 (ms). 0 → 5000.
	PulseDispatchTimeoutSec int  `yaml:"pulse_dispatch_timeout_sec" json:"pulse_dispatch_timeout_sec"` // 명령 큐잉 타임아웃. 0 → 30.
}

// CaptureConfig — G-2 7초 영상 캡처 워커 설정. cycle 시작 hook 에서 LRCN 입력 mp4 생성.
// mode=fixture: 본선 D-6 (RTSP 실기 없음). FixturePath 의 mp4 를 TempDir 로 복사.
// mode=rtsp:    강릉 D-18 이후. gst-launch-1.0 + rtspsrc 로 N초 캡처.
type CaptureConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Mode             string `yaml:"mode" json:"mode"`                           // "fixture" | "rtsp"
	FixturePath      string `yaml:"fixture_path" json:"fixture_path"`           // mode=fixture 일 때 source mp4
	DurationSeconds  int    `yaml:"duration_seconds" json:"duration_seconds"`   // 0 → 7
	TempDir          string `yaml:"temp_dir" json:"temp_dir"`                   // 0 → /tmp/bluei-edge/captures
	RetentionMinutes int    `yaml:"retention_minutes" json:"retention_minutes"` // 0 → 60. 음수면 cleanup 안 함

	// R6.1 — 상시 캡처 모드 (cycle hook 과 독립적, 7초 간격 연속).
	// continuous=true 시 ContinuousTanks 각 tank 에서 DurationSeconds 간격으로 mp4 캡처.
	// phase=feeding(cycle 활성) | baseline(cycle 외) 결정은 R6.2 decidePhase 콜백에서.
	Continuous            bool     `yaml:"continuous" json:"continuous"`
	ContinuousTanks       []string `yaml:"continuous_tanks" json:"continuous_tanks"`
	ContinuousModeSpacing int      `yaml:"continuous_mode_spacing_seconds" json:"continuous_mode_spacing_seconds"`

	// R17 — 디스크 사용률 임계 강제 cleanup. 0 = 비활성. 권장: 80~85
	MaxDiskPercent float64 `yaml:"max_disk_percent" json:"max_disk_percent"`
}

// WeightHistoryWorkerConfig — D-5 일일 추정 체중 스냅샷 worker 설정.
// active lifecycle Cage/Tank 마다 주기적으로 projection 산출 후 upsert.
type WeightHistoryWorkerConfig struct {
	Enabled         bool `yaml:"enabled" json:"enabled"`
	IntervalSec     int  `yaml:"interval_sec" json:"interval_sec"`
	InitialDelaySec int  `yaml:"initial_delay_sec" json:"initial_delay_sec"`
}

type SiteConfig struct {
	SiteID   string `yaml:"site_id"`
	Name     string `yaml:"name"`
	Timezone string `yaml:"timezone"`
	// 페어링 QR이 앱에 전달하는 사업장/farm-site 식별 정보(어장 자동구성에 사용).
	SiteType string  `yaml:"site_type"` // land_based_ras|hatchery|flow_through|pond|tank
	Operator string  `yaml:"operator"`  // 사업자/운영자명
	Lat      float64 `yaml:"lat"`
	Lng      float64 `yaml:"lng"`
}

type EdgeConfig struct {
	EdgeID  string `yaml:"edge_id"`
	Mode    string `yaml:"mode"` // development | production
	DataDir string `yaml:"data_dir"`
}

type RuntimeConfig struct {
	StartupTimeoutSec  int `yaml:"startup_timeout_sec"`
	ShutdownTimeoutSec int `yaml:"shutdown_timeout_sec"`
	ClockSkewWarnSec   int `yaml:"clock_skew_warning_sec"`
}

type APIConfig struct {
	BindHost          string     `yaml:"bind_host"`
	Port              int        `yaml:"port"`
	RequestTimeoutSec int        `yaml:"request_timeout_sec"`
	Auth              AuthConfig `yaml:"auth"`
}

type AuthConfig struct {
	Enabled          bool   `yaml:"enabled"`
	OperatorTokenEnv string `yaml:"operator_token_env"`
}

type StorageConfig struct {
	Driver        string `yaml:"driver"`
	SQLitePath    string `yaml:"sqlite_path"`
	MigrationMode string `yaml:"migration_mode"` // auto | explicit
}

type DevicesRef struct {
	ConfigPath string `yaml:"config_path"`
}

type TanksRef struct {
	ConfigPath string `yaml:"config_path"`
}

type LoggingRef struct {
	ConfigPath string `yaml:"config_path"`
}

type RulesRef struct {
	Enabled    bool   `yaml:"enabled"`
	ConfigPath string `yaml:"config_path"`
}

type CollectorConfig struct {
	Enabled  bool               `yaml:"enabled"`
	Adapters []CollectorAdapter `yaml:"adapters"`
}

type CollectorAdapter struct {
	ID          string       `yaml:"id"`
	Type        string       `yaml:"type"`
	IntervalSec int          `yaml:"interval_sec"`
	Metrics     []MockMetric `yaml:"metrics"`
}

type MockMetric struct {
	SensorID string  `yaml:"sensor_id"`
	DeviceID string  `yaml:"device_id"`
	Metric   string  `yaml:"metric"`
	Unit     string  `yaml:"unit"`
	Value    float64 `yaml:"value"`
}

type ControlConfig struct {
	Enabled                  bool             `yaml:"enabled"`
	AutomaticCommandsEnabled bool             `yaml:"automatic_commands_enabled"`
	RequireIdempotencyKey    bool             `yaml:"require_idempotency_key"`
	DefaultCommandTTLSec     int              `yaml:"default_command_ttl_sec"`
	Adapters                 []ControlAdapter `yaml:"adapters"`
}

type ControlAdapter struct {
	ID         string `yaml:"id"`
	Type       string `yaml:"type"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

type SyncConfig struct {
	Enabled        bool        `yaml:"enabled"`
	Mode           string      `yaml:"mode"`
	Endpoint       *string     `yaml:"endpoint"`
	AccessToken    string      `yaml:"access_token"`   // GX10 노드 토큰(POST /gx10/nodes 발급). Bearer 헤더로 전송.
	NodeCode       string      `yaml:"node_code"`      // GX10 노드 식별자. X-GX10-Node 헤더로 전송(토큰 대조 노드 매칭).
	DailyCronKST   string      `yaml:"daily_cron_kst"` // "HH:MM" KST. 빈 문자열이면 daily 트리거 비활성
	BatchSize      int         `yaml:"batch_size"`
	HTTPTimeoutSec int         `yaml:"http_timeout_sec"` // 0 → 60s
	Retry          RetryConfig `yaml:"retry"`
}

type RetryConfig struct {
	MinBackoffSec int `yaml:"min_backoff_sec"`
	MaxBackoffSec int `yaml:"max_backoff_sec"`
}

type InferenceConfig struct {
	Enabled bool         `yaml:"enabled"`
	Mode    string       `yaml:"mode"`
	Ollama  OllamaConfig `yaml:"ollama"`
	LRCN    LRCNConfig   `yaml:"lrcn"`
}

type OllamaConfig struct {
	Endpoint   string `yaml:"endpoint"`
	Model      string `yaml:"model"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

// LRCNConfig — G-3 vision_pipeline 의 LRCN HTTP client 설정.
// services/lrcn/server.py FastAPI 와 통신. Enabled=false 면 pipeline wire skip.
type LRCNConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`         // default false
	Endpoint   string `yaml:"endpoint" json:"endpoint"`       // default http://127.0.0.1:8081
	TimeoutSec int    `yaml:"timeout_sec" json:"timeout_sec"` // default 15 (CPU 1.17s/clip 측정)
}

// DevicesConfig is loaded from devices.config_path.
type DevicesConfig struct {
	Devices []DeviceEntry `yaml:"devices"`
}

type DeviceEntry struct {
	DeviceID     string   `yaml:"device_id"`
	DeviceType   string   `yaml:"device_type"`
	AdapterID    string   `yaml:"adapter_id"`
	Location     Location `yaml:"location"`
	Capabilities []string `yaml:"capabilities"`
}

type Location struct {
	AreaID         string `yaml:"area_id"`
	TankID         string `yaml:"tank_id"`
	PlatformTankID string `yaml:"platform_tank_id"`
}

// TanksConfig is loaded from tanks.config_path.
type TanksConfig struct {
	Tanks []TankProfile `yaml:"tanks"`
}

type TankProfile struct {
	TankID         string        `yaml:"tank_id" json:"tank_id"`
	PlatformTankID string        `yaml:"platform_tank_id" json:"platform_tank_id,omitempty"`
	DisplayName    string        `yaml:"display_name" json:"display_name"`
	Species        string        `yaml:"species" json:"species"`
	SystemType     string        `yaml:"system_type" json:"system_type"`
	VolumeM3       float64       `yaml:"volume_m3" json:"volume_m3,omitempty"`
	BiomassKg      float64       `yaml:"biomass_kg" json:"biomass_kg,omitempty"`
	FishCount      int           `yaml:"fish_count" json:"fish_count,omitempty"`
	AvgWeightG     float64       `yaml:"avg_weight_g" json:"avg_weight_g,omitempty"`
	TargetRanges   []MetricRange `yaml:"target_ranges" json:"target_ranges,omitempty"`
	// Phase 1b: map[string]any 로 변경. feeding_policy_override 등 nested object 보관.
	// 기존 YAML flat string 도 호환 (any 가 string 도 받음).
	Metadata map[string]any `yaml:"metadata" json:"metadata,omitempty"`
	// Phase 1 Group 도입. nullable — 기존 YAML 에 없어도 호환.
	GroupID string `yaml:"group_id" json:"group_id,omitempty"`
	// Phase 1 multi-tank 도메인 (migrations 009). 모두 nullable — backward compat.
	SiteID           string `yaml:"site_id" json:"site_id,omitempty"`
	WTGID            string `yaml:"wtg_id" json:"wtg_id,omitempty"`
	LotNo            string `yaml:"lot_no" json:"lot_no,omitempty"`
	LifecycleStage   string `yaml:"lifecycle_stage" json:"lifecycle_stage,omitempty"`     // 'fry' | 'fingerling' | 'growout'
	MutableLifecycle bool   `yaml:"mutable_lifecycle" json:"mutable_lifecycle,omitempty"` // 해상 케이지 true
	// Phase C-9 — Tank 물리 정보. 모두 nullable.
	// form_factor: 'round' | 'square' | 'rectangular'. 빈 문자열이면 미지정.
	FormFactor string  `yaml:"form_factor" json:"form_factor,omitempty"`
	DiameterM  float64 `yaml:"diameter_m" json:"diameter_m,omitempty"` // round 형 직경 (m)
	LengthM    float64 `yaml:"length_m" json:"length_m,omitempty"`     // square/rectangular 가로 (m)
	WidthM     float64 `yaml:"width_m" json:"width_m,omitempty"`       // rectangular 세로 (m)
	DepthM     float64 `yaml:"depth_m" json:"depth_m,omitempty"`       // 수심 (m)
}

type MetricRange struct {
	Metric string   `yaml:"metric" json:"metric"`
	Min    *float64 `yaml:"min" json:"min,omitempty"`
	Max    *float64 `yaml:"max" json:"max,omitempty"`
	Unit   string   `yaml:"unit" json:"unit"`
}

// RulesConfig is loaded from rules.config_path.
type RulesConfig struct {
	Rules []RuleEntry `yaml:"rules"`
}

type RuleEntry struct {
	RuleID    string        `yaml:"rule_id"`
	Enabled   bool          `yaml:"enabled"`
	Type      string        `yaml:"type"` // threshold | missing_data
	Metric    string        `yaml:"metric"`
	Subject   RuleSubject   `yaml:"subject"`
	Condition RuleCondition `yaml:"condition"`
	MaxAgeSec int           `yaml:"max_age_sec"`
	Alert     RuleAlert     `yaml:"alert"`
}

type RuleSubject struct {
	Kind string `yaml:"kind"`
	ID   string `yaml:"id"`
}

type RuleCondition struct {
	Operator    string     `yaml:"operator"`
	Value       float64    `yaml:"value"`
	Unit        string     `yaml:"unit"`
	DurationSec int        `yaml:"duration_sec"`
	Evaluation  EvalConfig `yaml:"evaluation"`
}

type EvalConfig struct {
	Mode       string `yaml:"mode"`
	MinSamples int    `yaml:"min_samples"`
}

type RuleAlert struct {
	AlertType string `yaml:"alert_type"`
	Severity  string `yaml:"severity"`
	Message   string `yaml:"message"`
}

// LoggingConfig is loaded from logging.config_path.
type LoggingConfig struct {
	Logging LoggingDetail `yaml:"logging"`
}

type LoggingDetail struct {
	Level  string        `yaml:"level"`
	Format string        `yaml:"format"`
	Output LoggingOutput `yaml:"output"`
}

type LoggingOutput struct {
	Stdout bool       `yaml:"stdout"`
	File   FileOutput `yaml:"file"`
}

type FileOutput struct {
	Enabled   bool   `yaml:"enabled"`
	Path      string `yaml:"path"`
	MaxSizeMB int    `yaml:"max_size_mb"`
	MaxFiles  int    `yaml:"max_files"`
}

// VisionAlgorithmsConfig is loaded from configs/vision-algorithms.example.yaml.
type VisionAlgorithmsConfig struct {
	AlgorithmLibraryVersion int                     `yaml:"algorithm_library_version" json:"algorithm_library_version"`
	Algorithms              []VisionAlgorithmEntry  `yaml:"algorithms" json:"algorithms"`
	TankApplications        []VisionTankApplication `yaml:"tank_applications" json:"tank_applications"`
}

type VisionAlgorithmEntry struct {
	VisionAlgorithmID string            `yaml:"vision_algorithm_id" json:"vision_algorithm_id"`
	DisplayName       string            `yaml:"display_name" json:"display_name"`
	Status            string            `yaml:"status" json:"status"`
	Species           string            `yaml:"species" json:"species"`
	GrowthStage       string            `yaml:"growth_stage" json:"growth_stage"`
	SizeRangeG        []float64         `yaml:"size_range_g" json:"size_range_g"`
	DensityRange      string            `yaml:"density_range" json:"density_range"`
	TankShape         string            `yaml:"tank_shape" json:"tank_shape"`
	TankSizeRangeM    []float64         `yaml:"tank_size_range_m" json:"tank_size_range_m"`
	CameraPosition    string            `yaml:"camera_position" json:"camera_position"`
	FeederType        string            `yaml:"feeder_type" json:"feeder_type"`
	Source            map[string]string `yaml:"source" json:"source,omitempty"`
	Outputs           []string          `yaml:"outputs" json:"outputs"`
	Model             map[string]any    `yaml:"model" json:"model,omitempty"`
	Validation        map[string]any    `yaml:"validation" json:"validation,omitempty"`
}

type VisionTankApplication struct {
	TankID                   string         `yaml:"tank_id" json:"tank_id"`
	CameraID                 string         `yaml:"camera_id" json:"camera_id"`
	AppliedVisionAlgorithmID string         `yaml:"applied_vision_algorithm_id" json:"applied_vision_algorithm_id"`
	CurrentGrowthStage       string         `yaml:"current_growth_stage" json:"current_growth_stage"`
	CurrentAvgWeightG        float64        `yaml:"current_avg_weight_g" json:"current_avg_weight_g"`
	CurrentDensityRange      string         `yaml:"current_density_range" json:"current_density_range"`
	ROI                      map[string]any `yaml:"roi" json:"roi,omitempty"`
	Calibration              map[string]any `yaml:"calibration" json:"calibration,omitempty"`
	LabelPolicy              map[string]any `yaml:"label_policy" json:"label_policy,omitempty"`
}
