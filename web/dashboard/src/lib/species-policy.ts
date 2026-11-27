// 운영 정책 도메인 상수 + 계산 헬퍼.
// 본선 D-9 frontend constants. backend species_profiles 와 별개 (회귀 0).
// 본선 후: backend 의 species_profiles yaml 로 통합 + 학습 기반 보정 (Phase β).
import { t } from './i18n';

export type BsfMode = 'aggressive' | 'standard' | 'conservative';

export const BSF_MODE_LABEL: Record<BsfMode, string> = {
  aggressive: 'species.bsfAggressive',
  standard: 'species.bsfStandard',
  conservative: 'species.bsfConservative',
};

export type LifecycleStageKey = 'fry' | 'fingerling' | 'growout';

export const STAGE_LABEL: Record<LifecycleStageKey, string> = {
  fry: 'species.stageFry',
  fingerling: 'species.stageFingerling',
  growout: 'species.stageGrowout',
};

type DensityThresholds = {
  safe_max: number;      // 🟢 안전
  caution_max: number;   // 🟡 주의
  warning_max: number;   // 🟠 경계
  danger_max: number;    // 🔴 위험 / 초과
};

type StagePolicy = {
  weight_range_g: [number, number];
  density_thresholds_kg_per_m3: DensityThresholds;
  bsf_percent_bw_per_day: Record<BsfMode, number>;
  feeding_pattern_default: {
    pulse_duration_ms: number;
    gap_ms: number;
    total_pulses: number;
    // Phase B: ESP32 살포(DAC1, rpm) / 공급(DAC2, amount 0~255) 모터 권장 default.
    speed_rpm: number;
    amount: number;
  };
  fcr_target: number;
  feed_type: string;
};

type GetModel = {
  base_get95_h: number;          // 18℃, 1%BW 기준 (폴백 일반모델)
  q10: number;
  reference_temp_c: number;
  reference_meal_pct_bw: number;
  meal_exponent: number;          // 보수적
  next_feed_at_get_pct: number;   // 다음 급이 시작 GET % (운영 규칙)
  formula: string;
  // vault 대서양연어 GET 정본 모델(참조표 검증): GET95(h)=α·BW_g^β·e^(−γ·T).
  // 있으면 BW(평균체중) 기반 이 모델을 우선 사용. 출처: Obsidian Atlantic Salmon GET Model Report.
  salmon_alpha?: number;         // α=16.3
  salmon_beta?: number;          // β=0.31 (체중지수)
  salmon_gamma?: number;         // γ=0.11 (온도계수, ≤18℃)
};

type SpeciesPolicy = {
  species: string;
  display_name: string;
  lifecycle_stages: Record<LifecycleStageKey, StagePolicy>;
  get_model: GetModel;
};

export const SPECIES_POLICY: Record<string, SpeciesPolicy> = {
  atlantic_salmon: {
    species: 'atlantic_salmon',
    display_name: 'species.atlanticSalmon',
    lifecycle_stages: {
      fry: {
        weight_range_g: [0, 100],
        density_thresholds_kg_per_m3: {
          safe_max: 20, caution_max: 30, warning_max: 40, danger_max: 50,
        },
        bsf_percent_bw_per_day: { aggressive: 5.0, standard: 4.0, conservative: 3.0 },
        feeding_pattern_default: {
          pulse_duration_ms: 1500, gap_ms: 45_000, total_pulses: 3,
          // 치어: 작은 사료 → 분사 느림 + 공급량 적게.
          speed_rpm: 18, amount: 80,
        },
        fcr_target: 1.0,
        feed_type: 'fry_micropellet',
      },
      fingerling: {
        weight_range_g: [100, 500],
        density_thresholds_kg_per_m3: {
          safe_max: 30, caution_max: 40, warning_max: 60, danger_max: 80,
        },
        bsf_percent_bw_per_day: { aggressive: 3.0, standard: 2.5, conservative: 2.0 },
        feeding_pattern_default: {
          pulse_duration_ms: 2000, gap_ms: 60_000, total_pulses: 5,
          // 중간육성: 중간 속도 + 중간 공급량.
          speed_rpm: 24, amount: 130,
        },
        fcr_target: 1.05,
        feed_type: 'fingerling_pellet',
      },
      growout: {
        weight_range_g: [500, 5000],
        density_thresholds_kg_per_m3: {
          safe_max: 40, caution_max: 50, warning_max: 75, danger_max: 100,
        },
        bsf_percent_bw_per_day: { aggressive: 2.0, standard: 1.5, conservative: 1.0 },
        feeding_pattern_default: {
          // 2026-07-04 강릉 실측 보정: 공급 3s + 정지 6s (onoff_feeder 리드 11s 는 backend 가 ON 에 가산).
          // backend ai_scheduler.go speciesPolicyTable 과 동기.
          pulse_duration_ms: 3000, gap_ms: 6_000, total_pulses: 6,
          // 성어: 멀리 분사 + 공급량 많이.
          speed_rpm: 32, amount: 180,
        },
        fcr_target: 1.1,
        feed_type: 'growout_pellet',
      },
    },
    get_model: {
      base_get95_h: 24,
      q10: 2.0,
      reference_temp_c: 18,
      reference_meal_pct_bw: 1.0,
      meal_exponent: 0.5,
      next_feed_at_get_pct: 95,   // 소화(95% 위배출) 후 재급이
      formula: 'GET95(h) = 16.3 × BW_g^0.31 × e^(−0.11×T)  [vault 대서양연어 정본]',
      salmon_alpha: 16.3,
      salmon_beta: 0.31,
      salmon_gamma: 0.11,
    },
  },
};

export const DEFAULT_FALLBACK_TEMP_C = 14;   // sensor 없을 때 fallback
export const MAX_DAILY_CYCLES = 6;            // 운영 캡 (분할 상한)
export const DEFAULT_BSF_MODE: BsfMode = 'standard';

// 1회 최대 투여량(수처리 부하 상한, %BW). 1일 급이량을 이 상한으로 나눠 급이 횟수 N 산정.
//   1회최대량(g) = biomass_kg × MEAL_CAP_PCT_BW/100 × 1000  (biomass=개체수×체중 → "개체수 기준")
//   N = ceil(1일량 / 1회최대량) = ceil(BSF% / MEAL_CAP_PCT_BW), [1, MAX_DAILY_CYCLES] clamp.
// ⚠ 잠정값: 6mm 사료 초당 배출·개체당 섭취 실측(WP-G) 나오면 보정. RAS 수처리 순간부하 분산이 목적.
export const DEFAULT_MEAL_CAP_PCT_BW = 0.25;

// ── 계산 헬퍼 ────────────────────────────────────────────────────────────────

export type DensityStatus = 'safe' | 'caution' | 'warning' | 'danger' | 'overdense';

export const DENSITY_STATUS_LABEL: Record<DensityStatus, string> = {
  safe: 'species.densitySafe',
  caution: 'species.densityCaution',
  warning: 'species.densityWarning',
  danger: 'species.densityDanger',
  overdense: 'species.densityOverdense',
};

export const DENSITY_STATUS_COLOR: Record<DensityStatus, string> = {
  safe:      'bg-green-500/20 text-green-300 border border-green-500/40',
  caution:   'bg-yellow-500/20 text-yellow-300 border border-yellow-500/40',
  warning:   'bg-orange-500/20 text-orange-300 border border-orange-500/40',
  danger:    'bg-red-500/20 text-red-300 border border-red-500/40',
  overdense: 'bg-red-600/30 text-red-200 border border-red-500/60',
};

export type FeedingPolicyComputation = {
  // 입식
  biomass_kg: number;
  density_kg_per_m3: number | null;
  density_status: DensityStatus | null;
  density_thresholds: DensityThresholds | null;

  // 단계
  stage: LifecycleStageKey | null;
  stage_label: string;
  fcr_target: number | null;

  // 정책
  bsf_mode: BsfMode;
  bsf_percent: number | null;          // 정책 + 단계 기반 BSF (%/일)
  daily_feed_g: number | null;

  // GET / 분할
  temperature_c: number;
  temperature_source: 'sensor' | 'fallback';
  get95_h: number | null;
  get50_h: number | null;
  get80_h: number | null;         // GET80(DT80) 소화간격(h) — 참고·경고용(하드 제약 아님)
  daily_cycles: number | null;
  cycle_target_amount_g: number | null;
  cycle_meal_pct_bw: number | null;
  meal_max_g: number | null;      // 1회 최대 투여량(수처리 부하 상한, g)

  // 사이클 폼 default
  feeding_pattern_default: StagePolicy['feeding_pattern_default'] | null;
  max_duration_min: number | null;
  // Phase B: ESP32 모터 권장 default (단계 기반). null = 단계 미판정.
  speed_rpm: number | null;
  amount: number | null;

  // 진단
  warnings: string[];
  block_reason: string | null;          // null 이면 사이클 시작 가능
};

export function classifyDensity(
  density: number,
  thresholds: DensityThresholds,
): DensityStatus {
  if (density <= thresholds.safe_max) return 'safe';
  if (density <= thresholds.caution_max) return 'caution';
  if (density <= thresholds.warning_max) return 'warning';
  if (density <= thresholds.danger_max) return 'danger';
  return 'overdense';
}

export function findStage(
  policy: SpeciesPolicy,
  avg_weight_g: number,
): LifecycleStageKey | null {
  const keys: LifecycleStageKey[] = ['fry', 'fingerling', 'growout'];
  for (const k of keys) {
    const [lo, hi] = policy.lifecycle_stages[k].weight_range_g;
    if (avg_weight_g >= lo && avg_weight_g < hi) return k;
  }
  // growout 상한 초과 시도 growout 으로 처리.
  if (avg_weight_g >= policy.lifecycle_stages.growout.weight_range_g[0]) return 'growout';
  return null;
}

// GET 모델 — 사용자 자료의 BlueI 기본식:
//   GET_95(T, M) = 24 × 2^((18−T)/10) × (M/1.0)^0.5
//   GET_50 ≈ GET_95 × ln(2) / ln(20)  ≈ × 0.231
const GET50_FACTOR = Math.log(2) / Math.log(20);

export function computeGet95(
  model: GetModel,
  temp_c: number,
  meal_pct_bw: number,
  bw_g?: number | null,
): number {
  // vault 대서양연어 BW 기반 정본 모델 우선: GET95 = α·BW_g^β·e^(−γ·T)
  if (model.salmon_alpha != null && bw_g != null && bw_g > 0) {
    return model.salmon_alpha
      * Math.pow(bw_g, model.salmon_beta as number)
      * Math.exp(-(model.salmon_gamma as number) * temp_c);
  }
  // 폴백: 일반 meal 기반 모델
  const tempPart = Math.pow(model.q10, (model.reference_temp_c - temp_c) / 10);
  const mealPart = Math.pow(meal_pct_bw / model.reference_meal_pct_bw, model.meal_exponent);
  return model.base_get95_h * tempPart * mealPart;
}

// GET80(DT80) = GET95 / 1.86 — 80% 위배출 시각(지수배출 ln0.20/ln0.05≈1/1.86). 최소 재급이 간격(소화 후 재급이).
export const SALMON_GET80_FACTOR = 1 / 1.86;
export function computeGet80(get95_h: number): number {
  return get95_h * SALMON_GET80_FACTOR;
}
export function computeGet50(get95_h: number): number {
  return get95_h * GET50_FACTOR;
}

export type ComputeInputs = {
  species: string;
  fish_count: number | null;
  avg_weight_g: number | null;
  volume_m3: number | null;
  bsf_mode: BsfMode;
  temperature_c?: number | null;        // sensor 실측. null/undefined → fallback
  max_daily_cycles?: number;
  meal_cap_pct_bw?: number;             // 1회 최대 투여량(%BW). null → DEFAULT_MEAL_CAP_PCT_BW
  daily_cycles?: number;                // >0 = 직접 지정(자동 산출 대체). backend AIScheduler 동일 (2026-07-07)
};

export function computeFeedingPolicy(input: ComputeInputs): FeedingPolicyComputation {
  const policy = SPECIES_POLICY[input.species];
  const warnings: string[] = [];
  let block_reason: string | null = null;

  // 종 미지원
  if (!policy) {
    return emptyComputation({
      bsf_mode: input.bsf_mode,
      temp: input.temperature_c ?? DEFAULT_FALLBACK_TEMP_C,
      temp_source: input.temperature_c == null ? 'fallback' : 'sensor',
      warnings: [t('species.warnUndefinedPolicy', { species: input.species })],
      block: t('species.blockUndefinedPolicy'),
    });
  }

  // 입식 정보 결손
  const fish_count = input.fish_count ?? 0;
  const avg_weight_g = input.avg_weight_g ?? 0;
  const volume_m3 = input.volume_m3 ?? 0;

  const biomass_kg = (fish_count * avg_weight_g) / 1000;
  const density_kg_per_m3 = volume_m3 > 0 ? biomass_kg / volume_m3 : null;

  if (fish_count <= 0 || avg_weight_g <= 0) {
    warnings.push(t('species.warnStockingMissing'));
  }
  if (volume_m3 <= 0) {
    warnings.push(t('species.warnVolumeMissing'));
  }

  // 단계 판정
  const stage = avg_weight_g > 0 ? findStage(policy, avg_weight_g) : null;
  const stagePolicy = stage ? policy.lifecycle_stages[stage] : null;

  // 밀도 게이트
  let density_status: DensityStatus | null = null;
  let density_thresholds: DensityThresholds | null = null;
  if (stagePolicy && density_kg_per_m3 != null) {
    density_thresholds = stagePolicy.density_thresholds_kg_per_m3;
    density_status = classifyDensity(density_kg_per_m3, density_thresholds);
    if (density_status === 'overdense') {
      block_reason = t('species.blockOverdense', {
        density: density_kg_per_m3.toFixed(1),
        threshold: density_thresholds.danger_max,
      });
    } else if (density_status === 'danger') {
      warnings.push(t('species.warnDensityDanger', {
        density: density_kg_per_m3.toFixed(1),
        warningMax: density_thresholds.warning_max,
        dangerMax: density_thresholds.danger_max,
      }));
    }
  }

  // BSF
  const bsf_percent = stagePolicy?.bsf_percent_bw_per_day[input.bsf_mode] ?? null;
  const daily_feed_g = bsf_percent != null && biomass_kg > 0
    ? biomass_kg * (bsf_percent / 100) * 1000
    : null;

  // 수온
  const temperature_c = input.temperature_c ?? DEFAULT_FALLBACK_TEMP_C;
  const temperature_source: 'sensor' | 'fallback' =
    input.temperature_c == null ? 'fallback' : 'sensor';

  // GET / 분할
  let get95_h: number | null = null;
  let get50_h: number | null = null;
  let get80_h: number | null = null;
  let daily_cycles: number | null = null;
  let cycle_target_amount_g: number | null = null;
  let cycle_meal_pct_bw: number | null = null;
  let meal_max_g: number | null = null;

  if (daily_feed_g != null && biomass_kg > 0) {
    // 1회 최대 투여량(수처리 부하 상한) = biomass × meal_cap%BW.
    const mealCapPct = input.meal_cap_pct_bw ?? DEFAULT_MEAL_CAP_PCT_BW;
    meal_max_g = biomass_kg * (mealCapPct / 100) * 1000;

    const cap = input.max_daily_cycles ?? MAX_DAILY_CYCLES;
    // 급이 횟수 N = 1일량 ÷ 1회최대량 (수처리 순간부하로 결정). GET80은 최소간격 검증만(하드 제약 아님).
    daily_cycles = meal_max_g > 0
      ? Math.max(1, Math.min(cap, Math.ceil(daily_feed_g / meal_max_g)))
      : 1;
    // 운영자 직접 지정(정책 daily_cycles) — 자동 산출·cap 대체. backend 와 동일 상한 12.
    if (input.daily_cycles != null && input.daily_cycles > 0) {
      daily_cycles = Math.min(12, input.daily_cycles);
    }
    cycle_target_amount_g = Math.round(daily_feed_g / daily_cycles);

    // GET 는 실제 회당량(cycle_target)의 meal%BW 로 계산 → 소화간격(GET80) 참고·경고용.
    const meal_pct_bw = (cycle_target_amount_g / biomass_kg) * 0.1; // g/kg → %BW (×100/1000)
    cycle_meal_pct_bw = meal_pct_bw;
    get95_h = computeGet95(policy.get_model, temperature_c, meal_pct_bw, avg_weight_g);
    get50_h = computeGet50(get95_h);
    get80_h = computeGet80(get95_h);
  }

  // 사이클 폼 default
  const feeding_pattern_default = stagePolicy?.feeding_pattern_default ?? null;
  const max_duration_min =
    feeding_pattern_default != null
      ? Math.ceil(
          (feeding_pattern_default.total_pulses *
            (feeding_pattern_default.pulse_duration_ms + feeding_pattern_default.gap_ms)) /
            60_000 *
            1.2,
        )
      : null;

  return {
    biomass_kg,
    density_kg_per_m3,
    density_status,
    density_thresholds,
    stage,
    stage_label: stage ? t(STAGE_LABEL[stage]) : t('species.stageUnknown'),
    fcr_target: stagePolicy?.fcr_target ?? null,
    bsf_mode: input.bsf_mode,
    bsf_percent,
    daily_feed_g,
    temperature_c,
    temperature_source,
    get95_h,
    get50_h,
    get80_h,
    daily_cycles,
    cycle_target_amount_g,
    cycle_meal_pct_bw,
    meal_max_g,
    feeding_pattern_default,
    max_duration_min,
    speed_rpm: feeding_pattern_default?.speed_rpm ?? null,
    amount: feeding_pattern_default?.amount ?? null,
    warnings,
    block_reason,
  };
}

function emptyComputation(opts: {
  bsf_mode: BsfMode;
  temp: number;
  temp_source: 'sensor' | 'fallback';
  warnings: string[];
  block: string | null;
}): FeedingPolicyComputation {
  return {
    biomass_kg: 0,
    density_kg_per_m3: null,
    density_status: null,
    density_thresholds: null,
    stage: null,
    stage_label: t('species.stageUnknown'),
    fcr_target: null,
    bsf_mode: opts.bsf_mode,
    bsf_percent: null,
    daily_feed_g: null,
    temperature_c: opts.temp,
    temperature_source: opts.temp_source,
    get95_h: null,
    get50_h: null,
    get80_h: null,
    daily_cycles: null,
    cycle_target_amount_g: null,
    cycle_meal_pct_bw: null,
    meal_max_g: null,
    feeding_pattern_default: null,
    max_duration_min: null,
    speed_rpm: null,
    amount: null,
    warnings: opts.warnings,
    block_reason: opts.block,
  };
}

// feedingWeightG — 급이 계산에 쓸 평균 체중(g). 급이량 역산 추정치를 우선한다
// (운영자 결정 2026-10-03). backend AIScheduler 의 feedingWeightProfile 과 같은 기준이다.
//
// avg_weight_g 는 마지막 샘플링 실측 앵커라 시간이 지날수록 실제와 벌어진다 — 2026-10-03
// 기준 91일 전 857g 인데 역산 추정은 1,389g 으로 1.62배 차이였고, 그래서 화면의 회당량
// (163g)이 실제 급이량(191g)과 맞지 않았다. 추정치가 없을 때만 앵커로 폴백한다.
export function feedingWeightG(t: {
  avg_weight_g?: number | null;
  estimated_avg_weight_g?: number | null;
}): number | null {
  const est = t.estimated_avg_weight_g;
  if (est != null && est > 0) return est;
  return t.avg_weight_g ?? null;
}
