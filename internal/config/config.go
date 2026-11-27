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
	TempControl          TempControlConfig         `yaml:"temp_control"`
	SeaTemp              SeaTempConfig             `yaml:"sea_temp"`
}

// SeaTempConfig — 외부 연안 수온(NIFS RISA) 수집. 취수 권역 수온 추세 참고용.
// Stations 비어있으면 수집기 비활성 (현장별 취수 권역이 다르므로 하드코딩 안 함).
type SeaTempConfig struct {
	Stations []string `yaml:"stations"` // NIFS 관측소 코드, 우선순위 순. 예: [bgna3, byy87]
}

// TempControlConfig — 자동 수온 제어 워커. 냉각기(히트펌프 switch) ON/OFF 로 목표수온 유지.
// 결정=optimize.RecommendTemp(히스테리시스·DO제약·temp_max 내장). 냉각기 ON=냉각(온도↓).
type TempControlConfig struct {
	Enabled      bool `yaml:"enabled"`        // false 면 워커 미기동
	IntervalSec  int  `yaml:"interval_sec"`   // 판단 주기. 0 → 60
	MinSwitchSec int  `yaml:"min_switch_sec"` // 냉각기 전환 최소 간격(압축기 보호). 0 → 600
	DryRun       bool `yaml:"dry_run"`        // true 면 결정 로그만(실작동 X)
	// 변화율 상한(max_ramp_c_per_hour)은 optimize.TempParams(운영자 UI 설정, /v1/optimize/temp/params)로 이동.
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
	// Metrics / ExcludeMetrics — payload 의 metric 값으로 룰 적용 범위를 좁힌다.
	// 같은 event_type 에 보관기간이 다른 룰을 둘 때(예: 수온만 장기 보존) 사용.
	// Metrics 가 있으면 해당 metric 만, ExcludeMetrics 가 있으면 해당 metric 을 제외.
	Metrics        []string `yaml:"metrics"`
	ExcludeMetrics []string `yaml:"exclude_metrics"`
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

	// onoff 급이기 (feeder control run/stop) — 경연 경로.
	FeederRateGramsPerSec float64  `yaml:"feeder_rate_g_per_sec" json:"feeder_rate_g_per_sec"`     // 배출률. 0 → 9.4.
	OnoffFeederLiveTanks  []string `yaml:"onoff_feeder_live_tanks" json:"onoff_feeder_live_tanks"` // 실작동 allowlist. 미포함 tank 는 dry-run. 기본 ["tank_02"].
	// 하드 안전 상한 — params 값보다 항상 우선. 초과 시 강제 종료.
	HardMaxSessionSec int     `yaml:"hard_max_session_sec" json:"hard_max_session_sec"` // 0 → 3600 (1h)
	HardMaxTotalGrams float64 `yaml:"hard_max_total_grams" json:"hard_max_total_grams"` // 0 → 25000
	MinGapMs          int     `yaml:"min_gap_ms" json:"min_gap_ms"`                     // onoff_feeder 1req/s 준수 하한. 0 → 1000.
	// WP-F — onoff_feeder(on/off) 마이크로펄스: run초 가동(≈rate×run초 g) → stop초 정지(섭취) 반복해 회당 목표량 채움.
	// 한 번에 다 주면 못 먹고 바닥 침전 → 짧게 나눠 준다. run/stop 초는 현장에서 조정.
	FeederRunSec  int `yaml:"feeder_run_sec" json:"feeder_run_sec"`   // 펄스 1회 가동(초). 0 → 1. (9.4g/s × run초 배출)
	FeederStopSec int `yaml:"feeder_stop_sec" json:"feeder_stop_sec"` // 펄스 사이 정지·섭취(초). 0 → 2. ⚠ 현장 조정.
	// 공급모터 리드타임(초) — 살포모터 기동 후 공급모터가 걸려 실배출이 시작되기까지 대기.
	// 실측 2026-07-04(탑뷰 영상): run 후 +11~13s 낙하 시작. 펄스 ON 시간 = spinup + 공급시간,
	// 배출량 추정은 공급시간만 사용. 0 → 10, -1 → 리드 없음(테스트/리드 없는 급이기).
	FeederSpinupSec int `yaml:"feeder_spinup_sec" json:"feeder_spinup_sec"`
	// stop 신뢰성 — onoff_feeder 1req/s 제한으로 단일 stop 이 429/네트워크로 드롭될 수 있음.
	// gap 동안 stop 을 이 간격(초)마다 반복 재전송(멱등)해 단일 드롭을 흡수. 0 → 2.
	FeederStopReinforceSec int `yaml:"feeder_stop_reinforce_sec" json:"feeder_stop_reinforce_sec"`
	// 보상 펄스 — gap 끝 라이브 무배출 판정이 "no" 확정이면 펄스 허용치 +1 (2026-07-08
	// 운영자 지시: 2초 유지 + 미급이 시 추가 펄스). 사이클당 상한. 0 → 비활성.
	CompensationMaxPerCycle int `yaml:"compensation_max_per_cycle" json:"compensation_max_per_cycle"`
	// 보상 만복 게이트 — 사이클 요동 피크(카메라 자기 진폭 P90 정규화, 0~1)가 이 값
	// 미만이면 저활동=만복으로 보고 보상을 폐기한다 (2026-07-20 운영자). 0 → 종전
	// gap끝 복귀도 게이트(compensationSatiationFactor) 유지. gap끝 스냅샷은 저진폭
	// 카메라(tank_02)에서 배고픔/저활동을 구분 못해(7-20 실측) 사이클 피크로 교체.
	CompensationSatiationPeakThreshold float64 `yaml:"compensation_satiation_peak_threshold" json:"compensation_satiation_peak_threshold"`
	// 만복 게이트 정규화 계수(카메라 P90)의 하한 — 과증폭 방지 (2026-07-31 운영자).
	// P90 이 최근 7일 자기 데이터라, 활동이 낮아지면 P90 도 낮아져 정규화값이 오히려
	// 올라가는 되먹임이 있다(실측: tank_02 0.885 → 0.454, 증폭 1.13 → 2.20배). 그러면
	// 저활동 사이클도 "활발"로 읽혀 만복 게이트가 무력화된다. 0 → 하한 없음(종전).
	SatiationRiseP90Floor float64 `yaml:"satiation_rise_p90_floor" json:"satiation_rise_p90_floor"`
	// 배출 판정 맥락 보정 — 2026-07-29~30 운영자 지시. 요동 상승폭 판정("no")은 사이클
	// 후반 만복 구간에서 정상 배출도 no 로 떨어뜨려(실측: no 비율 p1 10% → p7 90%)
	// 불필요한 보상 발사가 과급이를 만든다. 켜면 no 판정 뒤에 "자기기준비 + 반응창
	// 변동성"으로 차트 전체 맥락을 한 번 더 보고, 배출된 것으로 보이면 판정을
	// unclear 로 낮춘다(2026-07-30). 보상은 최종 판정을 그대로 따르므로 거짓 no 에
	// 발사되지 않는다. 상승폭 원본은 이벤트 raw_verdict 에 보존된다.
	// 실측(라벨 253펄스): 불필요 발사 37건 → 5건, 정밀도 24% → 64%.
	// false(기본) → 종전대로 no 판정 즉시 발사.
	CompensationGateEnabled bool `yaml:"compensation_gate_enabled" json:"compensation_gate_enabled"`
	// 게이트 임계 — 자기기준비(이번 반응창 평균 ÷ 초반 3주기 평균) 상한. 0 → 0.85.
	CompensationGateRelMax float64 `yaml:"compensation_gate_rel_max" json:"compensation_gate_rel_max"`
	// 게이트 임계 — 반응창 변동성(표준편차 ÷ 세션 기저) 상한. 0 → 0.04.
	CompensationGateVarMax float64 `yaml:"compensation_gate_var_max" json:"compensation_gate_var_max"`
	// 게이트 만복 전제조건 (2026-08-02 운영자 승인) — 게이트는 "후반의 no 는 어체가
	// 배불러 반응이 없는 것"이라는 전제로 완화한다. 그 사이클에서 실제 배출(yes)이
	// 아직 이 발수만큼 확인되지 않았다면 만복일 수 없으므로 게이트를 적용하지 않는다.
	// 급이기가 자주 멈추면서 전제가 깨진 사례에서 나온 조건 (8-02 tank_02: 배출 3발
	// 뿐인 사이클에서 게이트가 3건을 잘못 완화 → 결식 + 급이량 47g 과대계상).
	// 재판정 실측(최근 14일 라벨 742펄스): 0 → 검출 52%/정밀도 83%, 3 → 66%/75%.
	// K=3 까지 한계 교환비 약 1:1, K=4 부터 1:3.4 로 악화. 0 이면 종전대로 무조건 적용.
	CompensationGateMinConfirmed int `yaml:"compensation_gate_min_confirmed" json:"compensation_gate_min_confirmed"`
	// 섀도 중단 판정 v1 의 반응 임계 (2026-08-03). 미설정이면 코드 기본값
	// (shadow_stop.go 의 shadowLowReaction·shadowLowStreak).
	ShadowStop ShadowStopThresholds `yaml:"shadow_stop" json:"shadow_stop,omitempty"`
	// 수조별 덮어쓰기. 어체 밀도·체중·수온이 다르면 같은 값이 생물학적으로 다른 뜻이
	// 되므로 수조마다 다르게 둘 수 있어야 한다 (2026-08-03 운영자).
	//
	// ⚠ 종전 주석은 "자(스케일)는 수조별로 나누지 말 것"이었다. 카메라 차이가 덧셈형
	// (노이즈 바닥)이라 (peak − 기저) 뺄셈에서 상쇄된다는 전제였는데, **2026-08-05 에
	// 반증됐다** — 같은 자로 tank_01 0.25~0.32 / tank_02 0.41~0.54 가 나왔으나 운영자
	// 육안 순위는 정반대(tank_01 이 천장)였다. 상쇄는 불완전하고 화각·크롭 차이가
	// 곱셈형으로 남는다. 자는 이제 RiseAnchorByCamera 로 카메라별로 고정한다.
	//
	// ⚠ 배출 하한(clamp(peak×0.40, 0.12, 0.20))도 여기 없다. 사이클 최고값에 이미
	// 연동돼 스스로 적응하고, 0.12/0.20 경계는 측정 스케일이라 수조와 무관하다.
	//
	// 2026-08-03 실측(14일 배출확인 615펄스): tank_01 중앙 0.387 / tank_02 0.359,
	// Mann-Whitney z=+2.36 로 통계적 유의하나 차이는 0.029 로 작다. 공유 임계로도
	// 값 자체가 달라 수조별로 다르게 동작한다(tank_02 가 한 발 먼저 0.30 을 깬다).
	// 그래서 **구조만 열어두고 값은 공유 기본값으로 둔다** — 중단 정답(C)이 수조별로
	// 쌓여 대응 관계가 실제로 다르다고 확인될 때 여기에 값을 넣는다.
	ShadowStopByTank map[string]ShadowStopThresholds `yaml:"shadow_stop_by_tank" json:"shadow_stop_by_tank,omitempty"`
	// RiseAnchorByCamera — 카메라별 반응 천장(앵커). 단위는 **절대 상승폭**
	// = max(반응창) − 곡선 기저, 곡선은 판정과 같은 소스(live 우선, 없으면 clip).
	// 펄스 차트값 = 상승폭 / 앵커 이므로 1.00 = "이 카메라로 관측한 최고 반응".
	//
	// 2026-08-05 운영자 결정: "양식장 세팅 후 첫 급이 반응을 최고 기준으로 삼고 이후
	// 반응을 그 대비로 재서 급이 정도를 판단한다." 종전 rolling P90 은 (1) 자가 매일
	// 움직이고 (2) 활동이 낮아지면 P90 도 낮아져 판정이 관대해지는 되먹임이 있으며
	// (3) 애초에 분포 통계라 "이 수조가 낼 수 있는 최대"와 무관했다.
	//
	// 값은 **현장에서 획득한다** — 신규 현장 씨앗(docs/51)에 넣어 이식하지 말 것.
	// 캘리브레이션 표본은 낮 시간 정상 사이클로 한정한다. 일몰 무렵(16~17시) 클립은
	// 조명 급변으로 상승폭이 중앙값의 29~44배까지 튀어(실측 123.71·164.64) 앵커를
	// 그대로 파괴한다.
	//
	// 비어 있는 카메라는 종전 rolling P90 스케일로 폴백한다(미캘리브레이션 수조).
	RiseAnchorByCamera map[string]float64 `yaml:"rise_anchor_by_camera" json:"rise_anchor_by_camera,omitempty"`
	// 사이클 반응 지수 (clip_index) 기준선 — 2026-08-06. 사이클이 끝난 뒤 "이번 급이에
	// 얼마나 반응했나"를 강도·방향 두 값으로 요약해 **다음 사이클 판단**의 근거로 쓴다.
	//
	//	ClipIndexLevelP25ByTank  강도 하한. 수조별 최근 이력 P25.
	//	                         미만이면 방향과 무관하게 "활동이 너무 낮습니다".
	//	                         수조마다 절대 스케일이 달라 수조별로 둔다
	//	                         (7-23~8-05 실측 사이클 중앙: tank_01 3.16 / tank_02 2.51).
	//	ClipIndexLevelP75ByTank  강도 상한. 초과면 평소보다 배고팠던 사이클로 본다.
	//	                         다음 사이클 간격 권고(당김)에만 쓴다 — 상태 판정에는
	//	                         관여하지 않는다.
	//	ClipIndexDirectionLow    이 미만이면 "급격히 하락". 실측 P25.
	//	ClipIndexDirectionHigh   이 이상이면 "아직 지속". 실측 P75.
	//	                         방향은 비율이라 스케일이 상쇄돼 공통 기준을 쓴다
	//	                         (실측 중앙 두 수조 모두 0.72).
	//
	// ⚠ 이력에서 자동 재계산하지 않는다 — 활동이 전반적으로 낮아지면 기준선도 같이
	// 내려가 "낮음"이 영원히 안 뜨는 되먹임이 생긴다(rolling P90 에서 겪은 것과 같은 함정).
	// 재보정은 운영자가 값을 갱신하는 명시적 행위여야 한다.
	// 비우면 지수를 산출하지 않는다(보고에 clip_index 없음).
	ClipIndexLevelP25ByTank map[string]float64 `yaml:"clip_index_level_p25_by_tank" json:"clip_index_level_p25_by_tank,omitempty"`
	ClipIndexLevelP75ByTank map[string]float64 `yaml:"clip_index_level_p75_by_tank" json:"clip_index_level_p75_by_tank,omitempty"`
	ClipIndexDirectionLow   float64            `yaml:"clip_index_direction_low" json:"clip_index_direction_low,omitempty"`
	ClipIndexDirectionHigh  float64            `yaml:"clip_index_direction_high" json:"clip_index_direction_high,omitempty"`
	// r3(LRCN 탑뷰) 섀도 배출판정 — 2026-07-22. true 면 매 펄스 발사 시 판정용 짧은
	// 탑뷰 클립을 별도 캡처해 LRCN feeding head 로 채점하고 feed.pulse.r3_shadow 이벤트로
	// 적재한다. 급이 보상 결정은 무변(요동 유지) — r3 와 요동 판정의 일치를 라이브 관찰해
	// 주신호화 승격 전 임계를 보정하기 위함. RTSP 3연결(사이클클립+MJPEG+판정클립)이 늘어
	// 실장비 검증 후 켠다. false(기본) → 섀도 판정 비활성.
	R3ShadowEnabled bool `yaml:"r3_shadow_enabled" json:"r3_shadow_enabled"`
	// r3 섀도 verdict 임계 — feeding_score 가 이 값 미만이면 "no"(무배출), 이상이면 "yes".
	// 섀도 전용(보상 미영향). 0 → 0.5. 관측치(요동no vs r3 0.65+, 진짜 무배출 r3 낮음)로 보정.
	R3ShadowThreshold float64 `yaml:"r3_shadow_threshold" json:"r3_shadow_threshold"`
}

// ShadowStopThresholds — 섀도 중단 판정 v1 의 반응 임계 (feed_cycle/shadow_stop.go).
// 0 인 항목은 상위 기본값 → 코드 기본값 순으로 폴백한다.
type ShadowStopThresholds struct {
	// 낮은 반응 기준 (차트 스케일). 배출 확인된 펄스가 이 값 미만이면 저반응으로 센다.
	LowReaction float64 `yaml:"low_reaction" json:"low_reaction,omitempty"`
	// 저반응이 몇 발 연속이면 중단으로 볼지. ⚠ LowReaction 과 한 세트로 봐야 한다 —
	// 8-03 실측에서 임계만 보면 이르게 보였으나 연속 조건이 지연을 만들어 정답이었다.
	LowStreak int `yaml:"low_streak" json:"low_streak,omitempty"`
}

// CaptureConfig — G-2 7초 영상 캡처 워커 설정. cycle 시작 hook 에서 LRCN 입력 mp4 생성.
// mode=fixture: 본선 D-6 (RTSP 실기 없음). FixturePath 의 mp4 를 TempDir 로 복사.
// mode=rtsp:    강릉 D-18 이후. gst-launch-1.0 + rtspsrc 로 N초 캡처.
type CaptureConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Mode            string `yaml:"mode" json:"mode"`                         // "fixture" | "rtsp"
	FixturePath     string `yaml:"fixture_path" json:"fixture_path"`         // mode=fixture 일 때 source mp4
	DurationSeconds int    `yaml:"duration_seconds" json:"duration_seconds"` // 0 → 7
	TempDir         string `yaml:"temp_dir" json:"temp_dir"`                 // 0 → /tmp/bluei-edge/captures
	// ArchiveDir — 오래된 클립을 옮겨 둔 곳(NAS/외장). 비면 비활성.
	// 로컬 TempDir 에 파일이 없으면 여기서 같은 파일명을 찾는다 — 옮긴 뒤에도 재생·라벨링·
	// 학습 매니페스트가 그대로 동작하게 하는 폴백(2026-09-18). 이동은 별도 스크립트
	// (scripts/archive-captures-nas.sh)가 하고, 런타임은 읽기만 한다.
	ArchiveDir       string `yaml:"archive_dir" json:"archive_dir"`
	RetentionMinutes int    `yaml:"retention_minutes" json:"retention_minutes"` // 0 → 60. 음수면 cleanup 안 함

	// R6.1 — 상시 캡처 모드 (cycle hook 과 독립적, 7초 간격 연속).
	// continuous=true 시 ContinuousTanks 각 tank 에서 DurationSeconds 간격으로 mp4 캡처.
	// phase=feeding(cycle 활성) | baseline(cycle 외) 결정은 R6.2 decidePhase 콜백에서.
	Continuous            bool     `yaml:"continuous" json:"continuous"`
	ContinuousTanks       []string `yaml:"continuous_tanks" json:"continuous_tanks"`
	ContinuousModeSpacing int      `yaml:"continuous_mode_spacing_seconds" json:"continuous_mode_spacing_seconds"`

	// R17 — 디스크 사용률 임계 강제 cleanup. 0 = 비활성. 권장: 80~85
	MaxDiskPercent float64 `yaml:"max_disk_percent" json:"max_disk_percent"`

	// 관찰 꼬리 (2026-07-06 운영자 결정) — 급이 사이클 종료(confirm stop) 후에도 게이팅
	// 녹화를 이 시간(초)만큼 유지. 급이 후 먹이활동 지속·진정 도달 관찰용. 0 = 즉시 종료(기존).
	ObservationTailSec int `yaml:"observation_tail_sec" json:"observation_tail_sec"`
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

	// External cloud-API adapters (e.g. type "onoff_feeder"). Unused by mock adapters.
	// Secrets SHOULD come from env (ONOFF_FEEDER_ACCESS_KEY/ONOFF_FEEDER_SECRET_KEY); the
	// yaml fields are a fallback for dev only — do not commit real keys.
	BaseURL   string              `yaml:"base_url"`
	AccessKey string              `yaml:"access_key"`
	SecretKey string              `yaml:"secret_key"`
	DeviceID  string              `yaml:"device_id"`
	AreaID    string              `yaml:"area_id"`
	Tanks     []string            `yaml:"tanks"`      // local tank ids the (shared) readings apply to
	SensorMap []ExternalSensorMap `yaml:"sensor_map"` // external sensor id -> bluei metric

	// Chiller (냉각기) system-level monitoring — read-only. Emits status/current/
	// derived power to a system device (no tank). kW = ChillerPowerCoef * current(A).
	ChillerSwitchExtID     int     `yaml:"chiller_switch_ext_id"`     // switch list channel ext_id (on/off)
	ChillerCurrentSensorID int     `yaml:"chiller_current_sensor_id"` // sensor list id (Amperes)
	ChillerPowerCoef       float64 `yaml:"chiller_power_coef"`        // kW = coef * A (default 0.56 = √3·380·0.85/1000)
	ChillerDeviceID        string  `yaml:"chiller_device_id"`         // e.g. chiller_1
	ChillerTankID          string  `yaml:"chiller_tank_id"`           // system-level; empty = no tank
}

// ExternalSensorMap maps a 3rd-party API measurement point to a bluei metric.
type ExternalSensorMap struct {
	SensorID int    `yaml:"sensor_id"` // onoff_feeder measurement point id
	Metric   string `yaml:"metric"`    // e.g. water_temperature | ph | dissolved_oxygen
	Unit     string `yaml:"unit"`      // optional override; empty = use API unit
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

	// External control adapters (e.g. type "onoff_feeder_control"). Secrets SHOULD come
	// from env (ONOFF_FEEDER_ACCESS_KEY/ONOFF_FEEDER_SECRET_KEY); yaml is dev fallback only.
	BaseURL   string `yaml:"base_url"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

type SyncConfig struct {
	Enabled         bool        `yaml:"enabled"`
	Mode            string      `yaml:"mode"`
	Endpoint        *string     `yaml:"endpoint"`
	AccessToken     string      `yaml:"access_token"`      // GX10 노드 토큰(POST /gx10/nodes 발급). Bearer 헤더로 전송.
	NodeCode        string      `yaml:"node_code"`         // GX10 노드 식별자. X-GX10-Node 헤더로 전송(토큰 대조 노드 매칭).
	DailyCronKST    string      `yaml:"daily_cron_kst"`    // "HH:MM" KST. 빈 문자열이면 daily 트리거 비활성
	SyncIntervalMin int         `yaml:"sync_interval_min"` // 주기 동기화 간격(분). 0 → 비활성(daily 만). 부팅 직후 1회 + 이후 주기.
	BatchSize       int         `yaml:"batch_size"`
	HTTPTimeoutSec  int         `yaml:"http_timeout_sec"` // 0 → 60s
	Retry           RetryConfig `yaml:"retry"`
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
