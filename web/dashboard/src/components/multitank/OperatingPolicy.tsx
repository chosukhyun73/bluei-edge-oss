import { useEffect, useMemo, useState, useCallback } from 'react';
import { dateLocale } from '../../lib/format';
import { Settings, AlertTriangle, ExternalLink } from 'lucide-react';
import { Groups } from '../../lib/api';
import { useLanguage } from '../../lib/language-core';
import type { Group, Tank } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';
import { type BsfMode, BSF_MODE_LABEL, computeFeedingPolicy, DENSITY_STATUS_LABEL, DENSITY_STATUS_COLOR, MAX_DAILY_CYCLES, feedingWeightG } from '../../lib/species-policy';
import { useLiveTemps } from '../../lib/use-live-temps';
import type { GroupPolicy } from '../../lib/feeding-policy-store';
import {
  OPERATING_MODE_LABEL, DEFAULT_OPERATING_MODE,
  getGroupPolicy, setGroupPolicy,
  getEffectiveTankPolicy, mergeTankOverride,
  ensureLoaded as ensurePolicyLoaded,
} from '../../lib/feeding-policy-store';

// 사료공급 탭으로 이동 (MultiTankOperations 가 수신).
// 운영 모드의 정본은 수조별 설정(사료공급 탭) — 이 페이지는 상황판 역할 (2026-07-08 운영자 확정).
function gotoFeedTab(): void {
  window.dispatchEvent(new CustomEvent('bluei:goto-subtab', { detail: 'feed' }));
}

const MODES: BsfMode[] = ['aggressive', 'standard', 'conservative'];

function ModeRadio({
  value, onChange, size = 'md', disabled = false,
}: {
  value: BsfMode;
  onChange: (m: BsfMode) => void;
  size?: 'sm' | 'md';
  disabled?: boolean;
}) {
  const { tr } = useLanguage();
  const padding = size === 'sm' ? 'px-2 py-0.5 text-xs' : 'px-3 py-1 text-sm';
  return (
    <div className="inline-flex gap-1">
      {MODES.map(m => (
        <button
          key={m}
          type="button"
          onClick={() => onChange(m)}
          disabled={disabled}
          className={
            value === m
              ? `${padding} rounded border bg-green-600/20 border-green-500/60 text-green-200`
              : `${padding} rounded border border-gray-700 text-gray-400 hover:text-white hover:border-gray-600 disabled:opacity-40`
          }
        >
          {tr(BSF_MODE_LABEL[m])}
        </button>
      ))}
    </div>
  );
}

function fmtHours(h: number | null, tr: (k: string, v?: Record<string, string | number>) => string): string {
  if (h == null) return '—';
  if (h < 1) return tr('operatingPolicy.minutesValue', { n: (h * 60).toFixed(0) });
  return `${h.toFixed(1)}h`;
}

function fmtNumber(n: number | null, digits = 0): string {
  if (n == null) return '—';
  return n.toLocaleString(dateLocale(), { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

function TankPolicyCard({
  tank, group, liveTempC, onChanged,
}: {
  tank: Tank;
  group: Group | null;
  liveTempC: number | null; // 실측 수온 (없으면 null → fallback)
  onChanged: () => void;
}) {
  const { tr } = useLanguage();
  const effective = getEffectiveTankPolicy(tank.tank_id, group?.group_id);
  const computation = useMemo(() => computeFeedingPolicy({
    species: tank.species,
    fish_count: tank.fish_count ?? null,
    avg_weight_g: feedingWeightG(tank),
    volume_m3: tank.volume_m3 ?? null,
    bsf_mode: effective.bsf_mode,
    temperature_c: liveTempC, // 실측 수온(사료공급 탭과 동일 소스). null → fallback
    max_daily_cycles: effective.max_daily_cycles,
    daily_cycles: effective.daily_cycles,
  }), [tank, effective.bsf_mode, effective.max_daily_cycles, liveTempC]);

  // BSF 는 수조 전용 설정 — 항상 수조에 명시적으로 기록 (그룹 상속/해제 개념 제거, 2026-07-08).
  function handleBsfChange(m: BsfMode) {
    mergeTankOverride(tank.tank_id, { bsf_mode: m });
    onChanged();
  }

  const densityStatus = computation.density_status;
  const isBlocked = computation.block_reason != null;

  return (
    <div className={
      isBlocked
        ? 'p-4 bg-gray-900/40 border border-red-500/30 rounded-lg space-y-3'
        : 'p-4 bg-gray-900/40 border border-gray-700/40 rounded-lg space-y-3'
    }>
      {/* 헤더 */}
      <div className="flex items-baseline justify-between gap-3 flex-wrap">
        <div className="flex items-baseline gap-2 min-w-0">
          <span className="text-sm font-semibold text-white">{tank.display_name || tank.tank_id}</span>
          <span className="text-xs text-gray-500 font-mono">{tank.tank_id}</span>
          {tank.species && (
            <span className="text-xs text-gray-500">· {tank.species}</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-gray-400">{tr('operatingPolicy.policy')}</span>
          <ModeRadio
            value={effective.bsf_mode}
            onChange={handleBsfChange}
            size="sm"
          />
        </div>
      </div>

      <div className="flex items-center gap-3 text-xs text-gray-400">
        <span>
          {tr('operatingPolicy.operating')} <span className={effective.operating_mode === 'auto' ? 'text-green-300' : 'text-amber-300'}>
            {tr(OPERATING_MODE_LABEL[effective.operating_mode])}
          </span>
        </span>
        <button
          type="button"
          onClick={gotoFeedTab}
          className="inline-flex items-center gap-1 text-gray-500 hover:text-white underline underline-offset-2"
        >
          <ExternalLink className="w-3 h-3" />
          {tr('operatingPolicy.changeInFeedTab')}
        </button>
      </div>

      {/* 입식 + 밀도 */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3 text-xs">
        <Stat label={tr('operatingPolicy.fishCount')} value={fmtNumber(tank.fish_count ?? null)} />
        <Stat label={tr('operatingPolicy.avgWeight')} value={tank.avg_weight_g != null ? `${fmtNumber(tank.avg_weight_g)}g` : '—'} />
        <Stat label={tr('operatingPolicy.totalBiomass')} value={`${fmtNumber(computation.biomass_kg, 0)} kg`} />
        <Stat label={tr('operatingPolicy.volume')} value={tank.volume_m3 != null ? `${tank.volume_m3} m³` : '—'} />
        <Stat
          label={tr('operatingPolicy.density')}
          value={computation.density_kg_per_m3 != null ? `${computation.density_kg_per_m3.toFixed(1)} kg/m³` : '—'}
          badge={densityStatus && (
            <span className={`px-1.5 py-0.5 rounded text-xs ${DENSITY_STATUS_COLOR[densityStatus]}`}>
              {tr(DENSITY_STATUS_LABEL[densityStatus])}
            </span>
          )}
        />
      </div>

      {/* 단계 + BSF + GET */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 px-3 py-2 bg-gray-800/40 border border-gray-700/30 rounded text-xs">
        <Stat label={tr('operatingPolicy.growthStage')} value={computation.stage_label} />
        <Stat
          label={`BSF (${tr(BSF_MODE_LABEL[computation.bsf_mode])})`}
          value={computation.bsf_percent != null ? tr('operatingPolicy.bsfPercentPerDay', { v: computation.bsf_percent.toFixed(1) }) : '—'}
        />
        <Stat label={tr('operatingPolicy.dailyFeed')} value={tr('operatingPolicy.dailyFeedValue', { g: fmtNumber(computation.daily_feed_g) })} />
        <Stat label={`${tr('operatingPolicy.waterTemp')} (${computation.temperature_source === 'sensor' ? tr('operatingPolicy.measured') : 'fallback'})`} value={`${computation.temperature_c}℃`} />
      </div>

      {/* GET / 분할 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 px-3 py-2 bg-gray-800/40 border border-gray-700/30 rounded text-xs">
        <Stat label="GET₉₅" value={fmtHours(computation.get95_h, tr)} />
        <Stat label={`GET₅₀ (${tr('operatingPolicy.nextFeeding')})`} value={fmtHours(computation.get50_h, tr)} />
        <Stat label={tr('operatingPolicy.dailyCycles')} value={tr('operatingPolicy.dailyCyclesValue', { n: computation.daily_cycles ?? '—', max: MAX_DAILY_CYCLES })} />
        <Stat
          label={tr('operatingPolicy.cycleTarget')}
          value={`${fmtNumber(computation.cycle_target_amount_g)} g`}
        />
      </div>

      {/* 사이클 폼 default (참고) */}
      {computation.feeding_pattern_default && (
        <div className="text-xs text-gray-500 px-2 space-y-0.5">
          <div>
            <span className="text-gray-400">{tr('operatingPolicy.cycleFormDefault')}</span>{' '}
            {tr('operatingPolicy.totalPulses')} <span className="font-mono text-gray-300">{computation.feeding_pattern_default.total_pulses}</span>
            {' · '}{tr('operatingPolicy.pulseDuration')} <span className="font-mono text-gray-300">{computation.feeding_pattern_default.pulse_duration_ms}ms</span>
            {' · '}{tr('operatingPolicy.gap')} <span className="font-mono text-gray-300">{computation.feeding_pattern_default.gap_ms / 1000}s</span>
            {' · '}{tr('operatingPolicy.max')} <span className="font-mono text-gray-300">{computation.max_duration_min}{tr('operatingPolicy.min')}</span>
          </div>
          {/* Phase B: ESP32 살포(DAC1) / 공급(DAC2) 모터 권장 default */}
          <div>
            <span className="text-gray-400">+ {tr('operatingPolicy.motor')}:</span>{' '}
            {tr('operatingPolicy.spray')} <span className="font-mono text-gray-300">{computation.feeding_pattern_default.speed_rpm}rpm</span>
            {' · '}{tr('operatingPolicy.supply')} <span className="font-mono text-gray-300">{computation.feeding_pattern_default.amount}</span>
            <span className="text-gray-600"> (amount)</span>
          </div>
        </div>
      )}

      {/* 경고 / 차단 */}
      {computation.warnings.length > 0 && (
        <ul className="space-y-1">
          {computation.warnings.map((w, i) => (
            <li key={i} className="flex items-start gap-1.5 text-xs text-amber-300">
              <AlertTriangle className="w-3 h-3 flex-shrink-0 mt-0.5" />
              <span>{w}</span>
            </li>
          ))}
        </ul>
      )}
      {computation.block_reason && (
        <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-300 flex items-start gap-2">
          <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
          <div>
            <span className="font-semibold">{tr('operatingPolicy.cycleBlocked')}</span> {computation.block_reason}
          </div>
        </div>
      )}
    </div>
  );
}

function Stat({ label, value, badge }: {
  label: string;
  value: string;
  badge?: React.ReactNode;
}) {
  return (
    <div>
      <div className="text-xs text-gray-500 uppercase tracking-wide">{label}</div>
      <div className="mt-0.5 flex items-center gap-1.5">
        <span className="text-sm font-mono text-gray-200">{value}</span>
        {badge}
      </div>
    </div>
  );
}

export function OperatingPolicy() {
  const { tr } = useLanguage();
  const [groups, setGroups] = useState<Group[]>([]);
  const [tanksByGroup, setTanksByGroup] = useState<Map<string, Tank[]>>(new Map());
  const [selectedGroupId, setSelectedGroupId] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const liveTemps = useLiveTemps(); // 실측 수온 (사료공급 탭과 동일 소스, 60초 갱신)

  // 그룹 정책 — refreshKey 변경 시 재조회.
  const [groupPolicy, setGroupPolicyState] = useState<GroupPolicy>({
    bsf_mode: 'standard',
    max_daily_cycles: MAX_DAILY_CYCLES,
    daily_cycles: 0, // 0 = 자동 산출 (feeding-policy-store 기본값과 동일)
    operating_mode: DEFAULT_OPERATING_MODE,
    photoperiod_start_min: 9 * 60,
    photoperiod_end_min: 17 * 60,
    feeder_run_sec: 2,
    feeder_stop_sec: 6,
    feeder_spinup_sec: 11,          // 공급모터 리드 (feeding-policy-store 기본값과 동일)
  });

  useEffect(() => {
    if (selectedGroupId) {
      setGroupPolicyState(getGroupPolicy(selectedGroupId));
    }
  }, [selectedGroupId, refreshKey]);

  // localStorage 변경 이벤트 시 재렌더.
  useEffect(() => {
    const onChange = () => setRefreshKey(k => k + 1);
    window.addEventListener('bluei:feeding-policy-changed', onChange);
    return () => window.removeEventListener('bluei:feeding-policy-changed', onChange);
  }, []);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      // Phase 1b — 정책 캐시 워밍업 (group/tank metadata fetch). 실패해도 페이지 진행.
      await ensurePolicyLoaded().catch(() => {});
      const gRes = await Groups.list();
      setGroups(gRes.items);
      const map = new Map<string, Tank[]>();
      for (const g of gRes.items) {
        try {
          const tRes = await Groups.tanks(g.group_id);
          map.set(g.group_id, tRes.items);
        } catch {
          map.set(g.group_id, []);
        }
      }
      setTanksByGroup(map);
      if (gRes.items.length > 0 && !selectedGroupId) {
        setSelectedGroupId(gRes.items[0].group_id);
      }
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : tr('operatingPolicy.loadFailed'));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const selectedGroup = groups.find(g => g.group_id === selectedGroupId) ?? null;
  const tanks = tanksByGroup.get(selectedGroupId) ?? [];

  // BSF 일괄 적용 — 그룹은 "숨은 기본값 계층"이 아니라 명시적 도장 도구 (2026-07-08 운영자 확정).
  const [bulkBsf, setBulkBsf] = useState<BsfMode>('standard');

  function handleBulkBsfApply() {
    if (tanks.length === 0) return;
    if (!window.confirm(tr('operatingPolicy.bulkConfirm', {
      n: tanks.length, mode: tr(BSF_MODE_LABEL[bulkBsf]),
    }))) return;
    for (const t of tanks) {
      mergeTankOverride(t.tank_id, { bsf_mode: bulkBsf });
    }
    // 그룹 default 도 같은 값으로 — 이후 신규 수조의 초기값 (UI 에는 노출 안 함).
    if (selectedGroupId) setGroupPolicy(selectedGroupId, { bsf_mode: bulkBsf });
    setRefreshKey(k => k + 1);
  }

  // 운영 모드 요약 — 정본은 수조별 설정 (사료공급 탭). 여기는 읽기 전용 상황판.
  const modeCounts = useMemo(() => {
    let auto = 0, manual = 0;
    for (const t of tanks) {
      const eff = getEffectiveTankPolicy(t.tank_id, selectedGroupId);
      if (eff.operating_mode === 'auto') auto++;
      else manual++;
    }
    return { auto, manual };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tanks, selectedGroupId, refreshKey]);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2">
          <Settings className="w-4 h-4 text-green-400" />
          {tr('operatingPolicy.title')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {loading ? (
          <div className="space-y-3">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-48 w-full" />
          </div>
        ) : error ? (
          <div className="px-3 py-2 bg-destructive/10 border border-destructive/30 rounded text-sm text-destructive font-mono">
            {error}
          </div>
        ) : (
          <>
            {/* 그룹 선택 + 그룹 default 정책 */}
            <div className="flex flex-wrap items-end gap-x-6 gap-y-3 pb-3 border-b border-gray-700/30">
              <div className="flex flex-col gap-1">
                <label htmlFor="op-group" className="text-xs text-gray-400">{tr('operatingPolicy.group')}</label>
                <select
                  id="op-group"
                  value={selectedGroupId}
                  onChange={e => setSelectedGroupId(e.target.value)}
                  className="h-8 px-2 rounded border border-gray-700 bg-gray-800 text-sm text-white min-w-[14rem]"
                >
                  {groups.length === 0 && <option value="">{tr('operatingPolicy.noGroups')}</option>}
                  {groups.map(g => (
                    <option key={g.group_id} value={g.group_id}>{g.name}</option>
                  ))}
                </select>
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs text-gray-400">{tr('operatingPolicy.bulkBsf')}</label>
                <div className="flex items-center gap-2">
                  <ModeRadio value={bulkBsf} onChange={setBulkBsf} />
                  <button
                    type="button"
                    onClick={handleBulkBsfApply}
                    disabled={tanks.length === 0}
                    className="px-3 py-1 text-sm rounded border border-gray-600 text-gray-300 hover:text-white hover:border-gray-400 disabled:opacity-40"
                  >
                    {tr('operatingPolicy.bulkApplyBtn', { n: tanks.length })}
                  </button>
                </div>
              </div>

              <div className="flex flex-col gap-1">
                <label className="text-xs text-gray-400">{tr('operatingPolicy.operatingMode')}</label>
                <div className="flex items-center gap-2 h-8">
                  <span className="text-sm font-mono">
                    <span className="text-green-300">{tr('operatingPolicy.modeAutoCount', { n: modeCounts.auto })}</span>
                    <span className="text-gray-600"> · </span>
                    <span className="text-amber-300">{tr('operatingPolicy.modeManualCount', { n: modeCounts.manual })}</span>
                  </span>
                  <button
                    type="button"
                    onClick={gotoFeedTab}
                    className="inline-flex items-center gap-1 text-xs text-gray-500 hover:text-white underline underline-offset-2"
                  >
                    <ExternalLink className="w-3 h-3" />
                    {tr('operatingPolicy.modePerTankNote')}
                  </button>
                </div>
              </div>

              <div className="text-xs text-gray-500">
                {tr('operatingPolicy.dailyCycleCap')} <span className="text-gray-300 font-mono">{groupPolicy.max_daily_cycles}{tr('operatingPolicy.times')}</span>
              </div>
            </div>

            {selectedGroup?.description && (
              <p className="text-xs text-gray-500">{selectedGroup.description}</p>
            )}

            {/* 그룹 내 수조 — 세로 나열 */}
            {tanks.length === 0 ? (
              <p className="text-sm text-gray-500 py-8 text-center">
                {tr('operatingPolicy.noTanksInGroup')}
              </p>
            ) : (
              <div className="space-y-3">
                {tanks.map(t => (
                  <TankPolicyCard
                    key={t.tank_id}
                    tank={t}
                    group={selectedGroup}
                    liveTempC={liveTemps[t.tank_id] ?? null}
                    onChanged={() => setRefreshKey(k => k + 1)}
                  />
                ))}
              </div>
            )}

            <p className="text-xs text-gray-600 italic pt-2">
              {tr('operatingPolicy.storageNote')}
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}
