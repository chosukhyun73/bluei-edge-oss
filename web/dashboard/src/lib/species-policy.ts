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
  base_get95_h: number;          // 18℃, 1%BW 기준
  q10: number;
  reference_temp_c: number;
  reference_meal_pct_bw: number;
  meal_exponent: number;          // 보수적
  next_feed_at_get_pct: number;   // 다음 급이 시작 GET % (운영 규칙)
  formula: string;
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
          pulse_duration_ms: 2500, gap_ms: 75_000, total_pulses: 6,
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
      next_feed_at_get_pct: 50,
      formula: 'GET_95(T, M) = 24 × 2^((18−T)/10) × (M/1.0)^0.5',
    },
  },
};

export const DEFAULT_FALLBACK_TEMP_C = 14;   // sensor 없을 때 fallback
export const MAX_DAILY_CYCLES = 4;            // 운영 캡 (사용자 결정)
export const DEFAULT_BSF_MODE: BsfMode = 'standard';

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
  daily_cycles: number | null;
  cycle_target_amount_g: number | null;
  cycle_meal_pct_bw: number | null;

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
): number {
  const tempPart = Math.pow(model.q10, (model.reference_temp_c - temp_c) / 10);
  const mealPart = Math.pow(meal_pct_bw / model.reference_meal_pct_bw, model.meal_exponent);
  return model.base_get95_h * tempPart * mealPart;
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
  let daily_cycles: number | null = null;
  let cycle_target_amount_g: number | null = null;
  let cycle_meal_pct_bw: number | null = null;

  if (daily_feed_g != null && biomass_kg > 0) {
    // 추정 1회량 (초기 추정 3 분할)
    const estimate_per_cycle_g = daily_feed_g / 3;
    const meal_pct_bw = (estimate_per_cycle_g / biomass_kg) * 0.1; // g/kg → %BW (×100/1000)
    cycle_meal_pct_bw = meal_pct_bw;

    get95_h = computeGet95(policy.get_model, temperature_c, meal_pct_bw);
    get50_h = computeGet50(get95_h);

    const cap = input.max_daily_cycles ?? MAX_DAILY_CYCLES;
    daily_cycles = Math.max(1, Math.min(cap, Math.floor(24 / get50_h)));
    cycle_target_amount_g = Math.round(daily_feed_g / daily_cycles);
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
    daily_cycles,
    cycle_target_amount_g,
    cycle_meal_pct_bw,
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
    daily_cycles: null,
    cycle_target_amount_g: null,
    cycle_meal_pct_bw: null,
    feeding_pattern_default: null,
    max_duration_min: null,
    speed_rpm: null,
    amount: null,
    warnings: opts.warnings,
    block_reason: opts.block,
  };
}
