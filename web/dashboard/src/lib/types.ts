// GET /v1/groups 응답 shape
export type Group = {
  group_id: string;
  name: string;
  description: string;
  color: string;
  // Phase 1b — 운영 정책 (feeding_policy 등) 보관. backend echo 그대로.
  metadata?: Record<string, unknown>;
};

// POST /v1/groups body — internal/api/groups.go groupRequest 와 일치.
// color 미입력 시 backend 는 빈 문자열 허용 (validateGroupRequest).
export type NewGroupBody = {
  group_id: string;
  name: string;
  description?: string;
  color?: string;
  metadata?: Record<string, unknown>;
};

// GET /v1/groups/{id}/tanks 응답 — 실제 백엔드가 반환하는 필드만 선언
export type TargetRange = {
  metric: string;
  min?: number;
  max?: number;
  unit: string;
};

export type TankFormFactor = 'round' | 'square' | 'rectangular';

export type Tank = {
  tank_id: string;
  display_name: string;
  species: string;
  system_type: string;
  volume_m3?: number;
  biomass_kg?: number;
  fish_count?: number;
  avg_weight_g?: number;
  target_ranges?: TargetRange[];
  metadata?: Record<string, unknown>;
  group_id?: string | null;
  // multi-tank fields (GET /v1/tanks)
  site_id?: string | null;
  wtg_id?: string | null;
  lot_no?: string | null;
  lifecycle_stage?: string | null;
  mutable_lifecycle?: boolean;
  // C-9 — Tank 물리 정보 (모두 nullable).
  form_factor?: TankFormFactor | '' | null;
  diameter_m?: number;
  length_m?: number;
  width_m?: number;
  depth_m?: number;
};

// ── Feed cycle types (GET /v1/feed-cycles) ────────────────────────────────────

// state machine internal (single cycle GET): idle, pulse_active, pulse_complete, gap_observation, cycle_complete
// list 응답 (GET /v1/feed-cycles): active / completed (cycle.completed_at 기반 이분법)
// stop 직후 즉시 응답: completing
export type FeedCycleStatus =
  | 'idle'
  | 'pulse_active'
  | 'pulse_complete'
  | 'gap_observation'
  | 'cycle_complete'
  | 'active'
  | 'completed'
  | 'completing';

export type FeedCycleMode = 'adaptive' | 'fixed';

export type FeedCycle = {
  cycle_id: string;
  tank_id: string;
  controller_id?: string | null;
  mode: FeedCycleMode;
  status: FeedCycleStatus;
  pulses_executed: number;
  total_amount_g: number;
  target_amount_g?: number | null;
  max_pulses?: number | null;
  started_at: string;
  completed_at?: string | null;
  termination_reason?: string | null;
  // C-1: operator_intents 연결 — backend 가 INSERT 시 저장 + 조회 시 reason inline.
  intent_id?: string | null;
  intent_reason?: string | null;
  // C-3l: arbiter_decisions.decision_id 역조회 echo — 운영자 dispute 첨부용.
  decision_id?: string | null;
  // Phase B: ESP32 살포(DAC1) / 공급(DAC2) 모터 출력 echo.
  // rpm 14~42 (0/undefined = 펌웨어 default), amount 0~255.
  speed_rpm?: number | null;
  amount?: number | null;
  // Phase 5 (load cell): HX711 weight event 누적. 0/null = stub fallback.
  actual_total_amount_g?: number | null;
  silo_depletion_warned?: boolean | null;
};

export type AdaptiveParams = {
  target_amount_g: number;
  max_pulses?: number;
  max_duration_min?: number;
  gap_ms?: number;
  pulse_duration_ms?: number;
  // Phase B: 살포모터 rpm (14~42, 0 = default), 공급모터 amount (0~255).
  speed_rpm?: number;
  amount?: number;
};

export type FixedParams = {
  pulse_duration_ms: number;
  gap_ms: number;
  total_pulses: number;
  // Phase B: 살포모터 rpm (14~42, 0 = default), 공급모터 amount (0~255).
  speed_rpm?: number;
  amount?: number;
};

export type NewCycleBody = {
  tank_id: string;
  controller_id?: string;
  mode: FeedCycleMode;
  params: AdaptiveParams | FixedParams;
  intent_id?: string; // 운영자 의도 메모 연결 (선택)
};

// ── Operator intents (Phase 5) ────────────────────────────────────────────────

export type IntentType = 'feed_now' | 'skip_cycle' | 'change_pattern' | 'general_note';

export type OperatorIntent = {
  intent_id: string;
  operator_id: string;
  tank_id?: string | null;
  related_cycle_id?: string | null;
  related_decision_id?: string | null;
  intent_type: IntentType;
  reason: string;
  recorded_at: string;
};

export type NewIntentBody = {
  tank_id?: string;
  related_cycle_id?: string;
  related_decision_id?: string;
  intent_type: IntentType;
  reason: string;
  context?: Record<string, unknown>;
};

// F.2 — LLM 종합 판단 결과 (operator_intents POST 응답 + context_json 에 저장).
export type LLMAnalysis = {
  can_apply: boolean;
  reason: string;
  scope: string;
  blocked_by: string[];
  adjustment: {
    max_daily_cycles_override?: number;
    bsf_mode_override?: 'aggressive' | 'standard' | 'conservative';
    get_factor?: number;
    min_interval_min?: number;
  };
  explanation_ko: string;
  confidence: number;
  model_used: string;
  fallback: boolean;
};

// ── Learned safety (Phase 4 C-3l) ─────────────────────────────────────────────
// Backend: internal/learned_safety + internal/api/learned_safety.go
// Schema: migrations/016_learned_safety.sql

// 운영자가 잘못된 결정에 이의를 제기할 때의 dispute 유형.
// backend 는 자유 문자열을 받으나, dashboard 표준 enum 으로 제한해 일관성 유지.
export type DisputeType = 'wrong_condition' | 'wrong_action' | 'wrong_timing';

export type Dispute = {
  dispute_id: string;
  decision_id: string;
  tank_id: string;
  dispute_type: string; // DisputeType 외 legacy 문자열도 받아들임
  comment: string;
  disputed_at: string;
  created_at: string;
};

export type NewDisputeBody = {
  decision_id: string;
  tank_id: string;
  dispute_type: DisputeType | string;
  comment: string;
};

// Learned rule severity (backend mining 은 현재 "high" 만 생성, 그러나 future-proof).
export type LearnedRuleSeverity = 'high' | 'medium' | 'low';

export type LearnedRule = {
  rule_id: string;
  condition_json: string; // JSON.stringify({ metric, operator, threshold, window_h })
  severity: LearnedRuleSeverity | string;
  source: string; // "operator_dispute" | "incident_log"
  confidence: number;
  hit_count: number;
  created_at: string;
  last_matched_at?: string | null;
  enabled: boolean;
};

// ── State-vector types (GET /v1/tanks/{id}/state-vector) ──────────────────────

export type WaterMetric = { value: number; unit: string; quality: string };
export type WaterMetrics = Record<string, WaterMetric>;

export type EquipmentDevice = {
  device_id: string;
  device_type: string;
  status: string;
  quality: string;
  last_seen_at: string;
};

export type AnomalyAlert = {
  alert_id: string;
  kind: string;
  severity: string;
  opened_at: string;
  message: string;
};

export type PendingDecision = {
  decision_id: string;
  decision_kind: string;
  route: string;
  reasoning: string;
  proposed_at: string;
  auto_execute_at?: string;
};

export type StateVector = {
  tank_id: string;
  group_id: string | null;
  timestamp: string;
  fish: {
    activity_score: number;
    last_observed_at: string;
    quality: string;
    notes: string[];
  };
  water: {
    metrics: WaterMetrics;
    last_observed_at: string;
    predictions: { available: boolean };
    notes: string[];
  };
  equipment: {
    devices: EquipmentDevice[];
    count: number;
    health_summary: string;
    notes?: string[];
  };
  feeding: {
    today_total_g: number;
    last_feeding_at: string;
    last_feeding_g: number;
    notes: string[];
  };
  biological_context: {
    species: string;
    fish_count: number;
    avg_weight_g: number;
    biomass_kg: number;
    system_type: string;
    source: string;
    estimated_avg_weight_g?: number;
    fcr_source?: string;
    expected_fcr?: number;
    weight_history_available?: boolean;
    notes: string[];
  };
  confidence: {
    adaptation_level: string;
    tank_confidence_score: number;
    components: {
      forecast_accuracy: number;
      baseline_stability: number;
      training_maturity: number;
      composite: number;
      adaptation_level: string;
      has_baseline: boolean;
      has_forecast: boolean;
      sample_count: number;
      notes: string[];
    };
    has_active_weights: boolean;
    notes: string[];
  };
  anomaly: {
    has_model: boolean;
    open_alerts?: AnomalyAlert[];
    notes: string[];
  };
  adaptation: {
    transition_detected: boolean;
    transition_reason?: string;
    notes: string[];
  };
  autonomous: {
    mode: string;
    reason: string;
    changed_at: string;
    changed_by: string;
    notes: string[];
  };
  decisions: {
    last_routed_at?: string;
    last_route?: string;
    last_decision_kind?: string;
    last_reasoning?: string;
    pending_count: number;
    pending?: PendingDecision[];
    auto_execute_enabled: boolean;
    grace_minutes: number;
    policy_source: string;
  };
};

// ── Camera types (GET /v1/cameras) ───────────────────────────────────────────

export type Camera = {
  camera_id: string;
  display_name: string;
  position?: string;             // C-11 deprecated — 새 등록은 mount_location + view_angle 사용
  status: string;
  host?: string;
  http_port?: number;
  rtsp_port?: number;
  tank_id?: string;
  metadata?: Record<string, unknown>;
  stream_profiles?: Record<string, unknown>;
  // C-11 camera model library link + 설치 정보.
  model_id?: string;
  mounting_height_m?: number;     // C-11 deprecated — 기준 모호. height_from_water_m 사용.
  underwater_depth_m?: number;
  // C-12 — 메타 정정. position 의미 분리 + 수면 기준 명시.
  mount_location?: MountLocation;
  view_angle?: ViewAngle;
  height_from_water_m?: number;  // 양수=수면 위, 음수=수중
  tilt_deg?: number;             // 0=수평, 90=수직 아래
  purpose?: CameraPurpose[];
};

// C-11 deprecated — backward compat 만. 새 코드는 MountLocation + ViewAngle 사용.
/** @deprecated use MountLocation + ViewAngle (C-12) */
export const CAMERA_POSITION_OPTIONS = [
  { value: 'overhead', label: 'enum.camPosOverhead' },
  { value: 'side', label: 'enum.camPosSide' },
  { value: 'feeding_zone', label: 'enum.camPosFeedingZone' },
  { value: 'water_intake', label: 'enum.camPosWaterIntake' },
  { value: 'outlet', label: 'enum.camPosOutlet' },
  { value: 'underwater', label: 'enum.camPosUnderwater' },
  { value: 'other', label: 'enum.camPosOther' },
] as const;
/** @deprecated use MountLocation + ViewAngle (C-12) */
export type CameraPosition = typeof CAMERA_POSITION_OPTIONS[number]['value'];

// C-12 — mount_location : 어디 위에 설치되었는지. (선택)
export type MountLocation =
  | 'feeder_zone'
  | 'water_intake'
  | 'water_outlet'
  | 'tank_center'
  | 'tank_side'
  | 'other';

export const MOUNT_LOCATION_LABELS: Record<MountLocation, string> = {
  feeder_zone: 'enum.mountFeederZone',
  water_intake: 'enum.mountWaterIntake',
  water_outlet: 'enum.mountWaterOutlet',
  tank_center: 'enum.mountTankCenter',
  tank_side: 'enum.mountTankSide',
  other: 'enum.mountOther',
};

// C-12 — view_angle : 카메라가 어떤 구도로 보는가. AI 알고리즘 선택의 핵심.
export type ViewAngle =
  | 'top_down'
  | 'oblique_top'
  | 'side_horizontal'
  | 'underwater_top'
  | 'underwater_side';

export const VIEW_ANGLE_LABELS: Record<ViewAngle, string> = {
  top_down: 'enum.viewTopDown',
  oblique_top: 'enum.viewObliqueTop',
  side_horizontal: 'enum.viewSideHorizontal',
  underwater_top: 'enum.viewUnderwaterTop',
  underwater_side: 'enum.viewUnderwaterSide',
};

// AI 적합도 hint — 운영자가 시점 의미를 파악하기 위함.
export const VIEW_ANGLE_AI_HINT: Record<ViewAngle, string> = {
  top_down: 'enum.viewHintTopDown',
  oblique_top: 'enum.viewHintObliqueTop',
  side_horizontal: 'enum.viewHintSideHorizontal',
  underwater_top: 'enum.viewHintUnderwaterTop',
  underwater_side: 'enum.viewHintUnderwaterSide',
};

// C-12 — purpose : AI 사용 목적. 다중 선택. backend purpose_json 와 동기화.
export type CameraPurpose =
  | 'behavior'
  | 'counting'
  | 'size_estimation'
  | 'feeding_detect'
  | 'operator_view'
  | 'intake_outlet_monitor';

export const CAMERA_PURPOSE_LABELS: Record<CameraPurpose, string> = {
  behavior: 'enum.purposeBehavior',
  counting: 'enum.purposeCounting',
  size_estimation: 'enum.purposeSizeEstimation',
  feeding_detect: 'enum.purposeFeedingDetect',
  operator_view: 'enum.purposeOperatorView',
  intake_outlet_monitor: 'enum.purposeIntakeOutletMonitor',
};

export const CAMERA_PURPOSE_TOOLTIPS: Record<CameraPurpose, string> = {
  behavior: 'enum.purposeTipBehavior',
  counting: 'enum.purposeTipCounting',
  size_estimation: 'enum.purposeTipSizeEstimation',
  feeding_detect: 'enum.purposeTipFeedingDetect',
  operator_view: 'enum.purposeTipOperatorView',
  intake_outlet_monitor: 'enum.purposeTipIntakeOutletMonitor',
};

// C-11 — 카메라 모델 라이브러리. camera_profiles.model_id 가 model_id 를 참조.
// lens_type=dual 일 때만 baseline_mm + stereo_calibration_json 의미.
export type CameraLensType = 'single' | 'dual' | 'fisheye' | 'ptz' | 'other';

export type CameraModel = {
  model_id: string;
  vendor: string;
  product_code: string;
  display_name: string;
  lens_type: CameraLensType;
  baseline_mm?: number;
  stereo_calibration_json?: string;
  resolution_w?: number;
  resolution_h?: number;
  fov_deg?: number;
  fps?: number;
  night_mode: boolean;
  protocols?: string[];
  notes?: string;
  created_at?: string;
  updated_at?: string;
};

export type NewCameraModelBody = {
  model_id: string;
  vendor: string;
  product_code: string;
  display_name: string;
  lens_type: CameraLensType;
  baseline_mm?: number;
  stereo_calibration_json?: string;
  resolution_w?: number;
  resolution_h?: number;
  fov_deg?: number;
  fps?: number;
  night_mode?: boolean;
  protocols?: string[];
  notes?: string;
};

export type AlertOpen = {
  alert_id: string;
  alert_type: string;
  severity: string; // 'info' | 'warning' | 'critical'
  status: string;   // 'open' | 'acknowledged'
  subject: { kind: string; id: string };
  rule_id?: string;
  message: string;
  evidence?: Record<string, unknown>;
  raised_at: string;
  updated_at: string;
};

// ── Multi-tank domain types ───────────────────────────────────────────────────

export type Farm = {
  farm_id: string;
  license_no: string;
  operator: string;
  certifications?: string[];
  sites?: string[];
  metadata?: Record<string, unknown>;
};

export type Site = {
  site_id: string;
  farm_id: string;
  site_type: 'land' | 'marine';
  name: string;
  address?: string;
  lat?: number;
  lon?: number;
  heading_deg?: number;
  timezone: string;
};

export type WaterTreatmentGroup = {
  wtg_id: string;
  site_id: string;
  name: string;
  tank_ids: string[];
  shared_equipment?: string[];
  intake_sensor_id?: string;
  outlet_sensor_id?: string;
  capacity?: number;
  feeding_policy?: Record<string, unknown>;
};

export type ControllerStatus = 'pending' | 'active' | 'disabled' | 'fault';

export type Controller = {
  controller_id: string;
  tank_id?: string;
  site_id?: string;
  actuator_id?: string;
  mac_address: string;
  ip_address?: string;
  firmware_version?: string;
  status: ControllerStatus;
  registered_at: string;
  last_seen_at?: string;
  commissioning?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
};

export type ControllerTestResult = {
  controller_id: string;
  results: {
    dac_ok: boolean;
    stop_ok: boolean;
    latency_ms: number;
    motor_ok?: boolean;
    has_weight?: boolean;
    weight_g?: number;
    tared?: boolean;
  };
};

export type Actuator = {
  device_id: string;
  device_type: string;
  site_id?: string;
  tank_id?: string;
  wtg_id?: string;
  controller_id?: string;
  model?: string;
  rated_power_w?: number;
  position_in_tank?: string;
  capabilities?: Record<string, unknown>;
};

export type Sensor = {
  sensor_id: string;
  sensor_type: string;
  site_id?: string;
  tank_id?: string;
  wtg_id?: string;
  position?: string;
  hardware?: string;
  capabilities?: string[];
};

export type LifecycleStage = {
  stage: string;
  min_weight_g?: number;
  max_weight_g?: number;
  duration_days?: number;
};

export type SpeciesProfile = {
  species: string;
  display_name: string;
  lifecycle_stages?: LifecycleStage[];
  waste_model?: Record<string, unknown>;
};

// ── 운영자 등록 폼 body (C-6a) ──────────────────────────────────────────────
// backend internal/api/domain_post.go 와 일치.

export type NewFarmBody = {
  farm_id: string;
  license_no?: string;
  operator: string;
  certifications?: string[];
  metadata?: Record<string, unknown>;
};

export type NewSiteBody = {
  site_id: string;
  farm_id: string;
  site_type: 'land' | 'marine';
  name: string;
  timezone?: string;
  // land 전용
  address?: string;
  lat?: number;
  lon?: number;
  // marine 전용
  heading_deg?: number;
  metadata?: Record<string, unknown>;
};

export type NewWTGBody = {
  wtg_id: string;
  site_id: string;
  name: string;
  tank_ids?: string[];
  intake_sensor?: string;
  outlet_sensor?: string;
  capacity?: {
    volume_m3?: number;
    nh3_processing_kg_per_h?: number;
    flow_rate_m3_per_h?: number;
  };
};

export type NewTankBody = {
  tank_id: string;
  display_name: string;
  species: string;
  system_type: string;
  volume_m3?: number;
  biomass_kg?: number;
  fish_count?: number;
  avg_weight_g?: number;
  target_ranges?: TargetRange[];
  group_id?: string;
  site_id?: string;
  wtg_id?: string;
  lot_no?: string;
  lifecycle_stage?: string;
  // C-9 — Tank 물리 정보.
  form_factor?: TankFormFactor;
  diameter_m?: number;
  length_m?: number;
  width_m?: number;
  depth_m?: number;
  // Phase 1b — feeding_policy_override 등 nested object 보관.
  metadata?: Record<string, unknown>;
};

// ── Tank lifecycle (POST /v1/tanks/{id}/stocking) ────────────────────────────

export type TankGrowthStage = 'fry' | 'juvenile' | 'growout' | 'broodstock';

export type NewStockingBody = {
  species: string;
  growth_stage: TankGrowthStage;
  initial_count: number;
  initial_avg_weight_g: number;
  initial_total_biomass_kg?: number;
  target_harvest_weight_g?: number;
  target_harvest_date?: string;
  source_hatchery?: string;
  supplier_id?: string;
  stocked_at?: string;
  operator_id?: string;
  lot_no?: string;
};

// ── 출하 (POST /v1/tanks/{id}/harvest) ──────────────────────────────────────
export type NewHarvestBody = {
  harvested_count: number;
  avg_weight_g: number;
  total_biomass_kg?: number;
  cycle_fcr?: number;
  harvested_at?: string;
  operator_id?: string;
  notes?: string;
};

// ── 거래처 (파트너) 마스터 (GET /v1/partners, POST /v1/partners) ─────────────
export type PartnerType = 'hatchery' | 'feed_supplier' | 'drug_supplier' | 'buyer' | 'other';

export type Partner = {
  partner_id: string;
  partner_type: PartnerType | string;
  name: string;
  business_no?: string;
  license_no?: string;
  contact?: string;
  address?: string;
  gln?: string;
  notes?: string;
};

export type NewPartnerBody = {
  partner_type: PartnerType | string;
  name: string;
  business_no?: string;
  license_no?: string;
  contact?: string;
  address?: string;
  gln?: string;
  notes?: string;
};

export type PartnersListResponse = {
  partners: Partner[];
  count: number;
};

export type TankLifecycleCurrent = {
  tank_id: string;
  active_stocking_id: string;
  species: string;
  growth_stage: string;
  initial_count: number;
  initial_avg_weight_g: number;
  target_harvest_weight_g?: number;
  target_harvest_date?: string;
  source_hatchery?: string;
  stocked_at: string;
  status: 'active' | 'harvested' | string;
  updated_at: string;
  lot_no?: string;
  parent_lot_no?: string;
};

export type TankLifecycleHistory = {
  type: 'stocking' | 'harvest' | string;
  recorded_at: string;
  payload: Record<string, unknown>;
};

export type TankLifecycleResponse = {
  tank_id: string;
  current: TankLifecycleCurrent | null;
  history: TankLifecycleHistory[];
};

// ── GDST/ASC 1st-mile traceability CTE body types ────────────────────────────

export type TreatmentType = 'antibiotic' | 'vaccine' | 'chemical' | 'probiotic' | 'anesthetic' | 'other';
export type TransferType = 'move' | 'split' | 'merge' | 'sale';

export type NewTreatmentBody = {
  treatment_type: TreatmentType;
  substance: string;
  dose?: number;
  dose_unit?: string;
  reason?: string;
  withdrawal_until?: string;
  administered_at?: string;
  operator_id?: string;
  notes?: string;
  item_id?: string;
  consumed_qty?: number;
};

export type NewMortalityBody = {
  dead_count: number;
  estimated_cause?: string;
  observed_at?: string;
  operator_id?: string;
  notes?: string;
};

export type NewTransferBody = {
  transfer_type: TransferType;
  to_tank_id?: string;       // sale 시 불필요
  to_lot_no?: string;
  moved_count: number;
  avg_weight_g?: number;
  total_biomass_kg?: number;
  transferred_at?: string;
  operator_id?: string;
  notes?: string;
  // sale 전용
  destination_name?: string;
  vehicle_info?: string;
};

export type TraceabilityCTE = {
  type: 'stocking' | 'feeding' | 'sampling' | 'treatment' | 'mortality' | 'transfer' | 'harvest';
  recorded_at: string;
  sequence: number;
  payload: Record<string, unknown>;
};

// ── GDST 서류 첨부 (GET /v1/tanks/{id}/traceability documents 배열) ─────────────
export type TraceabilityDoc = {
  document_id: string;
  tank_id: string;
  lot_no?: string | null;
  cte_type: string;
  doc_type: string;
  event_ref?: string | null;
  filename: string;
  mime_type: string;
  size_bytes: number;
  uploaded_by?: string | null;
  uploaded_at: string;
  download_url: string;
};

export type TankTraceabilityResponse = {
  tank_id: string;
  current: TankLifecycleCurrent | null;
  timeline: TraceabilityCTE[];
  count: number;
  documents: TraceabilityDoc[];
};

// ── 사료 기록 (POST /v1/feedings) ────────────────────────────────────────────
export type NewFeedingBody = {
  tank_id: string;
  feed_amount_g: number;
  feed_type?: string;
  feed_lot?: string;
  feed_supplier?: string;
  fed_at?: string;
  source?: string;
  recorded_by?: string;
  feeder_id?: string;
  item_id?: string;
  consumed_qty?: number;
};

export type NewSensorBody = {
  sensor_id: string;
  sensor_type: string;
  site_id?: string;
  tank_id?: string;
  wtg_id?: string;
  position?: string;
  hardware?: string;
  capabilities?: string[];
};

export type NewActuatorBody = {
  device_id: string;
  device_type: string;
  site_id?: string;
  tank_id?: string;
  wtg_id?: string;
  controller_id?: string;
  model?: string;
  rated_power_w?: number;
  capabilities?: string[];
};

export type NewSpeciesProfileBody = {
  species: string;
  display_name: string;
  source?: string;
};

// C-10 — POST /v1/cameras body. password plaintext 절대 금지 (백엔드 거부).
// password 는 별도 POST /v1/cameras/{id}/secret 로 따로 저장.
export type NewCameraBody = {
  camera_id: string;
  tank_id: string;
  display_name: string;
  vendor?: string;
  host?: string;
  rtsp_port?: number;
  http_port?: number;
  username?: string;
  password_secret_ref?: string;
  position?: string;             // C-11 deprecated — 새 등록은 mount_location + view_angle
  purpose?: CameraPurpose[];
  // C-11 — 모델 라이브러리 link + 설치 정보.
  model_id?: string;
  mounting_height_m?: number;    // C-11 deprecated
  underwater_depth_m?: number;
  // C-12 — 정정된 메타 (의미 분리 + 수면 기준 명시).
  mount_location?: MountLocation;
  view_angle?: ViewAngle;
  height_from_water_m?: number;
  tilt_deg?: number;
  // RTSP 경로 (선택). { sub: { path }, main: { path } }. 비우면 backend 가 vendor 기본 경로 fallback.
  stream_profiles?: Record<string, unknown>;
};

// ── Predictive forecast types (GET /v1/predictive/forecast) ──────────────────

export type PredictiveForecastStatus = 'ok' | 'caution' | 'breach';

export type PredictiveForecast = {
  wtg_id: string;
  capacity_kg_per_h: number;
  headroom_kg_per_h: number;
  recent_load_kg_per_h: number;
  threshold: number;
  status: PredictiveForecastStatus;
};

// ── Feeding schedule types (GET /v1/feeding-schedules) ───────────────────────

export type SchedulePattern = {
  pulse_duration_ms: number;
  gap_ms: number;
  total_pulses: number;
  target_amount_g?: number | null;
};

export type SchedulePriority = 'manual_override' | 'ai_advisory';

export type FeedingSchedule = {
  schedule_id: string;
  tank_ids: string[];
  cron: string;
  times: string[];
  pattern: SchedulePattern;
  priority: SchedulePriority;
  safety_gate: boolean;
  enabled: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
};

export type NewScheduleBody = {
  tank_ids: string[];
  times?: string[];
  cron?: string;
  pattern: SchedulePattern;
  priority?: SchedulePriority;
  enabled?: boolean;
  created_by?: string;
};

// ── Environmental snapshot types (C-3w) ───────────────────────────────────────

export type TidePhase = 'flood' | 'ebb' | 'slack' | 'high' | 'low' | '';

// GET /v1/environmental/current — 단일 snapshot
// GET /v1/environmental/history → { items, count }
// POST /v1/environmental/snapshot — 운영자 수동 주입
export type EnvironmentalSnapshot = {
  snapshot_id: string;
  site_id: string;
  wind_speed_ms?: number | null;
  wave_height_m?: number | null;
  tide_phase?: TidePhase | string;
  tide_minutes_to_low?: number | null;
  temperature_c?: number | null;
  recorded_at: string;
  source: string;
};

// 풍속/파고 임계값 — backend internal/environmental_safety/gate.go 와 동일.
// wind: > 12 m/s 차단, wave: > 2 m 차단.
// caution 영역은 frontend 추정 (backend 에는 caution 단계 없음).
export type EnvironmentalGateStatus = 'ok' | 'caution' | 'breach';

// ── Arbiter decisions (C-4) ──────────────────────────────────────────────────
// Backend: internal/api/arbiter.go — GET /v1/arbiter/decisions
// priority 는 backend arbiterPriorityLabel() 의 매핑 결과.
// decision 은 backend arbiterDecisionLabel() 의 verb.

export type ArbiterPriority =
  | 'manual_override'
  | 'ai_advisory'
  | 'ai_autonomous'
  | string; // legacy/unknown source fallback

export type ArbiterDecisionVerb = 'accept' | 'reject' | 'preempt' | string;

export type ArbiterDecision = {
  decision_id: string;
  tank_id: string;
  source: string;
  priority: ArbiterPriority;
  accepted: boolean;
  decision: ArbiterDecisionVerb;
  recorded_at: string;   // RFC3339
  submitted_at: string;  // RFC3339
  resulting_cycle_id?: string | null;
  existing_cycle_id?: string | null;
  preempted_cycle_id?: string | null;
  intent_id?: string | null;
  rejection_reason?: string | null;
};

// ── Safety gates aggregated status (C-5) ──────────────────────────────────────
// Backend: internal/api/safety_gates.go — GET /v1/safety-gates/status?tank_id=
// 게이트 3종 각각의 status 는 "ok" | "caution" | "breach" | "active" | "na".
//   - predictive(C-3p):    ok | caution | breach | na
//   - learned(C-3l):       ok | active | na   ("active" = enabled rules > 0)
//   - environmental(C-3w): ok | caution | breach | na  (land 는 항상 na)

export type SafetyGateStatus = 'ok' | 'caution' | 'breach' | 'active' | 'na' | string;

export type PredictiveGate = {
  status: SafetyGateStatus;
  headroom_kg_per_h?: number;
  capacity_kg_per_h?: number;
  summary: string;
};

export type LearnedGate = {
  status: SafetyGateStatus;
  rules_enabled: number;
  summary: string;
};

export type EnvironmentalGate = {
  status: SafetyGateStatus;
  summary: string;
  wind_speed_ms?: number | null;
  wave_height_m?: number | null;
};

export type SafetyGatesStatus = {
  tank_id: string;
  site_id: string;
  wtg_id: string;
  site_type: 'land' | 'marine' | string;
  predictive: PredictiveGate;
  learned: LearnedGate;
  environmental: EnvironmentalGate;
};

// ── Weight history types (GET /v1/tanks/{id}/weight-history) ──────────────────

export type WeightHistorySnapshot = {
  snapshot_date: string;
  estimated_avg_weight_g: number;
  anchor_source: string;
  expected_fcr: number;
  fcr_source: string;
  cumulative_feed_g: number;
  quality: string;
};

export type WeightHistoryResponse = {
  count: number;
  days: number;
  tank_id?: string;
  snapshots: WeightHistorySnapshot[];
};

// ──────────────────────────────────────────────────────────────────────────────
// C-13a — 센서 모델 라이브러리 + 인스턴스 메타. (카메라 C-11/C-12 패턴 동일 적용)
// sensors.model_id 가 sensor_models.model_id 를 참조.
// SensorMountLocation : 어디 위에 설치 (tank vs wtg 컨텍스트 모두 수용).
// MeasurementRole     : 운영자 의도 (다중 선택).
// MeasurementType     : 모델이 측정하는 종류 (마이그 024 CHECK 와 동일).
// ──────────────────────────────────────────────────────────────────────────────

export type MeasurementType =
  | 'water_temperature'
  | 'ph'
  | 'dissolved_oxygen'
  | 'unionized_ammonia'
  | 'nitrate'
  | 'nitrite'
  | 'carbon_dioxide'
  | 'total_suspended_solids'
  | 'turbidity'
  | 'salinity'
  | 'flow_rate'
  | 'pump_pressure'
  | 'water_level'
  | 'light_intensity'
  | 'feed_weight'
  | 'oxygen_saturation'
  | 'redox'
  | 'conductivity'
  | 'multi'
  | 'other';

export const MEASUREMENT_TYPE_LABELS: Record<MeasurementType, string> = {
  water_temperature: 'enum.measWaterTemperature',
  ph: 'pH',
  dissolved_oxygen: 'enum.measDissolvedOxygen',
  unionized_ammonia: 'enum.measUnionizedAmmonia',
  nitrate: 'enum.measNitrate',
  nitrite: 'enum.measNitrite',
  carbon_dioxide: 'enum.measCarbonDioxide',
  total_suspended_solids: 'enum.measTotalSuspendedSolids',
  turbidity: 'enum.measTurbidity',
  salinity: 'enum.measSalinity',
  flow_rate: 'enum.measFlowRate',
  pump_pressure: 'enum.measPumpPressure',
  water_level: 'enum.measWaterLevel',
  light_intensity: 'enum.measLightIntensity',
  feed_weight: 'enum.measFeedWeight',
  oxygen_saturation: 'enum.measOxygenSaturation',
  redox: 'enum.measRedox',
  conductivity: 'enum.measConductivity',
  multi: 'enum.measMulti',
  other: 'enum.measOther',
};

export type SensorProtocol =
  | 'modbus'
  | 'rs485'
  | 'rs232'
  | '4-20ma'
  | '0-10v'
  | 'i2c'
  | 'sdi-12'
  | 'http'
  | 'mqtt'
  | 'other';

export type SensorWetDry = 'wet_probe' | 'inline' | 'dry_mount' | 'other';

export type SensorModel = {
  model_id: string;
  vendor: string;
  product_code: string;
  display_name: string;
  measurement_type: MeasurementType;
  unit: string;
  range_min?: number;
  range_max?: number;
  accuracy_value?: number;
  accuracy_unit?: string;
  response_time_s?: number;
  protocol?: SensorProtocol;
  calibration_interval_days?: number;
  wet_dry?: SensorWetDry;
  notes?: string;
  created_at?: string;
  updated_at?: string;
};

export type NewSensorModelBody = {
  model_id: string;
  vendor: string;
  product_code: string;
  display_name: string;
  measurement_type: MeasurementType;
  unit: string;
  range_min?: number;
  range_max?: number;
  accuracy_value?: number;
  accuracy_unit?: string;
  response_time_s?: number;
  protocol?: SensorProtocol;
  calibration_interval_days?: number;
  wet_dry?: SensorWetDry;
  notes?: string;
};

// 카메라 mount_location (MountLocation) 과 별개 — 센서는 tank/wtg 컨텍스트가 다름.
export type SensorMountLocation =
  | 'water_intake'
  | 'water_outlet'
  | 'tank_top'
  | 'tank_bottom'
  | 'mid_depth'
  | 'feeder_inline'
  | 'pipe_inline'
  | 'wtg_intake'
  | 'wtg_outlet'
  | 'other';

export const MOUNT_LOCATION_SENSOR_LABELS: Record<SensorMountLocation, string> = {
  water_intake: 'enum.smountWaterIntake',
  water_outlet: 'enum.smountWaterOutlet',
  tank_top: 'enum.smountTankTop',
  tank_bottom: 'enum.smountTankBottom',
  mid_depth: 'enum.smountMidDepth',
  feeder_inline: 'enum.smountFeederInline',
  pipe_inline: 'enum.smountPipeInline',
  wtg_intake: 'enum.smountWtgIntake',
  wtg_outlet: 'enum.smountWtgOutlet',
  other: 'enum.smountOther',
};

// 운영자 의도 — 다중 선택.
export type MeasurementRole =
  | 'safety_gate_c3'
  | 'feeding_decision'
  | 'water_quality_monitoring'
  | 'predictive_input'
  | 'operator_only';

export const MEASUREMENT_ROLE_LABELS: Record<MeasurementRole, string> = {
  safety_gate_c3: 'enum.roleSafetyGateC3',
  feeding_decision: 'enum.roleFeedingDecision',
  water_quality_monitoring: 'enum.roleWaterQualityMonitoring',
  predictive_input: 'enum.rolePredictiveInput',
  operator_only: 'enum.roleOperatorOnly',
};

export const MEASUREMENT_ROLE_TOOLTIPS: Record<MeasurementRole, string> = {
  safety_gate_c3: 'enum.roleTipSafetyGateC3',
  feeding_decision: 'enum.roleTipFeedingDecision',
  water_quality_monitoring: 'enum.roleTipWaterQualityMonitoring',
  predictive_input: 'enum.roleTipPredictiveInput',
  operator_only: 'enum.roleTipOperatorOnly',
};

// C-13a — Sensor 인스턴스 확장 필드. 기존 Sensor 타입에 신규 필드 추가.
// (앞쪽 Sensor 타입은 변경하지 않고, 동일 이름 SensorWithMeta 로 superset 제공.)
export type SensorWithMeta = Sensor & {
  model_id?: string;
  mount_location?: SensorMountLocation;
  installed_depth_m?: number;
  measurement_role?: MeasurementRole[];
  calibration_last_at?: string;
  calibration_due_at?: string;
};

// NewSensorBody (앞쪽 정의) 와 호환되는 superset — 신규 필드 multi-select / 모델 link.
export type NewSensorBodyWithMeta = NewSensorBody & {
  model_id?: string;
  mount_location?: SensorMountLocation;
  installed_depth_m?: number;
  measurement_role?: MeasurementRole[];
  calibration_last_at?: string;
  calibration_due_at?: string;
};

// ──────────────────────────────────────────────────────────────────────────────
// C-13b — 액추에이터 모델 라이브러리 + 인스턴스 메타. (카메라 C-11/C-12 패턴 동일 적용)
// actuators.model_id 가 actuator_models.model_id 를 참조.
// ActuatorMountLocation : 어디 위에 설치 (tank vs wtg 컨텍스트 모두 수용).
// SafetyRole            : 운영 안전 의도 multi-select — Mode Arbiter / C-3 게이트 연결.
// OperatingMode         : auto/manual/standby/maintenance/fault.
// DeviceCategory        : 마이그 CHECK 와 일치하는 16 카테고리.
// ControlMethod         : 제어 방식 enum (on_off/pwm/4-20ma/0-10v/modbus/mqtt/esp32_controller/manual/other).
// ──────────────────────────────────────────────────────────────────────────────

// 액추에이터 카테고리. backend 마이그 (025 + 029) CHECK 와 동일.
// circulation_pump / heat_pump / air_pump 는 C-13b 카테고리별 spec 폼 (마이그 029) 추가분.
export type DeviceCategory =
  | 'pump'
  | 'circulation_pump'
  | 'heat_pump'
  | 'air_pump'
  | 'aerator'
  | 'oxygen_cone'
  | 'heater'
  | 'chiller'
  | 'uv_sterilizer'
  | 'led_light'
  | 'feeder'
  | 'valve'
  | 'biofilter'
  | 'drum_filter'
  | 'dosing_pump'
  | 'ozonator'
  | 'blower'
  | 'skimmer'
  | 'other';

export const DEVICE_CATEGORY_LABELS: Record<DeviceCategory, string> = {
  pump: 'enum.devPump',
  circulation_pump: 'enum.devCirculationPump',
  heat_pump: 'enum.devHeatPump',
  air_pump: 'enum.devAirPump',
  aerator: 'enum.devAerator',
  oxygen_cone: 'enum.devOxygenCone',
  heater: 'enum.devHeater',
  chiller: 'enum.devChiller',
  uv_sterilizer: 'enum.devUvSterilizer',
  led_light: 'enum.devLedLight',
  feeder: 'enum.devFeeder',
  valve: 'enum.devValve',
  biofilter: 'enum.devBiofilter',
  drum_filter: 'enum.devDrumFilter',
  dosing_pump: 'enum.devDosingPump',
  ozonator: 'enum.devOzonator',
  blower: 'enum.devBlower',
  skimmer: 'enum.devSkimmer',
  other: 'enum.devOther',
};

// 제어 방식 enum.
export type ControlMethod =
  | 'on_off'
  | 'pwm'
  | '4-20ma'
  | '0-10v'
  | 'modbus'
  | 'mqtt'
  | 'esp32_controller'
  | 'manual'
  | 'other';

export const CONTROL_METHOD_LABELS: Record<ControlMethod, string> = {
  on_off: 'enum.ctrlOnOff',
  pwm: 'enum.ctrlPwm',
  '4-20ma': 'enum.ctrl420ma',
  '0-10v': 'enum.ctrl010v',
  modbus: 'Modbus',
  mqtt: 'MQTT',
  esp32_controller: 'enum.ctrlEsp32Controller',
  manual: 'enum.ctrlManual',
  other: 'enum.ctrlOther',
};

// 액추에이터 mount_location — 어디 설치되었는가.
export type ActuatorMountLocation =
  | 'tank_inlet'
  | 'tank_outlet'
  | 'tank_center'
  | 'tank_bottom'
  | 'wtg_intake'
  | 'wtg_outlet'
  | 'pipe_inline'
  | 'feeder_zone'
  | 'external'
  | 'other';

export const ACTUATOR_MOUNT_LOCATION_LABELS: Record<ActuatorMountLocation, string> = {
  tank_inlet: 'enum.amountTankInlet',
  tank_outlet: 'enum.amountTankOutlet',
  tank_center: 'enum.amountTankCenter',
  tank_bottom: 'enum.amountTankBottom',
  wtg_intake: 'enum.amountWtgIntake',
  wtg_outlet: 'enum.amountWtgOutlet',
  pipe_inline: 'enum.amountPipeInline',
  feeder_zone: 'enum.amountFeederZone',
  external: 'enum.amountExternal',
  other: 'enum.amountOther',
};

// safety_role — 운영 안전 의도. 다중 선택. Mode Arbiter / C-3 안전 게이트 연결.
// oxygen_critical / circulation_critical : 정지 시 즉시 critical 알람.
// feed_actuator : 운영자 의도와 직결 (Operator Intent / Arbiter 우선순위).
export type SafetyRole =
  | 'oxygen_critical'
  | 'circulation_critical'
  | 'feed_actuator'
  | 'filtration'
  | 'heating_cooling'
  | 'lighting'
  | 'oxygen_backup'
  | 'disinfection'
  | 'non_critical';

export const SAFETY_ROLE_LABELS: Record<SafetyRole, string> = {
  oxygen_critical: 'enum.safetyOxygenCritical',
  circulation_critical: 'enum.safetyCirculationCritical',
  feed_actuator: 'enum.safetyFeedActuator',
  filtration: 'enum.safetyFiltration',
  heating_cooling: 'enum.safetyHeatingCooling',
  lighting: 'enum.safetyLighting',
  oxygen_backup: 'enum.safetyOxygenBackup',
  disinfection: 'enum.safetyDisinfection',
  non_critical: 'enum.safetyNonCritical',
};

// 운영자가 chip 위에서 확인할 수 있는 short hint — AI/Arbiter 연결 의미.
export const SAFETY_ROLE_HINTS: Record<SafetyRole, string> = {
  oxygen_critical: 'enum.safetyHintOxygenCritical',
  circulation_critical: 'enum.safetyHintCirculationCritical',
  feed_actuator: 'enum.safetyHintFeedActuator',
  filtration: 'enum.safetyHintFiltration',
  heating_cooling: 'enum.safetyHintHeatingCooling',
  lighting: 'enum.safetyHintLighting',
  oxygen_backup: 'enum.safetyHintOxygenBackup',
  disinfection: 'enum.safetyHintDisinfection',
  non_critical: 'enum.safetyHintNonCritical',
};

// operating_mode — 현재 운전 상태.
export type OperatingMode = 'auto' | 'manual' | 'standby' | 'maintenance' | 'fault';

export const OPERATING_MODE_LABELS: Record<OperatingMode, string> = {
  auto: 'enum.opmodeAuto',
  manual: 'enum.opmodeManual',
  standby: 'enum.opmodeStandby',
  maintenance: 'enum.opmodeMaintenance',
  fault: 'enum.opmodeFault',
};

// power_type — 전원 종류. DC = 전력 W, AC(단상/3상) = 전력 kW (docs/wip 결정 2026-05-24).
export type PowerType = 'single_phase' | 'three_phase' | 'dc';

export const POWER_TYPE_LABELS: Record<PowerType, string> = {
  single_phase: 'enum.powerSinglePhase',
  three_phase: 'enum.powerThreePhase',
  dc: 'DC',
};

// actuator_models 라이브러리 row.
export type ActuatorModel = {
  model_id: string;
  vendor: string;
  product_code: string;
  display_name: string;
  device_category: DeviceCategory;
  rated_power_w?: number;
  capacity_value?: number;
  capacity_unit?: string;
  control_method?: ControlMethod;
  response_time_s?: number;
  control_range_min?: number;
  control_range_max?: number;
  control_range_unit?: string;
  consumable_replacement_days?: number;
  notes?: string;
  // C-13b — 카테고리별 spec (공통 컬럼으로 표현 안 되는 고유 spec). backend echo.
  category_specs?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};

// POST /v1/actuator-models — request body.
export type NewActuatorModelBody = {
  model_id: string;
  vendor: string;
  product_code: string;
  display_name: string;
  device_category: DeviceCategory;
  rated_power_w?: number;
  capacity_value?: number;
  capacity_unit?: string;
  control_method?: ControlMethod;
  response_time_s?: number;
  control_range_min?: number;
  control_range_max?: number;
  control_range_unit?: string;
  consumable_replacement_days?: number;
  notes?: string;
  // C-13b — 카테고리별 spec 객체. backend 가 카테고리별 필수 키 검증 (422 거부).
  category_specs?: Record<string, number | string>;
};

// Actuator 인스턴스 확장 필드 — 앞쪽 Actuator 타입 변경 없이 superset 제공.
export type ActuatorWithMeta = Actuator & {
  model_id?: string;
  mount_location?: ActuatorMountLocation;
  safety_role?: SafetyRole[];
  operating_mode?: OperatingMode;
  alarm_thresholds?: Record<string, unknown>;
  last_maintenance_at?: string;
  next_maintenance_due_at?: string;
};

// NewActuatorBody superset — 모델 link + 안전/운영 메타.
export type NewActuatorBodyWithMeta = NewActuatorBody & {
  model_id?: string;
  mount_location?: ActuatorMountLocation;
  safety_role?: SafetyRole[];
  operating_mode?: OperatingMode;
  alarm_thresholds?: Record<string, unknown>;
  last_maintenance_at?: string;
  next_maintenance_due_at?: string;
};

// ──────────────────────────────────────────────────────────────────────────────
// Vision (LRCN/검출기) API — 인공지능 관리 sub-tab 용. backend: /v1/vision/*.
// ──────────────────────────────────────────────────────────────────────────────

export type VisionAlgorithmStatus =
  | 'candidate' | 'validated' | 'deprecated' | string;

export type VisionAlgorithm = {
  vision_algorithm_id: string;
  display_name: string;
  status: VisionAlgorithmStatus;
  species?: string;
  growth_stage?: string;
  size_range_g?: [number, number];
  density_range?: string;
  tank_shape?: string;
  tank_size_range_m?: [number, number];
  camera_position?: string;
  feeder_type?: string;
  source?: { prototype?: string; reference_commit?: string };
  outputs?: string[];
  model?: {
    feature_extractor?: string;
    requires_operator_labels?: boolean;
    runtime?: string;
    weights_path?: string;
  };
  validation?: Record<string, unknown>;
};

export type VisionAlgorithmsResponse = {
  algorithm_library_version: number;
  count: number;
  items: VisionAlgorithm[];
};

// Manifest state — local-ai/models/active/manifest.json 의 algorithm 항목.
export type VisionAlgorithmHistoryEntry = {
  weights_path: string;
  job_id?: string;
  applied_at?: string;
  removed_at?: string;
  operator_id?: string;
};

export type VisionAlgorithmActiveState = {
  active_weights_path: string;
  active_job_id?: string;
  applied_at?: string;
  operator_id?: string;
  history?: VisionAlgorithmHistoryEntry[];
};

export type VisionAlgorithmApplyResponse = {
  ok: boolean;
  sequence: number;
  applied: {
    action: 'promote' | 'rollback';
    algorithm_id: string;
    previous_algorithm_id?: string;
    job_id?: string;
    operator_id: string;
    applied_at: string;
  };
  active: VisionAlgorithmActiveState;
  note?: string;
};

export type VisionTankApplication = {
  tank_id: string;
  camera_id: string;
  applied_vision_algorithm_id: string;
  current_growth_stage?: string;
  current_avg_weight_g?: number;
  current_density_range?: string;
  roi?: Record<string, unknown>;
  calibration?: Record<string, unknown>;
  label_policy?: Record<string, unknown>;
};

export type VisionTankApplicationsResponse = {
  count: number;
  items: VisionTankApplication[];
};

export type VisionObservation = {
  observation_id?: string;
  camera_id?: string;
  tank_id?: string;
  recorded_at?: string;
  model_version?: string;
  scores?: Record<string, number>;
  notes?: string;
  [key: string]: unknown;
};

export type VisionObservationsResponse = {
  count: number;
  items: VisionObservation[];
  tank_id?: string;
};

// G-4 cycle 종료 dispute auto-modal.
export type VisionDisputeVerdict = 'correct' | 'wrong' | 'unsure';

export type VisionObservationDisputeBody = {
  camera_id: string;
  tank_id: string;
  operator_id: string;
  verdict: VisionDisputeVerdict;
  reason?: string;
  operator_score?: number; // G-4 단일 슬라이더 (legacy, 호환)
  pre_score?: number;      // R5: 급이 전 안정성 0~1
  during_score?: number;   // R5: 급이 도중 반응 0~1
  clip_ref?: string;       // R5: 라벨이 가리키는 observation clip mp4 path
  algorithm_id?: string;   // R11: 어느 LRCN 모델용 라벨인지
};

export type VisionObservationDispute = {
  dispute_id: string;
  observation_id: string;
  camera_id: string;
  tank_id?: string;
  operator_id: string;
  verdict: VisionDisputeVerdict;
  reason?: string;
  operator_score?: number;
  pre_score?: number;
  during_score?: number;
  clip_ref?: string;
  disputed_at: string;
};

export type VisionObservationDisputeResponse = {
  ok: boolean;
  sequence: number;
  dispute: VisionObservationDispute;
};

export type VisionTrainingJob = {
  job_id: string;
  algorithm_id?: string;
  // kind 누락 시 vision-detector. tank-baseline / water-forecast 도 동일 endpoint 사용.
  kind?: 'vision-detector' | 'tank-baseline' | 'water-forecast' | string;
  tank_id?: string;
  status?: string;
  progress?: number;
  // 5/9 ai-training.js 가 사용하는 진행 표시 필드
  progress_pct?: number;
  stage_label?: string;
  candidate_path?: string;
  error?: string;
  started_at?: string;
  finished_at?: string;
  updated_at?: string;
  // 시험 결과 신호등 (5/9 renderResult)
  metrics?: {
    label_correlation?: number;
    inference_latency_ms?: number;
    visibility_valid_rate?: number;
    [key: string]: number | undefined;
  };
  [key: string]: unknown;
};

// POST /v1/vision/training/jobs body
export type VisionTrainingStartBody = {
  algorithm_id?: string;
  // R11: lrcn-finetune 추가
  kind?: 'vision-detector' | 'tank-baseline' | 'water-forecast' | 'lrcn-finetune';
  tank_id?: string;
};

// 박스 라벨링 — normalized [0,1] 좌표
export type VisionBootstrapBox = {
  x: number;
  y: number;
  w: number;
  h: number;
  class: 'fish' | 'food' | 'exclude';
};

// POST /v1/vision/bootstrap/labels body
export type VisionBootstrapSaveBody = {
  camera_id: string;
  snapshot_ref?: string;
  operator_id?: string;
  boxes: VisionBootstrapBox[];
  // base64 JPEG (data:image/jpeg;base64,...) — 저장 시 frame 같이 보냄
  image?: string;
};

// R6.3 — LRCN 지도학습용 영상 풀.
export type VisionTrainingPoolPhase = 'feeding' | 'baseline';

export type VisionTrainingPoolItem = {
  sequence: number;
  event_id: string;
  recorded_at: string;
  payload: {
    clip_id: string;
    camera_id: string;
    tank_id?: string;
    reason: string;
    started_at: string;
    ended_at: string;
    uri: string;          // mp4 path (backend 가 /v1/vision/observations/.../clip.mp4 와는 별도 — R6 후속 endpoint)
    mime_type: string;
    size_bytes: number;
    evidence?: {
      phase?: VisionTrainingPoolPhase;
      cycle_id?: string;
      [k: string]: unknown;
    };
  };
};

export type VisionTrainingPoolResponse = {
  phase: VisionTrainingPoolPhase;
  count: number;
  candidate_pool: number;
  items: VisionTrainingPoolItem[];
  tank_id_filter?: string;
  camera_id_filter?: string;
};

// R15/R16 — captures 디렉토리 디스크 사용량 + retention 정책 상태
export type CaptureDiskResponse = {
  captures_dir: string;
  disk: {
    total_bytes: number;
    free_bytes: number;
    used_bytes: number;
    used_percent: number;
  };
  captures: { count: number; size_bytes: number };
  excluded: Record<string, { count: number; size_bytes: number }>;
};

export type VisionTrainingJobsResponse = {
  count: number;
  items: VisionTrainingJob[];
};

export type VisionTrainingStatus = {
  algorithm_id: string;
  algorithm_display: string;
  algorithm_status: VisionAlgorithmStatus;
  is_running: boolean;
  can_start_training: boolean;
  current_job: VisionTrainingJob | null;
  active_weights: {
    active_weights_path: string;
    [key: string]: unknown;
  };
  bootstrap: {
    box_count: number;
    frame_count: number;
    required_boxes: number;
    required_frames: number;
    can_start_training: boolean;
    hint: string;
  };
  dispute: {
    label_count: number;
    required: number;
    can_validate: boolean;
    hint: string;
  };
  labels: {
    count: number;
    observations: number;
    required: number;
    can_start_training: boolean;
  };
  safety_note?: string;
};

export type VisionBootstrapLabel = {
  label_id?: string;
  camera_id?: string;
  tank_id?: string;
  recorded_at?: string;
  box_count?: number;
  [key: string]: unknown;
};

export type VisionBootstrapLabelsResponse = {
  count: number;
  total_boxes: number;
  items: VisionBootstrapLabel[];
};

// ── 재고 관리 (Inventory) ─────────────────────────────────────────────────────
// Backend: /v1/inventory (GET), /v1/inventory/items (POST),
//          /v1/inventory/purchase (POST), /v1/inventory/consume (POST)
//          /v1/inventory/purchases/{id}/documents (POST multipart)

export type InventoryCategory = 'feed' | 'drug' | 'material';

export type InventoryItem = {
  item_id: string;
  category: InventoryCategory;
  name: string;
  unit: string;
  on_hand_qty: number;
  spec?: string;
  supplier?: string;
  notes?: string;
  reorder_level?: number;
  below_reorder: boolean;
};

export type InventoryListResponse = {
  items: InventoryItem[];
  count: number;
};

export type NewInventoryItemBody = {
  category: InventoryCategory;
  name: string;
  unit: string;
  spec?: string;
  supplier?: string;
  reorder_level?: number;
  notes?: string;
};

export type PurchaseBody = {
  item_id?: string;
  category?: InventoryCategory;
  name?: string;
  unit?: string;
  qty: number;
  unit_price?: number;
  total_price?: number;
  supplier?: string;
  lot?: string;
  purchased_at?: string;
  operator_id?: string;
  notes?: string;
};

export type ConsumeBody = {
  item_id: string;
  qty: number;
  reason?: string;
  notes?: string;
  operator_id?: string;
};

// ── 사이트 단위 입식 (POST /v1/site-stockings) ────────────────────────────────

export type SiteStockingAllocation = {
  tank_id: string;
  count: number;
  avg_weight_g: number;
  lot_no?: string;
  stocking_id?: string; // 응답에만 있음
};

export type SiteStocking = {
  site_stocking_id: string;
  site_id: string;
  supplier_id?: string;
  supplier_name?: string;
  species: string;
  growth_stage: TankGrowthStage;
  source_hatchery?: string;
  batch_lot_no?: string;
  total_count: number;
  allocations: SiteStockingAllocation[];
  stocked_at: string;
};

export type NewSiteStockingBody = {
  site_id: string;
  supplier_id?: string;
  species: string;
  growth_stage: TankGrowthStage;
  source_hatchery?: string;
  batch_lot_no?: string;
  stocked_at?: string;
  operator_id?: string;
  notes?: string;
  allocations: Array<{ tank_id: string; count: number; avg_weight_g: number; lot_no?: string }>;
};

export type SiteStockingsListResponse = {
  stockings: SiteStocking[];
  count: number;
};

// ── 사이트 단위 출하 (POST /v1/site-harvests) ─────────────────────────────────

export type SiteHarvestLine = {
  tank_id: string;
  lot_no?: string;
  count: number;
  avg_weight_g?: number;
  full_close: boolean;
  harvest_id?: string; // 응답에만 있음
};

export type SiteHarvest = {
  site_harvest_id: string;
  site_id: string;
  buyer_id?: string;
  buyer_name?: string;
  total_count: number;
  lines: SiteHarvestLine[];
  vehicle_info?: string;
  harvested_at: string;
};

export type NewSiteHarvestBody = {
  site_id: string;
  buyer_id?: string;
  vehicle_info?: string;
  harvested_at?: string;
  operator_id?: string;
  notes?: string;
  lines: Array<{ tank_id: string; lot_no?: string; count: number; avg_weight_g?: number; full_close: boolean }>;
};

export type SiteHarvestsListResponse = {
  harvests: SiteHarvest[];
  count: number;
};
// ───── Hatchery 종묘장 엔티티 (GX10 정본 — 엣지 SQLite 스키마와 동일 snake_case) ─────
export type BroodstockOrigin = 'wild' | 'domestic';

export type BroodstockCohort = {
  cohort_id: string;
  group_id: string;
  tank_id?: string;
  species: string;
  origin_type: BroodstockOrigin;
  origin_region?: string;
  supplier?: string;
  generation?: string;
  parent_cohort_id?: string;
  acquired_date?: string;
  male_count: number;
  female_count: number;
  maturity?: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};
export type NewBroodstockBody = {
  group_id: string;
  tank_id?: string;
  species: string;
  origin_type: BroodstockOrigin;
  origin_region?: string;
  supplier?: string;
  generation?: string;
  parent_cohort_id?: string;
  acquired_date?: string;
  male_count: number;
  female_count: number;
  maturity?: string;
  notes?: string;
};

export type SpawnBatch = {
  batch_id: string;
  group_id: string;
  tank_id?: string;
  species: string;
  lot_code?: string;
  female_cohort_id?: string;
  male_cohort_id?: string;
  origin_type?: string;
  origin_region?: string;
  supplier?: string;
  generation?: string;
  spawn_date?: string;
  egg_count: number;
  egg_volume_ml: number;
  fertilization_rate: number;
  hatch_date?: string;
  hatched_count: number;
  hatch_rate: number;
  status: string;
  buyer?: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};
export type NewSpawnBatchBody = {
  group_id: string;
  tank_id?: string;
  species: string;
  female_cohort_id?: string;
  male_cohort_id?: string;
  spawn_date?: string;
  egg_count: number;
  egg_volume_ml: number;
  fertilization_rate: number;
  hatch_date?: string;
  hatched_count?: number;
  hatch_rate?: number;
  status?: string;
  buyer?: string;
  notes?: string;
};

export type LarvalBatch = {
  batch_id: string;
  group_id: string;
  tank_id?: string;
  species: string;
  source_lot_code?: string;
  origin_type?: string;
  origin_region?: string;
  supplier?: string;
  generation?: string;
  start_date?: string;
  initial_count: number;
  current_count: number;
  survival_rate: number;
  dev_stage?: string;
  density_per_l: number;
  status: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};
export type NewLarvalBatchBody = {
  group_id: string;
  tank_id?: string;
  species?: string;
  source_lot_code?: string;
  start_date?: string;
  initial_count: number;
  current_count: number;
  dev_stage?: string;
  density_per_l: number;
  notes?: string;
};

export type LiveFeedCulture = {
  culture_id: string;
  group_id: string;
  tank_id?: string;
  feed_type: string;
  strain?: string;
  start_date?: string;
  volume_l: number;
  density_per_ml: number;
  last_harvest_date?: string;
  harvest_amount?: string;
  status: string;
  notes?: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};
export type NewLiveFeedBody = {
  group_id: string;
  tank_id?: string;
  feed_type: string;
  strain?: string;
  start_date?: string;
  volume_l: number;
  density_per_ml: number;
  last_harvest_date?: string;
  harvest_amount?: string;
  notes?: string;
};

// 종묘 처치(B-4) — MT 성전환·소독 등. lot_code 로 seed lot KDE 귀속.
export type HatcheryTreatmentType =
  | 'sex_reversal' | 'disinfection' | 'antibiotic' | 'vaccine'
  | 'chemical' | 'probiotic' | 'anesthetic' | 'other';
export type HatcheryTreatment = {
  treatment_id: string;
  group_id: string;
  subject_kind: 'spawn' | 'larval';
  batch_id?: string;
  lot_code?: string;
  tank_id?: string;
  species?: string;
  treatment_type: HatcheryTreatmentType;
  substance: string;
  dose?: number;
  dose_unit?: string;
  route?: string;
  reason?: string;
  withdrawal_until?: string;
  administered_at: string;
  operator_id?: string;
  item_id?: string;
  consumed_qty?: number;
  notes?: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};
export type NewHatcheryTreatmentBody = {
  group_id: string;
  subject_kind: 'spawn' | 'larval';
  batch_id?: string;
  tank_id?: string;
  species?: string;
  treatment_type: HatcheryTreatmentType;
  substance: string;
  dose?: number;
  dose_unit?: string;
  route?: string;
  reason?: string;
  withdrawal_until?: string;
  administered_at: string;
  operator_id?: string;
  item_id?: string;
  consumed_qty?: number;
  notes?: string;
};
