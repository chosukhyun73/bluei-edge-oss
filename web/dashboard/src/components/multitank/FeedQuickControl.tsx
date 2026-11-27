import { useEffect, useState } from 'react';
import { Play, Square, Bot, Hand } from 'lucide-react';
import type { FeedCycle, Tank, LLMAnalysis } from '../../lib/types';
import { FeedCycles, OperatorIntents } from '../../lib/api';
import { Button } from '../ui/button';
import { computeFeedingPolicy, BSF_MODE_LABEL, feedingWeightG } from '../../lib/species-policy';
import { getEffectiveTankPolicy } from '../../lib/feeding-policy-store';
import { LLMAnalysisModal } from './LLMAnalysisModal';
import { useLanguage } from '../../lib/language-core';
import { useLiveTemps } from '../../lib/use-live-temps';

type Variant = 'mini' | 'compact' | 'full';

type FeedQuickControlProps = {
  tank: Tank;
  variant?: Variant;
  onChanged?: () => void;
  // 즉시 실행 모드(카메라 확대뷰) — LLM 종합판단 생략, 클릭 즉시 사이클 시작.
  immediate?: boolean;
};

function fmtAge(startedAt: string, tr: (k: string, v?: Record<string, string | number>) => string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  if (ms < 60_000) return tr('feedQuick.ageSeconds', { s: Math.floor(ms / 1000) });
  if (ms < 3_600_000) return tr('feedQuick.ageMinutesSeconds', { m: Math.floor(ms / 60_000), s: Math.floor((ms % 60_000) / 1000) });
  return tr('feedQuick.ageHoursMinutes', { h: Math.floor(ms / 3_600_000), m: Math.floor((ms % 3_600_000) / 60_000) });
}

export function FeedQuickControl({ tank, variant = 'compact', onChanged, immediate = false }: FeedQuickControlProps) {
  const { tr } = useLanguage();
  const liveTemps = useLiveTemps();
  const [activeCycle, setActiveCycle] = useState<FeedCycle | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // F.2 — LLM 분석 modal
  const [llmModal, setLlmModal] = useState<{ intentId: string; analysis: LLMAnalysis | null } | null>(null);

  // 활성 사이클 polling (3초)
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | null = null;

    async function fetchActive() {
      try {
        const r = await FeedCycles.listForTank(tank.tank_id, 5);
        if (cancelled) return;
        // backend list 응답은 active/completed 이분법 — completed_at 기반으로 안전 판단.
        const active = r.items.find(c => !c.completed_at) ?? null;
        setActiveCycle(active);
      } catch {
        // silent
      }
    }
    void fetchActive();
    timer = setInterval(fetchActive, 3000);
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, [tank.tank_id]);

  // 정책 기반 자동 default 계산
  const effective = getEffectiveTankPolicy(tank.tank_id, tank.group_id ?? '');
  const computation = computeFeedingPolicy({
    species: tank.species,
    fish_count: tank.fish_count ?? null,
    avg_weight_g: feedingWeightG(tank),
    volume_m3: tank.volume_m3 ?? null,
    bsf_mode: effective.bsf_mode,
    temperature_c: liveTemps[tank.tank_id] ?? null,
    max_daily_cycles: effective.max_daily_cycles,
    daily_cycles: effective.daily_cycles,
  });

  const blocked = computation.block_reason != null;

  async function handleStart() {
    if (blocked || busy || activeCycle) return;
    setBusy(true);
    setError(null);
    try {
      // immediate 경로의 중복 급이 가드 — LLM 검수를 뺀 자리의 원래 목적(누가 방금 줬는데
      // 또 주는 것 방지)을 기록 기반으로 즉시 확인. 최근 1시간 내 급이가 있으면 확인 팝업.
      if (immediate) {
        const recent = await FeedCycles.listForTank(tank.tank_id, 20);
        const now = Date.now();
        const todayStart = new Date(); todayStart.setHours(0, 0, 0, 0);
        const todayG = recent.items
          .filter(c => new Date(c.started_at).getTime() >= todayStart.getTime())
          .reduce((s, c) => s + (c.total_amount_g ?? 0), 0);
        const last = recent.items.find(c => c.completed_at);
        const lastMs = last?.completed_at ? now - new Date(last.completed_at).getTime() : Infinity;
        if (lastMs < 60 * 60_000) {
          const ok = window.confirm(tr('feedQuick.duplicateGuard', {
            min: Math.max(1, Math.round(lastMs / 60_000)),
            g: Math.round(last?.total_amount_g ?? 0),
            today: Math.round(todayG),
          }));
          if (!ok) { setBusy(false); return; }
        }
      }
      // 운영자 의도 자동 기록 (수조 화면에서 트리거 = quick action).
      // immediate(카메라 뷰): LLM 종합판단 생략 — 클릭 즉시 사이클 시작 (2026-07-04 유령급이 방지).
      if (!immediate) setLlmModal({ intentId: '', analysis: null });
      const intent = await OperatorIntents.create({
        tank_id: tank.tank_id,
        intent_type: 'feed_now',
        reason: tr('feedQuick.quickStartReason', { policy: tr(BSF_MODE_LABEL[effective.bsf_mode]) }),
        ...(immediate ? { skip_llm: true } : {}),
      });
      // F.2 — LLM 분석이 있으면 modal 표시
      if (!immediate) setLlmModal(intent.llm_analysis ? { intentId: intent.intent_id, analysis: intent.llm_analysis } : null);
      await FeedCycles.start({
        tank_id: tank.tank_id,
        mode: 'adaptive',
        intent_id: intent.intent_id,
        params: {
          target_amount_g: computation.cycle_target_amount_g ?? 50,
          max_pulses: computation.feeding_pattern_default?.total_pulses,
          max_duration_min: computation.max_duration_min ?? 10,
          // onoff_feeder(on/off): run/stop 초로 마이크로펄스 도출(ESP32는 무시). 그룹정책 값.
          run_sec: effective.feeder_run_sec,
          stop_sec: effective.feeder_stop_sec,
          // Phase B: 단계 기반 ESP32 모터 출력 (살포 rpm / 공급 amount). null 이면 펌웨어 default.
          ...(computation.speed_rpm != null ? { speed_rpm: computation.speed_rpm } : {}),
          ...(computation.amount != null ? { amount: computation.amount } : {}),
        },
      });
      onChanged?.();
    } catch (err) {
      setLlmModal(null); // 판단 실패/중단 시 loading 모달이 갇히지 않게 닫는다
      setError(err instanceof Error ? err.message : tr('feedQuickControl.errorStart'));
    } finally {
      setBusy(false);
    }
  }

  async function handleStop() {
    if (!activeCycle || busy) return;
    setBusy(true);
    setError(null);
    try {
      await FeedCycles.stop(activeCycle.cycle_id);
      onChanged?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : tr('feedQuickControl.errorStop'));
    } finally {
      setBusy(false);
    }
  }

  // F.2 — LLM 분석 modal (모든 variant 공통)
  const llmModalEl = llmModal ? (
    <LLMAnalysisModal
      intentId={llmModal.intentId}
      analysis={llmModal.analysis}
      onApply={async (force: boolean) => {
        await OperatorIntents.apply(llmModal.intentId, { force });
      }}
      onClose={() => setLlmModal(null)}
    />
  ) : null;

  // ── mini variant — 카메라 카드 캡션 옆 (버튼만) ─────────────────────────
  if (variant === 'mini') {
    if (activeCycle) {
      return (
        <>
          {llmModalEl}
          <button
            type="button"
            disabled={busy}
            onClick={handleStop}
            className="inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded bg-red-600/80 text-white hover:bg-red-500 disabled:opacity-40 font-medium"
            title={tr('feedQuick.feedingStopTitle', { n: activeCycle.pulses_executed, m: activeCycle.max_pulses ?? '?' })}
          >
            <Square className="w-3 h-3" />
            {tr('feedQuickControl.stop')}
          </button>
        </>
      );
    }
    return (
      <>
        {llmModalEl}
        <button
          type="button"
          disabled={busy || blocked || !computation.cycle_target_amount_g}
          onClick={handleStart}
          className="inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded bg-green-600/80 text-white hover:bg-green-500 disabled:opacity-30 font-medium"
          title={
            blocked
              ? tr('feedQuick.startBlockedTitle', { reason: computation.block_reason ?? '' })
              : `${tr(BSF_MODE_LABEL[effective.bsf_mode])} · ${computation.cycle_target_amount_g}g/cycle`
          }
        >
          <Play className="w-3 h-3" />
          {tr('feedQuickControl.start')}
        </button>
      </>
    );
  }

  // ── compact variant ────────────────────────────────────────────────────
  if (variant === 'compact') {
    if (activeCycle) {
      return (
        <>
          {llmModalEl}
        <div className="flex items-center gap-2 px-2.5 py-1.5 bg-green-500/10 border border-green-500/30 rounded text-xs">
          <Bot className="w-3 h-3 text-green-400 flex-shrink-0" />
          <div className="flex-1 min-w-0">
            <span className="text-green-300 font-medium">{tr('feedQuickControl.feeding')}</span>
            <span className="text-gray-500 ml-1.5">
              {tr('feedQuick.pulsesUnit', { n: activeCycle.pulses_executed, m: activeCycle.max_pulses ?? '?' })} ·
              {' '}{activeCycle.total_amount_g}g · {fmtAge(activeCycle.started_at, tr)}
            </span>
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={handleStop}
            className="h-6 px-2 text-xs border-red-500/40 text-red-300 hover:bg-red-500/10"
          >
            <Square className="w-3 h-3 mr-1" />
            {tr('feedQuickControl.stop')}
          </Button>
        </div>
        </>
      );
    }
    return (
      <>
        {llmModalEl}
      <div className="space-y-1">
        <div className="flex items-center gap-2 px-2.5 py-1.5 bg-gray-800/40 border border-gray-700/40 rounded text-xs">
          <Hand className="w-3 h-3 text-gray-500 flex-shrink-0" />
          <div className="flex-1 min-w-0">
            {blocked ? (
              <span className="text-red-400">{tr('feedQuickControl.blockedDensity')}</span>
            ) : (
              <span className="text-gray-400">
                {tr(BSF_MODE_LABEL[effective.bsf_mode])} · {computation.cycle_target_amount_g ?? '—'}g/cycle
              </span>
            )}
          </div>
          <Button
            size="sm"
            disabled={busy || blocked || !computation.cycle_target_amount_g}
            onClick={handleStart}
            className="h-6 px-2 text-xs"
          >
            <Play className="w-3 h-3 mr-1" />
            {tr('feedQuickControl.start')}
          </Button>
          {error && <span className="text-xs text-red-400 ml-2">{error}</span>}
        </div>
        {!blocked && computation.get80_h != null && (
          <div className="px-2.5 flex items-center gap-x-2.5 gap-y-0.5 flex-wrap text-[11px] text-gray-500 font-mono">
            <span title={tr('feedQuickControl.planDigestTip')}>GET {computation.get95_h?.toFixed(1)}h</span>
            <span className="text-gray-600">·</span>
            <span title={tr('feedQuickControl.planRefeedTip')}>{tr('feedQuickControl.planRefeed')} {computation.get80_h.toFixed(1)}h</span>
            <span className="text-gray-600">·</span>
            <span title={tr('feedQuickControl.planCyclesTip')}>{tr('feedQuickControl.planCyclesPerDay', { n: computation.daily_cycles ?? '—' })}</span>
          </div>
        )}
      </div>
      </>
    );
  }

  // ── full variant ───────────────────────────────────────────────────────
  return (
    <>
      {llmModalEl}
    <div className="space-y-2">
      {activeCycle ? (
        <div className="p-3 bg-green-500/10 border border-green-500/30 rounded space-y-2">
          <div className="flex items-baseline justify-between">
            <span className="text-sm font-semibold text-green-300 flex items-center gap-1.5">
              <Bot className="w-3.5 h-3.5" />
              {tr('feedQuickControl.autoFeedingInProgress')}
            </span>
            <span className="text-xs text-gray-500 font-mono">{activeCycle.cycle_id.slice(-12)}</span>
          </div>
          <div className="grid grid-cols-3 gap-2 text-xs">
            <div>
              <div className="text-gray-500">{tr('feedQuickControl.pulses')}</div>
              <div className="text-gray-200 font-mono">
                {activeCycle.pulses_executed}/{activeCycle.max_pulses ?? '?'}
              </div>
            </div>
            <div>
              <div className="text-gray-500">{tr('feedQuickControl.amountSupplied')}</div>
              <div className="text-gray-200 font-mono">
                {activeCycle.total_amount_g}
                {activeCycle.target_amount_g != null && `/${activeCycle.target_amount_g}`}g
              </div>
            </div>
            <div>
              <div className="text-gray-500">{tr('feedQuickControl.elapsed')}</div>
              <div className="text-gray-200 font-mono">{fmtAge(activeCycle.started_at, tr)}</div>
            </div>
          </div>
          <Button
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={handleStop}
            className="w-full border-red-500/40 text-red-300 hover:bg-red-500/10"
          >
            <Square className="w-3.5 h-3.5 mr-1" />
            {tr('feedQuickControl.feedStop')}
          </Button>
        </div>
      ) : (
        <div className={`p-3 ${blocked ? 'bg-red-500/5 border-red-500/30' : 'bg-gray-800/40 border-gray-700/40'} border rounded space-y-2`}>
          <div className="flex items-baseline justify-between">
            <span className="text-sm font-semibold text-gray-300 flex items-center gap-1.5">
              <Hand className="w-3.5 h-3.5" />
              {tr('feedQuickControl.feedStandby')}
            </span>
            <span className="text-xs text-gray-500">
              {tr('feedQuickControl.policy')} <span className="text-gray-300">{tr(BSF_MODE_LABEL[effective.bsf_mode])}</span>
              {effective.is_override && <span className="text-amber-300"> · override</span>}
            </span>
          </div>
          {!blocked && computation.cycle_target_amount_g != null && (
            <div className="grid grid-cols-3 gap-2 text-xs">
              <div>
                <div className="text-gray-500">{tr('feedQuickControl.targetAmount')}</div>
                <div className="text-gray-200 font-mono">{computation.cycle_target_amount_g}g</div>
              </div>
              <div>
                <div className="text-gray-500">{tr('feedQuickControl.totalPulses')}</div>
                <div className="text-gray-200 font-mono">
                  {computation.feeding_pattern_default?.total_pulses ?? '—'}
                </div>
              </div>
              <div>
                <div className="text-gray-500">{tr('feedQuickControl.maxDuration')}</div>
                <div className="text-gray-200 font-mono">{tr('feedQuick.minutesUnit', { n: computation.max_duration_min ?? '—' })}</div>
              </div>
            </div>
          )}
          {blocked && (
            <p className="text-xs text-red-300">⚠ {computation.block_reason}</p>
          )}
          <Button
            size="sm"
            disabled={busy || blocked || !computation.cycle_target_amount_g}
            onClick={handleStart}
            className="w-full"
          >
            <Play className="w-3.5 h-3.5 mr-1" />
            {tr('feedQuickControl.feedStartAuto')}
          </Button>
        </div>
      )}
      {error && (
        <div className="text-xs text-red-400 font-mono px-2">{error}</div>
      )}
    </div>
    </>
  );
}
