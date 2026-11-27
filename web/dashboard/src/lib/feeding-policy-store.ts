// Phase 1b — 운영 정책 (BSF mode / operating mode / max daily cycles) backend 이관.
// Source of truth: group_profiles.metadata.feeding_policy + tank_profiles.metadata.feeding_policy_override.
// localStorage 는 제거 — 캐시 미스 시 backend 에서 즉시 fetch.
//
// 인터페이스 호환: 호출처(OperatingPolicy/FeedQuickControl/FeedCycleMonitor)가 sync 가정이므로
// 메모리 캐시 + 백그라운드 fetch 로 동기 시그니처 유지. 캐시 미로드 시 default 반환 +
// ensureLoaded() 비동기 트리거 → 다음 read 부터 정확한 값. 이벤트 'bluei:feeding-policy-changed'
// 로 호출처 re-render 유도.

import type { BsfMode } from './species-policy';
import { DEFAULT_BSF_MODE, MAX_DAILY_CYCLES } from './species-policy';
import type { Group, Tank } from './types';
import { Groups, Tanks } from './api';

const LEGACY_STORAGE_KEY = 'bluei_feeding_policy_v1';

export type OperatingMode = 'auto' | 'manual';
// 값은 i18n 키 — 소비처(OperatingPolicy)에서 tr 로 해석한다.
export const OPERATING_MODE_LABEL: Record<OperatingMode, string> = {
  auto: 'operatingPolicy.modeAuto',
  manual: 'operatingPolicy.modeManual',
};
export const DEFAULT_OPERATING_MODE: OperatingMode = 'auto';

export type GroupPolicy = {
  bsf_mode: BsfMode;
  max_daily_cycles: number;
  // 일 사이클 수 직접 지정 (0 = 자동: 1일량÷1회최대량 산출 + max_daily_cycles 상한).
  // >0 이면 자동 산출을 대체 — backend AIScheduler 와 동일 (2026-07-07).
  daily_cycles: number;
  operating_mode: OperatingMode;
  // 급이 활동시간대(점등시간) — 소등되면 카메라 영상이 없어 급이·관찰 불가. 급이는 이 창 안에서만.
  photoperiod_start_min: number; // 점등(시작) 분/자정기준. 예: 540 = 09:00
  photoperiod_end_min: number;   // 소등(종료) 분. 예: 1020 = 17:00
  // 급이기 마이크로펄스 — onoff_feeder(on/off): run초 가동(≈9.4×run g) → stop초 정지(섭취) 반복해 회당 목표량 채움.
  feeder_run_sec: number;  // 펄스 1회 가동(초)
  feeder_stop_sec: number; // 펄스 사이 정지·섭취(초)
  // 공급모터 리드타임(초) — 살포모터 기동 후 공급모터 체결까지 대기 (실측 2026-07-04: +11~13s).
  // backend 가 펄스 ON 시간에 가산. 배출량 계산은 가동초만 사용.
  feeder_spinup_sec: number;
};

export type TankPolicyOverride = {
  bsf_mode?: BsfMode;             // 미설정 시 그룹 default 사용
  max_daily_cycles?: number;
  operating_mode?: OperatingMode;
};

// 캐시 — backend 로딩 1회 후 in-memory. setXxx 호출 시 캐시도 갱신.
const groupPoliciesCache = new Map<string, GroupPolicy>();
const tankOverridesCache = new Map<string, TankPolicyOverride>();
// 캐시 안에 있는 group 의 raw metadata (POST 시 다른 필드 보존 위해 필요).
const groupMetaRawCache = new Map<string, Record<string, unknown>>();
const tankMetaRawCache = new Map<string, Record<string, unknown>>();
const groupRowCache = new Map<string, Group>();
const tankRowCache = new Map<string, Tank>();

let cacheLoaded = false;
let loadPromise: Promise<void> | null = null;

function defaultGroupPolicy(): GroupPolicy {
  return {
    bsf_mode: DEFAULT_BSF_MODE,
    max_daily_cycles: MAX_DAILY_CYCLES,
    daily_cycles: 0, // 0 = 자동 산출
    operating_mode: DEFAULT_OPERATING_MODE,
    photoperiod_start_min: 9 * 60,  // 09:00 (점등 추정 — 실측 후 보정)
    photoperiod_end_min: 17 * 60,   // 17:00 (소등 ~17:30 전 버퍼)
    feeder_run_sec: 3,              // 1회 3초 공급 ≈ 28.2g (2026-07-04 현장 결정)
    feeder_stop_sec: 6,             // 사이 6초 정지(섭취) — 현장 실측 안정값
    feeder_spinup_sec: 11,          // 공급모터 리드 (2026-07-04 영상 실측 +11~13s)
  };
}

function dispatchChanged(): void {
  if (typeof window === 'undefined') return;
  try {
    window.dispatchEvent(new CustomEvent('bluei:feeding-policy-changed'));
  } catch {
    /* ignore */
  }
}

// Legacy localStorage 1회 정리. 마이그 skip — 사용자가 backend 에 다시 입력.
function clearLegacyStorage(): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.removeItem(LEGACY_STORAGE_KEY);
  } catch {
    /* ignore */
  }
}

function parseGroupPolicy(raw: unknown): GroupPolicy | null {
  if (!raw || typeof raw !== 'object') return null;
  const obj = raw as Record<string, unknown>;
  const base = defaultGroupPolicy();
  if (typeof obj['bsf_mode'] === 'string') base.bsf_mode = obj['bsf_mode'] as BsfMode;
  if (typeof obj['operating_mode'] === 'string')
    base.operating_mode = obj['operating_mode'] as OperatingMode;
  if (typeof obj['max_daily_cycles'] === 'number') base.max_daily_cycles = obj['max_daily_cycles'];
  if (typeof obj['daily_cycles'] === 'number') base.daily_cycles = obj['daily_cycles'];
  if (typeof obj['photoperiod_start_min'] === 'number')
    base.photoperiod_start_min = obj['photoperiod_start_min'];
  if (typeof obj['photoperiod_end_min'] === 'number')
    base.photoperiod_end_min = obj['photoperiod_end_min'];
  if (typeof obj['feeder_run_sec'] === 'number') base.feeder_run_sec = obj['feeder_run_sec'];
  if (typeof obj['feeder_stop_sec'] === 'number') base.feeder_stop_sec = obj['feeder_stop_sec'];
  if (typeof obj['feeder_spinup_sec'] === 'number')
    base.feeder_spinup_sec = obj['feeder_spinup_sec'];
  return base;
}

function parseTankOverride(raw: unknown): TankPolicyOverride | null {
  if (!raw || typeof raw !== 'object') return null;
  const obj = raw as Record<string, unknown>;
  const out: TankPolicyOverride = {};
  if (typeof obj['bsf_mode'] === 'string') out.bsf_mode = obj['bsf_mode'] as BsfMode;
  if (typeof obj['operating_mode'] === 'string')
    out.operating_mode = obj['operating_mode'] as OperatingMode;
  if (typeof obj['max_daily_cycles'] === 'number') out.max_daily_cycles = obj['max_daily_cycles'];
  return out;
}

/**
 * ensureLoaded — backend 에서 정책 캐시를 한 번만 채움.
 * 마운트 시 trigger 권장 (App.tsx / OperatingPolicy.tsx).
 * idempotent: 여러 번 호출 안전, 동시 호출은 같은 promise 공유.
 */
export function ensureLoaded(): Promise<void> {
  if (cacheLoaded) return Promise.resolve();
  if (loadPromise) return loadPromise;
  loadPromise = (async () => {
    try {
      await loadFromBackend();
      cacheLoaded = true;
      clearLegacyStorage();
      dispatchChanged();
      armAutoRefresh();
    } catch {
      // network 오류 시 캐시 비어있어 default 반환. 다음 ensureLoaded() 재시도.
      loadPromise = null;
      throw new Error('ensureLoaded failed');
    }
  })();
  return loadPromise;
}

// backend 조회 → 캐시 재구성. ensureLoaded(최초 1회) 와 주기 refresh 가 공유.
// fetch 성공 후에만 clear+재구성 — 외부에서 제거된 override 잔존 방지 + 실패 시 기존 유지.
async function loadFromBackend(): Promise<void> {
  const gRes = await Groups.list();
  const tRes = await Tanks.list();
  groupRowCache.clear();
  groupMetaRawCache.clear();
  groupPoliciesCache.clear();
  tankRowCache.clear();
  tankMetaRawCache.clear();
  tankOverridesCache.clear();
  for (const g of gRes.items) {
    groupRowCache.set(g.group_id, g);
    const meta = (g.metadata ?? {}) as Record<string, unknown>;
    groupMetaRawCache.set(g.group_id, meta);
    const fp = parseGroupPolicy(meta['feeding_policy']);
    if (fp) groupPoliciesCache.set(g.group_id, fp);
  }
  for (const t of tRes.items) {
    tankRowCache.set(t.tank_id, t);
    const meta = (t.metadata ?? {}) as Record<string, unknown>;
    tankMetaRawCache.set(t.tank_id, meta);
    const ov = parseTankOverride(meta['feeding_policy_override']);
    if (ov) tankOverridesCache.set(t.tank_id, ov);
  }
}

// 주기 재조회 — 정책이 이 화면 밖(backend/API/다른 세션)에서 바뀌어도 모드 토글·스케줄
// 카드가 자동 동기되도록 60초마다 refresh + change 이벤트. 앱 수명 동안 유지(1회 arming).
const POLICY_REFRESH_INTERVAL_MS = 60_000;
let autoRefreshArmed = false;
function armAutoRefresh(): void {
  if (autoRefreshArmed || typeof window === 'undefined') return;
  autoRefreshArmed = true;
  window.setInterval(() => {
    loadFromBackend()
      .then(() => dispatchChanged())
      .catch(() => { /* 네트워크 오류 — 기존 캐시 유지, 다음 tick 재시도 */ });
  }, POLICY_REFRESH_INTERVAL_MS);
}

// ── 동기 getter — 캐시 미로드 시 default + background fetch trigger ─────────────

export function getGroupPolicy(group_id: string): GroupPolicy {
  if (!cacheLoaded) void ensureLoaded().catch(() => {});
  return { ...defaultGroupPolicy(), ...(groupPoliciesCache.get(group_id) ?? {}) };
}

export function getTankOverride(tank_id: string): TankPolicyOverride {
  if (!cacheLoaded) void ensureLoaded().catch(() => {});
  return { ...(tankOverridesCache.get(tank_id) ?? {}) };
}

// ── async setter — 캐시 갱신 + backend POST (fire-and-forget) ─────────────────

export function setGroupPolicy(group_id: string, policy: Partial<GroupPolicy>): void {
  const current = { ...defaultGroupPolicy(), ...(groupPoliciesCache.get(group_id) ?? {}), ...policy };
  groupPoliciesCache.set(group_id, current);
  // metadata 갱신 (다른 필드 보존).
  const meta = { ...(groupMetaRawCache.get(group_id) ?? {}) };
  meta['feeding_policy'] = current;
  groupMetaRawCache.set(group_id, meta);
  dispatchChanged();
  // backend POST — 기존 group 의 전체 row + 갱신된 metadata 로 upsert.
  const existing = groupRowCache.get(group_id);
  void Groups.create({
    group_id,
    name: existing?.name ?? group_id,
    description: existing?.description ?? '',
    color: existing?.color ?? '',
    metadata: meta,
  }).catch(() => {
    // 실패 시 캐시는 유지 (다음 ensureLoaded 시 backend 가 진실원). 운영자 재입력 가능.
  });
}

export function setTankOverride(tank_id: string, override: TankPolicyOverride): void {
  // override 가 빈 object 면 clearTankOverride 로 위임.
  const isEmpty =
    override.bsf_mode == null && override.max_daily_cycles == null && override.operating_mode == null;
  if (isEmpty) {
    clearTankOverride(tank_id);
    return;
  }
  tankOverridesCache.set(tank_id, override);
  const meta = { ...(tankMetaRawCache.get(tank_id) ?? {}) };
  meta['feeding_policy_override'] = override;
  tankMetaRawCache.set(tank_id, meta);
  dispatchChanged();
  postTankMetadata(tank_id, meta);
}

// 기존 override 를 보존하며 일부 필드만 갱신 — setTankOverride 는 통째 교체라
// 호출처가 patch 의도로 쓰면 다른 필드(bsf_mode ↔ operating_mode)가 지워진다 (2026-07-08 fix).
export function mergeTankOverride(tank_id: string, patch: TankPolicyOverride): void {
  setTankOverride(tank_id, { ...getTankOverride(tank_id), ...patch });
}

export function clearTankOverride(tank_id: string): void {
  tankOverridesCache.delete(tank_id);
  const meta = { ...(tankMetaRawCache.get(tank_id) ?? {}) };
  delete meta['feeding_policy_override'];
  tankMetaRawCache.set(tank_id, meta);
  dispatchChanged();
  postTankMetadata(tank_id, meta);
}

// Tank upsert helper — 기존 tank row 의 필수 필드 + 갱신된 metadata 로 POST.
function postTankMetadata(tank_id: string, metadata: Record<string, unknown>): void {
  const existing = tankRowCache.get(tank_id);
  if (!existing) {
    // tank row 정보가 없으면 POST 안전하지 않음 (필수 필드 부족). 캐시만 갱신.
    return;
  }
  void Tanks.create({
    tank_id,
    display_name: existing.display_name,
    species: existing.species,
    system_type: existing.system_type,
    ...(existing.volume_m3 != null && { volume_m3: existing.volume_m3 }),
    ...(existing.biomass_kg != null && { biomass_kg: existing.biomass_kg }),
    ...(existing.fish_count != null && { fish_count: existing.fish_count }),
    ...(existing.avg_weight_g != null && { avg_weight_g: existing.avg_weight_g }),
    ...(existing.target_ranges != null && { target_ranges: existing.target_ranges }),
    ...(existing.group_id != null && { group_id: existing.group_id }),
    ...(existing.site_id != null && { site_id: existing.site_id }),
    ...(existing.wtg_id != null && { wtg_id: existing.wtg_id }),
    ...(existing.lot_no != null && { lot_no: existing.lot_no }),
    ...(existing.lifecycle_stage != null && { lifecycle_stage: existing.lifecycle_stage }),
    ...(existing.form_factor && { form_factor: existing.form_factor as 'round' | 'square' | 'rectangular' }),
    ...(existing.diameter_m != null && { diameter_m: existing.diameter_m }),
    ...(existing.length_m != null && { length_m: existing.length_m }),
    ...(existing.width_m != null && { width_m: existing.width_m }),
    ...(existing.depth_m != null && { depth_m: existing.depth_m }),
    metadata,
  }).catch(() => {
    // ignore — 다음 ensureLoaded 시 진실원 backend.
  });
}

// 수조의 실효 정책 (override 우선, 없으면 그룹 default).
export type EffectiveTankPolicy = GroupPolicy & {
  is_override: boolean;
  group_default: GroupPolicy;
};

export function getEffectiveTankPolicy(
  tank_id: string,
  group_id: string | null | undefined,
): EffectiveTankPolicy {
  const groupDefault: GroupPolicy = group_id
    ? getGroupPolicy(group_id)
    : defaultGroupPolicy();
  const override = getTankOverride(tank_id);
  const has_override =
    override.bsf_mode != null ||
    override.max_daily_cycles != null ||
    override.operating_mode != null;
  return {
    ...groupDefault, // photoperiod 등 그룹 기본값 보존 (override 없는 필드)
    bsf_mode: override.bsf_mode ?? groupDefault.bsf_mode,
    max_daily_cycles: override.max_daily_cycles ?? groupDefault.max_daily_cycles,
    operating_mode: override.operating_mode ?? groupDefault.operating_mode,
    is_override: has_override,
    group_default: groupDefault,
  };
}
