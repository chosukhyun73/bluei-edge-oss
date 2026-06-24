import { useState, useEffect, useCallback, useRef } from 'react';
import { useLanguage } from '../../lib/language-context';
import type {
  FeedCycle,
  FeedCycleMode,
  FeedCycleStatus,
  Tank,
  FeedingSchedule,
  NewScheduleBody,
  OperatorIntent,
  LLMAnalysis,
} from '../../lib/types';
import { FeedCycles, OperatorIntents, Tanks, Schedules, ApiError } from '../../lib/api';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Button } from '../ui/button';
import { Skeleton } from '../ui/skeleton';
import { DisputeModal } from './DisputeModal';
import { LLMAnalysisModal } from './LLMAnalysisModal';
import { SafetyGateBadges } from './SafetyGateBadges';
import {
  computeFeedingPolicy,
  BSF_MODE_LABEL,
  type BsfMode,
  type FeedingPolicyComputation,
} from '../../lib/species-policy';
import {
  getEffectiveTankPolicy,
  setTankOverride,
  type OperatingMode,
} from '../../lib/feeding-policy-store';
import { safetyGateMessage } from '../../lib/format';

// ── Constants ─────────────────────────────────────────────────────────────────

// backend list 응답은 active/completed 이분법. cycle_complete 는 state machine 내부 값.
const TERMINAL_STATUSES = new Set<FeedCycleStatus>(['cycle_complete', 'completed']);
const TERMINAL_REASONS = new Set(['operator_stop', 'safety_block']);

// 완료된 사이클 판단 — completed_at 기반이 가장 robust (status 문자열 변형 무관)
function isCycleCompleted(c: FeedCycle): boolean {
  return c.completed_at != null && c.completed_at !== '';
}

function getStatusLabels(tr: (k: string) => string): Record<FeedCycleStatus, string> {
  return {
    idle: tr('feedCycle.status.idle'),
    pulse_active: tr('feedCycle.status.pulseActive'),
    pulse_complete: tr('feedCycle.status.pulseComplete'),
    gap_observation: tr('feedCycle.status.gapObservation'),
    cycle_complete: tr('feedCycle.status.cycleComplete'),
    active: tr('feedCycle.status.active'),
    completed: tr('feedCycle.status.completed'),
    completing: tr('feedCycle.status.completing'),
  };
}

// ── Status badge ──────────────────────────────────────────────────────────────

function CycleStatusBadge({
  status,
  terminationReason,
}: {
  status: FeedCycleStatus;
  terminationReason?: string | null;
}) {
  const { tr } = useLanguage();
  let cls = '';
  if (status === 'idle') {
    cls = 'bg-gray-500/15 text-gray-400 border-gray-500/30';
  } else if (status === 'pulse_active') {
    cls = 'bg-blue-500/15 text-blue-300 border-blue-500/30 animate-pulse';
  } else if (status === 'pulse_complete') {
    cls = 'bg-sky-500/15 text-sky-300 border-sky-500/30';
  } else if (status === 'gap_observation') {
    cls = 'bg-amber-500/15 text-amber-300 border-amber-500/30';
  } else if (status === 'cycle_complete' || status === 'completed' || status === 'completing') {
    const isNegative = terminationReason && TERMINAL_REASONS.has(terminationReason);
    cls = isNegative
      ? 'bg-red-500/15 text-red-300 border-red-500/30'
      : 'bg-green-500/15 text-green-300 border-green-500/30';
  } else if (status === 'active') {
    cls = 'bg-blue-500/15 text-blue-300 border-blue-500/30';
  }

  return (
    <span
      className={[
        'inline-flex items-center px-2 py-0.5 rounded-md border text-xs font-medium',
        cls,
      ].join(' ')}
    >
      {getStatusLabels(tr)[status]}
    </span>
  );
}

// ── Progress bar ──────────────────────────────────────────────────────────────

function ProgressBar({ value, max, label }: { value: number; max: number; label: string }) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  return (
    <div>
      <div className="flex justify-between text-xs text-gray-400 mb-1">
        <span>{label}</span>
        <span>
          {value} / {max}
        </span>
      </div>
      <div className="h-2 rounded-full bg-gray-700 overflow-hidden">
        <div
          className="h-full rounded-full bg-blue-500 transition-all duration-500"
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

// ── Cycle detail panel ────────────────────────────────────────────────────────

interface CycleDetailProps {
  cycle: FeedCycle;
  onStop: (id: string) => Promise<void>;
  stopping: boolean;
  tank?: Tank | null;
  intentReason?: string | null;
  onDispute?: (cycle: FeedCycle) => void;
  disputed?: boolean;
}

function CycleDetail({
  cycle,
  onStop,
  stopping,
  tank,
  intentReason,
  onDispute,
  disputed,
}: CycleDetailProps) {
  const { tr } = useLanguage();
  const isTerminal = isCycleCompleted(cycle) || TERMINAL_STATUSES.has(cycle.status);
  const showAmountProgress =
    cycle.mode === 'adaptive' &&
    cycle.target_amount_g != null &&
    cycle.target_amount_g > 0;
  const showPulseProgress =
    cycle.max_pulses != null && cycle.max_pulses > 0;
  // Phase 5 (load cell): HX711 weight event 수신 시 actual_total > 0.
  // 0 또는 null 이면 stub (TotalAmountG = 1g/sec 시간 기반) 만 표시.
  const hasActualWeight =
    cycle.actual_total_amount_g != null && cycle.actual_total_amount_g > 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-2 items-center justify-between">
        <div>
          <p className="text-xs text-gray-500 font-mono">{cycle.cycle_id}</p>
          <p className="text-sm text-gray-300 mt-0.5">
            Cage/Tank: <span className="font-mono text-white">{cycle.tank_id}</span>
          </p>
          <p className="text-sm text-gray-400 mt-0.5">
            {tr('cycleDetail.mode')}: {cycle.mode === 'adaptive' ? tr('cycleDetail.modeAdaptive') : tr('cycleDetail.modeFixed')}
          </p>
          {(tank?.lifecycle_stage || tank?.lot_no) && (
            <div className="flex flex-wrap items-center gap-1.5 mt-1">
              {tank.lifecycle_stage && (
                <span className="px-1.5 py-0.5 text-xs rounded border bg-blue-500/15 text-blue-300 border-blue-500/30">
                  {tank.lifecycle_stage}
                </span>
              )}
              {tank.lot_no && (
                <span className="text-xs text-gray-500 font-mono">{tr('feedCycle.lot', { n: tank.lot_no })}</span>
              )}
            </div>
          )}
        </div>
        <CycleStatusBadge status={cycle.status} terminationReason={cycle.termination_reason} />
      </div>

      {showAmountProgress && (
        <ProgressBar
          value={hasActualWeight ? cycle.actual_total_amount_g! : cycle.total_amount_g}
          max={cycle.target_amount_g!}
          label={hasActualWeight ? tr('cycleDetail.actualFeedAmount') : tr('cycleDetail.estimatedFeedAmount')}
        />
      )}
      {showPulseProgress && (
        <ProgressBar
          value={cycle.pulses_executed}
          max={cycle.max_pulses!}
          label={tr('cycleDetail.pulseCount')}
        />
      )}

      {/* Phase 5 (load cell): 추정 vs 실측 2열 비교. weight event 미수신이면 안내만. */}
      <div className="grid grid-cols-2 gap-2 text-xs">
        <div className="px-2 py-1.5 rounded-md bg-gray-700/40 border border-gray-700">
          <p className="text-gray-500 uppercase tracking-wide mb-0.5">{tr('cycleDetail.estimatedTimeBased')}</p>
          <p className="font-mono text-gray-300">{cycle.total_amount_g.toFixed(2)} g</p>
        </div>
        <div
          className={
            'px-2 py-1.5 rounded-md border ' +
            (hasActualWeight
              ? 'bg-emerald-500/10 border-emerald-500/30'
              : 'bg-gray-700/20 border-gray-700 opacity-60')
          }
        >
          <p
            className={
              'uppercase tracking-wide mb-0.5 ' +
              (hasActualWeight ? 'text-emerald-300' : 'text-gray-500')
            }
          >
            {tr('cycleDetail.actualHX711')}
          </p>
          <p
            className={
              'font-mono ' + (hasActualWeight ? 'text-emerald-200' : 'text-gray-500')
            }
          >
            {hasActualWeight ? `${cycle.actual_total_amount_g!.toFixed(2)} g` : tr('cycleDetail.waiting')}
          </p>
        </div>
      </div>

      {/* Phase 5: 사료통 빈 감지 — 3 연속 펄스 ≤1g */}
      {cycle.silo_depletion_warned && (
        <div className="px-3 py-2 bg-orange-500/15 border border-orange-500/40 rounded-md">
          <p className="text-xs text-orange-300 uppercase tracking-wide mb-0.5">
            {tr('cycleDetail.siloDepletionTitle')}
          </p>
          <p className="text-xs text-orange-200 leading-relaxed">
            {tr('cycleDetail.siloDepletionMsg')}
          </p>
        </div>
      )}

      {cycle.termination_reason && (
        <p className="text-xs text-amber-400 font-mono px-3 py-2 bg-amber-500/10 border border-amber-500/20 rounded-md">
          {tr('cycleDetail.terminationReason')}: {cycle.termination_reason}
        </p>
      )}

      {/* 운영자 의도 메모 — intent_id 연결 시 표시 */}
      {intentReason && (
        <div className="px-3 py-2 bg-blue-500/10 border border-blue-500/20 rounded-md">
          <p className="text-xs text-blue-400 uppercase tracking-wide mb-0.5">{tr('cycleDetail.operatorIntent')}</p>
          <p className="text-xs text-blue-200 leading-relaxed">{intentReason}</p>
        </div>
      )}

      <div className="grid grid-cols-2 gap-2 text-xs text-gray-400">
        <div>
          <p className="text-gray-500 uppercase tracking-wide mb-0.5">{tr('cycleDetail.startedAt')}</p>
          <p className="font-mono text-gray-300">
            {new Date(cycle.started_at).toLocaleString('ko-KR')}
          </p>
        </div>
        {cycle.completed_at && (
          <div>
            <p className="text-gray-500 uppercase tracking-wide mb-0.5">{tr('cycleDetail.completedAt')}</p>
            <p className="font-mono text-gray-300">
              {new Date(cycle.completed_at).toLocaleString('ko-KR')}
            </p>
          </div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {!isTerminal && (
          <Button
            size="sm"
            variant="outline"
            disabled={stopping}
            onClick={() => void onStop(cycle.cycle_id)}
            className="border-red-500/40 text-red-400 hover:bg-red-500/10 hover:text-red-300"
            aria-label={`${tr('cycleDetail.cycleLabel')} ${cycle.cycle_id} ${tr('cycleDetail.stopLabel')}`}
          >
            {stopping ? tr('cycleDetail.stopping') : tr('cycleDetail.emergencyStop')}
          </Button>
        )}
        {/* C-3l: 이 결정에 이의 제기 — decision_id 있을 때만 활성 */}
        {onDispute && (
          <Button
            size="sm"
            variant="outline"
            disabled={!cycle.decision_id}
            onClick={() => onDispute(cycle)}
            className="border-amber-500/40 text-amber-400 hover:bg-amber-500/10 hover:text-amber-300 disabled:opacity-40"
            aria-label={tr('cycleDetail.disputeBtn')}
            title={
              cycle.decision_id
                ? tr('cycleDetail.disputeTitle')
                : tr('cycleDetail.disputeNotApplicable')
            }
          >
            {tr('cycleDetail.disputeBtn')}
          </Button>
        )}
        {disputed && (
          <span className="text-xs px-2 py-0.5 rounded-md border bg-amber-500/15 text-amber-300 border-amber-500/30">
            {tr('cycleDetail.disputed')}
          </span>
        )}
      </div>
    </div>
  );
}

// ── New cycle form (수동 모드 전용) ────────────────────────────────────────────

interface NewCycleFormProps {
  tanks: Tank[];
  fixedTankId?: string;          // 단일 수조 컨텍스트 고정 시 selector 숨김
  onSuccess: () => void;
  onCancel?: () => void;
  defaultMode?: FeedCycleMode;    // 수동 섹션에서는 'fixed' 강제
}

function NewCycleForm({
  tanks,
  fixedTankId,
  onSuccess,
  onCancel,
  defaultMode = 'adaptive',
}: NewCycleFormProps) {
  const { tr } = useLanguage();
  const [tankId, setTankId] = useState(fixedTankId ?? tanks[0]?.tank_id ?? '');
  const [mode, setMode] = useState<FeedCycleMode>(defaultMode);
  // adaptive — 정책 기반 자동 default (tank 변경 시 재계산)
  const [targetAmountG, setTargetAmountG] = useState('');
  const [maxPulses, setMaxPulses] = useState('');
  const [maxDurationMin, setMaxDurationMin] = useState('');
  // fixed
  const [pulseDurationMs, setPulseDurationMs] = useState('');
  const [gapMs, setGapMs] = useState('');
  const [totalPulses, setTotalPulses] = useState('');
  // Phase B: ESP32 모터 출력 (살포 rpm 14~42 · 공급 amount 0~255). 빈 문자열 = 펌웨어 default.
  const [speedRpm, setSpeedRpm] = useState('');
  const [amount, setAmount] = useState('');
  // 1일 목표 + 사이클 간격
  const [dailyTargetG, setDailyTargetG] = useState('');
  const [cycleIntervalMin, setCycleIntervalMin] = useState('');
  const [todayConsumedG, setTodayConsumedG] = useState<number>(0);
  const [allowOverDaily, setAllowOverDaily] = useState(false);
  // 활성 사이클 사전 체크 (409 Conflict 방지)
  const [existingActiveCycle, setExistingActiveCycle] = useState<FeedCycle | null>(null);
  // 운영자 의도 메모 (선택)
  const [intentReason, setIntentReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // F.2 — LLM 분석 modal
  const [llmModal, setLlmModal] = useState<{ intentId: string; analysis: LLMAnalysis | null } | null>(null);

  // fixedTankId 변경 시 동기화 (수동 섹션에서 상위 selectedTankId 와 일치)
  useEffect(() => {
    if (fixedTankId) setTankId(fixedTankId);
  }, [fixedTankId]);

  // 선택된 수조의 정책 기반 자동 default 계산 (tank 변경 또는 정책 변경 시 재실행)
  const selectedTank = tanks.find(t => t.tank_id === tankId) ?? null;
  const policyComputation = selectedTank ? (() => {
    const effective = getEffectiveTankPolicy(tankId, selectedTank.group_id ?? '');
    return computeFeedingPolicy({
      species: selectedTank.species,
      fish_count: selectedTank.fish_count ?? null,
      avg_weight_g: selectedTank.avg_weight_g ?? null,
      volume_m3: selectedTank.volume_m3 ?? null,
      bsf_mode: effective.bsf_mode,
      temperature_c: null,
      max_daily_cycles: effective.max_daily_cycles,
    });
  })() : null;

  useEffect(() => {
    if (!policyComputation) return;
    // adaptive default
    if (policyComputation.cycle_target_amount_g != null) {
      setTargetAmountG(String(policyComputation.cycle_target_amount_g));
    }
    if (policyComputation.feeding_pattern_default) {
      setMaxPulses(String(policyComputation.feeding_pattern_default.total_pulses));
      setPulseDurationMs(String(policyComputation.feeding_pattern_default.pulse_duration_ms));
      setGapMs(String(policyComputation.feeding_pattern_default.gap_ms));
      setTotalPulses(String(policyComputation.feeding_pattern_default.total_pulses));
      // Phase B: ESP32 모터 출력 default — 단계 기반.
      setSpeedRpm(String(policyComputation.feeding_pattern_default.speed_rpm));
      setAmount(String(policyComputation.feeding_pattern_default.amount));
    }
    if (policyComputation.max_duration_min != null) {
      setMaxDurationMin(String(policyComputation.max_duration_min));
    }
    // 1일 목표 + 사이클 간격 (분) — 정책 기반.
    if (policyComputation.daily_feed_g != null) {
      setDailyTargetG(String(Math.round(policyComputation.daily_feed_g)));
    }
    if (policyComputation.get50_h != null) {
      setCycleIntervalMin(String(Math.round(policyComputation.get50_h * 60)));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tankId, policyComputation?.bsf_mode, policyComputation?.cycle_target_amount_g]);

  // 오늘 누적 사료량 + 활성 사이클 — 선택 수조 polling (5초)
  useEffect(() => {
    if (!tankId) {
      setTodayConsumedG(0);
      setExistingActiveCycle(null);
      return;
    }
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | null = null;

    const fetchTankCycles = () => {
      FeedCycles.listForTank(tankId, 50)
        .then(r => {
          if (cancelled) return;
          const todayStart = new Date();
          todayStart.setHours(0, 0, 0, 0);
          const sum = r.items
            .filter(c => new Date(c.started_at).getTime() >= todayStart.getTime())
            .reduce((acc, c) => acc + (c.total_amount_g ?? 0), 0);
          setTodayConsumedG(sum);
          // 완료되지 않은 cycle 만 활성으로 판단 (completed_at 기반).
          const active = r.items.find(c => !isCycleCompleted(c)) ?? null;
          setExistingActiveCycle(active);
        })
        .catch(() => {
          if (!cancelled) {
            setTodayConsumedG(0);
            setExistingActiveCycle(null);
          }
        });
    };

    fetchTankCycles();
    timer = setInterval(fetchTankCycles, 5_000);
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, [tankId]);

  const dailyTargetNum = Number(dailyTargetG) || 0;
  const targetCycleNum = Number(targetAmountG) || 0;
  const projectedDailyAfter = todayConsumedG + targetCycleNum;
  const willExceedDaily = dailyTargetNum > 0 && projectedDailyAfter > dailyTargetNum;
  const blocked =
    existingActiveCycle
      ? tr('feedCycle.blockedActiveCycle', {
          id: existingActiveCycle.cycle_id.slice(-12),
          mode: existingActiveCycle.mode === 'adaptive' ? tr('feedCycle.modeAuto') : tr('feedCycle.modeManual'),
          pulses: existingActiveCycle.pulses_executed,
        })
      : policyComputation?.block_reason
        ?? (willExceedDaily && !allowOverDaily
            ? tr('feedCycle.blockedDailyExceed', {
                consumed: todayConsumedG,
                cycle: targetCycleNum,
                projected: projectedDailyAfter,
                target: dailyTargetNum,
              })
            : null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!tankId) { setError(tr('newCycle.errorSelectTank')); return; }
    if (blocked) { setError(`${tr('newCycle.blockedLabel')}: ${blocked}`); return; }
    setSubmitting(true);
    setError(null);
    try {
      // 의도 메모가 있으면 먼저 기록
      let intentId: string | undefined;
      if (intentReason.trim()) {
        setLlmModal({ intentId: '', analysis: null });
        const res = await OperatorIntents.create({
          tank_id: tankId,
          intent_type: 'feed_now',
          reason: intentReason.trim(),
        });
        intentId = res.intent_id;
        // F.2 — LLM 분석이 있으면 modal 표시 (비동기 — cycle 시작은 계속 진행)
        setLlmModal(res.llm_analysis ? { intentId: res.intent_id, analysis: res.llm_analysis } : null);
      }

      // Phase B: ESP32 모터 출력 — 0 이면 펌웨어 default 사용 (미전송).
      const speedRpmNum = speedRpm ? Number(speedRpm) : 0;
      const amountNum = amount ? Number(amount) : 0;
      await FeedCycles.start({
        tank_id: tankId,
        mode,
        intent_id: intentId,
        params: mode === 'adaptive'
          ? {
              target_amount_g: Number(targetAmountG),
              max_pulses: maxPulses ? Number(maxPulses) : undefined,
              max_duration_min: maxDurationMin ? Number(maxDurationMin) : undefined,
              pulse_duration_ms: pulseDurationMs ? Number(pulseDurationMs) : undefined,
              gap_ms: gapMs ? Number(gapMs) : undefined,
              ...(speedRpmNum > 0 ? { speed_rpm: speedRpmNum } : {}),
              ...(amountNum > 0 ? { amount: amountNum } : {}),
            }
          : {
              pulse_duration_ms: Number(pulseDurationMs),
              gap_ms: Number(gapMs),
              total_pulses: Number(totalPulses),
              ...(speedRpmNum > 0 ? { speed_rpm: speedRpmNum } : {}),
              ...(amountNum > 0 ? { amount: amountNum } : {}),
            },
      });
      onSuccess();
    } catch (err: unknown) {
      // backend 코드별 한국어 안내 — 운영자가 원인을 즉시 알 수 있게.
      if (err instanceof ApiError) {
        switch (err.code) {
          case 'SAFETY_GATE_BLOCKED': {
            const reason = (err.details?.rejection_reason as string | undefined) ?? '';
            setError(`${tr('newCycle.errorSafetyGate')} ${safetyGateMessage(reason)}`);
            break;
          }
          case 'CYCLE_CONFLICT':
            setError(tr('newCycle.errorCycleConflict'));
            break;
          case 'INVALID_MODE':
            setError(tr('newCycle.errorInvalidMode'));
            break;
          case 'MISSING_INTENT_TYPE':
            setError(tr('newCycle.errorMissingIntentType'));
            break;
          case 'MISSING_REASON':
            setError(tr('newCycle.errorMissingReason'));
            break;
          case 'TANK_NOT_FOUND':
            setError(tr('newCycle.errorTankNotFound'));
            break;
          default:
            setError(`[${err.code}] ${err.message}`);
        }
      } else {
        setError(err instanceof Error ? err.message : tr('newCycle.errorUnknown'));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      {llmModal && (
        <LLMAnalysisModal
          intentId={llmModal.intentId}
          analysis={llmModal.analysis}
          onApply={async (force: boolean) => {
            await OperatorIntents.apply(llmModal.intentId, { force });
          }}
          onClose={() => setLlmModal(null)}
        />
      )}
    <Card className="bg-gray-800/60 border-blue-500/20">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm text-blue-400">
          {fixedTankId ? tr('newCycle.titleTestManual') : tr('newCycle.titleNewCycle')}
        </CardTitle>
      </CardHeader>
      <CardContent className="pb-5">
        <form onSubmit={handleSubmit} className="space-y-4" aria-label={tr('newCycle.formAriaLabel')}>
          {error && (
            <p className="text-xs text-red-400 font-mono px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md">
              {error}
            </p>
          )}

          {/* 정책 기반 자동 default 안내 */}
          {policyComputation && policyComputation.daily_feed_g != null && (
            <div className="px-3 py-2 bg-blue-500/10 border border-blue-500/30 rounded text-xs space-y-1">
              <div className="text-blue-300">
                <span className="font-semibold">{tr('newCycle.policyAutoCalc')}</span>{' '}
                {tr(BSF_MODE_LABEL[policyComputation.bsf_mode])} · {policyComputation.stage_label}{' '}
                · BSF {tr('feedCycle.bsfPercentPerDay', { v: policyComputation.bsf_percent?.toFixed(1) ?? '—' })}
              </div>
              <div className="text-gray-400">
                {tr('newCycle.dailyFeedAmount')} <span className="text-gray-200 font-mono">
                  {policyComputation.daily_feed_g?.toLocaleString('ko-KR')}g
                </span>
                {' '}÷ {tr('feedCycle.cyclesPerDay', { n: policyComputation.daily_cycles ?? '—' })} ={' '}
                <span className="text-gray-200 font-mono">{policyComputation.cycle_target_amount_g}g/cycle</span>
                {' · '}{tr('newCycle.temperature')} {policyComputation.temperature_c}℃ ({policyComputation.temperature_source === 'sensor' ? tr('newCycle.tempMeasured') : 'fallback'})
                {' · '}GET₅₀ {policyComputation.get50_h?.toFixed(1)}h
              </div>
            </div>
          )}

          {/* 1일 목표 + 오늘 누적 + 사이클 간격 (사용자 변경 가능) */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 px-3 py-3 bg-gray-900/40 border border-gray-700/40 rounded">
            <NumberField
              id="fc-daily-target"
              label={tr('newCycle.labelDailyTarget')}
              value={dailyTargetG}
              onChange={setDailyTargetG}
              placeholder="18000"
            />
            <div>
              <label className="text-xs text-gray-400 font-medium block mb-1">{tr('newCycle.labelTodayConsumed')}</label>
              <div className="h-8 px-3 py-1.5 bg-gray-900 border border-gray-700 rounded text-sm font-mono">
                <span className={willExceedDaily ? 'text-red-300' : 'text-gray-200'}>
                  {todayConsumedG.toLocaleString('ko-KR')}
                </span>
                {dailyTargetNum > 0 && (
                  <span className="text-gray-500 ml-1">
                    / {dailyTargetNum.toLocaleString('ko-KR')}g ({((todayConsumedG / dailyTargetNum) * 100).toFixed(0)}%)
                  </span>
                )}
              </div>
            </div>
            <NumberField
              id="fc-interval"
              label={tr('newCycle.labelCycleInterval')}
              value={cycleIntervalMin}
              onChange={setCycleIntervalMin}
              placeholder="312"
            />
          </div>

          {/* 1일 초과 시 사용자 동의 override */}
          {willExceedDaily && (
            <label className="flex items-start gap-2 px-3 py-2 bg-amber-500/10 border border-amber-500/30 rounded text-xs cursor-pointer">
              <input
                type="checkbox"
                checked={allowOverDaily}
                onChange={e => setAllowOverDaily(e.target.checked)}
                className="mt-0.5 accent-amber-500"
              />
              <span className="text-amber-200">
                <span className="font-semibold">{tr('newCycle.dailyExceedWarning')}</span>{' '}
                {tr('newCycle.dailyExceedHint')} {tr('feedCycle.dailyExceedDetail', {
                  projected: projectedDailyAfter.toLocaleString('ko-KR'),
                  target: dailyTargetNum.toLocaleString('ko-KR'),
                })}
                {tr('newCycle.dailyExceedNote')}
              </span>
            </label>
          )}

          {/* 활성 사이클 사전 안내 — 가장 흔한 차단 사유 (409 conflict 예방) */}
          {existingActiveCycle && (
            <div className="px-3 py-2 bg-amber-500/10 border border-amber-500/40 rounded text-xs space-y-1">
              <div className="text-amber-200 font-semibold">
                ⚠ {tr('newCycle.existingCycleWarning')}
              </div>
              <div className="text-amber-100/80 leading-relaxed">
                cycle <span className="font-mono">{existingActiveCycle.cycle_id.slice(-12)}</span>{' '}
                · {existingActiveCycle.mode === 'adaptive' ? `🤖 ${tr('newCycle.modeAuto')}` : `✋ ${tr('newCycle.modeManual')}`}
                {' '}· {tr('feedCycle.pulsesProgress', { n: existingActiveCycle.pulses_executed, m: existingActiveCycle.max_pulses ?? '?' })}
                {' '}· {tr('feedCycle.amountSupplied', { g: existingActiveCycle.total_amount_g })}
              </div>
              <div className="text-amber-100/70">
                {tr('newCycle.existingCycleGuide')}
              </div>
            </div>
          )}

          {/* 차단 사유 — 활성 사이클 외 다른 이유 (정책 / 1일 초과 등) */}
          {blocked && !existingActiveCycle && (
            <div className="px-3 py-2 bg-red-500/10 border border-red-500/30 rounded text-xs text-red-300">
              <span className="font-semibold">⚠ {tr('newCycle.blockedLabel')}</span> {blocked}
            </div>
          )}

          {/* Tank selector — 단일 수조 컨텍스트에서는 숨김 */}
          {!fixedTankId && (
            <div className="flex flex-col gap-1">
              <label htmlFor="fc-tank" className="text-xs text-gray-400 font-medium">
                Cage/Tank *
              </label>
              <select
                id="fc-tank"
                value={tankId}
                onChange={e => setTankId(e.target.value)}
                className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/50"
              >
                {tanks.map(t => (
                  <option key={t.tank_id} value={t.tank_id}>
                    {t.lifecycle_stage && t.lot_no
                      ? `${t.tank_id} — ${t.lifecycle_stage} · Lot ${t.lot_no}`
                      : t.lifecycle_stage
                      ? `${t.tank_id} — ${t.lifecycle_stage}`
                      : t.lot_no
                      ? `${t.tank_id} · Lot ${t.lot_no}`
                      : t.display_name || t.tank_id}
                  </option>
                ))}
              </select>
            </div>
          )}

          {/* Mode radio — 자동 (AI) / 수동 (운영자 제어). 수동 섹션에서는 단일 모드만 노출 */}
          {!fixedTankId && (
            <div className="flex flex-col gap-1">
              <p className="text-xs text-gray-400 font-medium">{tr('newCycle.operatingModeLabel')}</p>
              <div className="flex gap-3">
                {(['adaptive', 'fixed'] as FeedCycleMode[]).map(m => (
                  <label
                    key={m}
                    className={
                      mode === m
                        ? 'flex-1 px-3 py-2 rounded border border-green-500/60 bg-green-600/10 cursor-pointer'
                        : 'flex-1 px-3 py-2 rounded border border-gray-700 hover:border-gray-600 cursor-pointer'
                    }
                  >
                    <input
                      type="radio"
                      name="fc-mode"
                      value={m}
                      checked={mode === m}
                      onChange={() => setMode(m)}
                      className="accent-green-500 mr-2"
                    />
                    <span className="text-sm text-white font-medium">
                      {m === 'adaptive' ? `🤖 ${tr('newCycle.modeAdaptiveLabel')}` : `✋ ${tr('newCycle.modeFixedLabel')}`}
                    </span>
                    <p className="text-xs text-gray-500 mt-0.5 ml-5">
                      {m === 'adaptive'
                        ? tr('newCycle.modeAdaptiveDesc')
                        : tr('newCycle.modeFixedDesc')}
                    </p>
                  </label>
                ))}
              </div>
            </div>
          )}

          {/* Params */}
          {mode === 'adaptive' ? (
            <>
              <p className="text-xs text-gray-500 px-1">
                {tr('newCycle.adaptiveModeDesc')}
              </p>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <NumberField
                  id="fc-target"
                  label={tr('newCycle.labelTargetAmount')}
                  value={targetAmountG}
                  onChange={setTargetAmountG}
                  placeholder="4500"
                />
                <NumberField
                  id="fc-maxpulses"
                  label={tr('newCycle.labelMaxPulses')}
                  value={maxPulses}
                  onChange={setMaxPulses}
                  placeholder="6"
                />
                <NumberField
                  id="fc-maxdur"
                  label={tr('newCycle.labelMaxDuration')}
                  value={maxDurationMin}
                  onChange={setMaxDurationMin}
                  placeholder="10"
                />
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <NumberField
                  id="fc-pulsedur-adaptive"
                  label={tr('newCycle.labelPulseDurAdaptive')}
                  value={pulseDurationMs}
                  onChange={setPulseDurationMs}
                  placeholder="2500"
                />
                <NumberField
                  id="fc-gap-adaptive"
                  label={tr('newCycle.labelGapAdaptive')}
                  value={gapMs}
                  onChange={setGapMs}
                  placeholder="75000"
                />
              </div>
              {/* Phase B: ESP32 살포(DAC1) / 공급(DAC2) 모터 출력 */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <NumberField
                  id="fc-speedrpm-adaptive"
                  label={tr('newCycle.labelSpeedRpm')}
                  value={speedRpm}
                  onChange={setSpeedRpm}
                  placeholder="32"
                />
                <NumberField
                  id="fc-amount-adaptive"
                  label={tr('newCycle.labelAmount')}
                  value={amount}
                  onChange={setAmount}
                  placeholder="180"
                />
              </div>
            </>
          ) : (
            <>
              <p className="text-xs text-gray-500 px-1">
                {tr('newCycle.fixedModeDesc')}
              </p>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <NumberField
                  id="fc-pulsedur"
                  label={tr('newCycle.labelPulseDurFixed')}
                  value={pulseDurationMs}
                  onChange={setPulseDurationMs}
                  placeholder="2500"
                />
                <NumberField
                  id="fc-gap"
                  label={tr('newCycle.labelGapFixed')}
                  value={gapMs}
                  onChange={setGapMs}
                  placeholder="75000"
                />
                <NumberField
                  id="fc-totalpulses"
                  label={tr('newCycle.labelTotalPulses')}
                  value={totalPulses}
                  onChange={setTotalPulses}
                  placeholder="6"
                />
              </div>
              {/* Phase B: ESP32 살포(DAC1) / 공급(DAC2) 모터 출력 */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <NumberField
                  id="fc-speedrpm-fixed"
                  label={tr('newCycle.labelSpeedRpm')}
                  value={speedRpm}
                  onChange={setSpeedRpm}
                  placeholder="32"
                />
                <NumberField
                  id="fc-amount-fixed"
                  label={tr('newCycle.labelAmount')}
                  value={amount}
                  onChange={setAmount}
                  placeholder="180"
                />
              </div>
            </>
          )}

          {/* 운영자 의도 메모 (선택 입력) */}
          <div className="flex flex-col gap-1">
            <label htmlFor="fc-intent" className="text-xs text-gray-400 font-medium">
              {tr('newCycle.labelIntentReason')} <span className="text-gray-600">({tr('newCycle.optional')})</span>
            </label>
            <textarea
              id="fc-intent"
              rows={2}
              value={intentReason}
              onChange={e => setIntentReason(e.target.value)}
              placeholder={tr('newCycle.intentPlaceholder')}
              className="px-3 py-2 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500/50 resize-none"
            />
          </div>

          {/* C-5: 안전 게이트 3종 상태 — 사이클 시작 전 운영자 의사결정 보조 */}
          {tankId && (
            <div className="flex flex-col gap-1.5">
              <p className="text-xs text-gray-400 font-medium">{tr('newCycle.safetyGateStatus')}</p>
              <SafetyGateBadges tankId={tankId} />
            </div>
          )}

          <div className="flex gap-2 pt-1">
            <Button type="submit" size="sm" disabled={submitting || blocked != null} aria-label={tr('newCycle.submitAriaLabel')}>
              {submitting ? tr('newCycle.starting') : (fixedTankId ? tr('newCycle.startOnce') : tr('newCycle.start'))}
            </Button>
            {onCancel && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onCancel}
                aria-label={tr('common.cancel')}
              >
                {tr('common.cancel')}
              </Button>
            )}
          </div>
        </form>
      </CardContent>
    </Card>
    </>
  );
}

function NumberField({
  id,
  label,
  value,
  onChange,
  placeholder,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs text-gray-400 font-medium">
        {label}
      </label>
      <input
        id={id}
        type="number"
        min={0}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500/50"
      />
    </div>
  );
}

// ── Age helper ────────────────────────────────────────────────────────────────

function ageString(startedAt: string, tr: (k: string, v?: Record<string, string | number>) => string): string {
  const secs = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000);
  if (secs < 60) return tr('feedCycle.ageSecondsAgo', { n: secs });
  const mins = Math.floor(secs / 60);
  if (mins < 60) return tr('feedCycle.ageMinutesAgo', { n: mins });
  return tr('feedCycle.ageHoursAgo', { n: Math.floor(mins / 60) });
}

// ── AI 권장 일정 (자동 모드 read-only) ────────────────────────────────────────
// Phase 1d 부터는 backend AIScheduler 가 feeding_schedules 테이블에 24h 전체를
// 등록한다. 이 함수는 backend schedule 이 아직 없을 때 (초기 polling 전)의 fallback.

function computeAiSchedule(
  computation: FeedingPolicyComputation | null,
  lastCompletedAt: string | null,
): { times: string[]; nextEta: Date | null } {
  if (!computation || !computation.daily_cycles || computation.daily_cycles <= 0) {
    return { times: [], nextEta: null };
  }
  const cycles = computation.daily_cycles;
  const intervalH = 24 / cycles;
  // 첫 급이 06:00 기준.
  const times: string[] = [];
  for (let i = 0; i < cycles; i++) {
    const totalH = 6 + i * intervalH;
    const h = Math.floor(totalH) % 24;
    const m = Math.round((totalH - Math.floor(totalH)) * 60);
    times.push(`${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`);
  }
  // 다음 예정 = 마지막 cycle 종료 시각 + GET₅₀ (없으면 다음 시각).
  let nextEta: Date | null = null;
  if (lastCompletedAt && computation.get50_h != null) {
    const last = new Date(lastCompletedAt);
    nextEta = new Date(last.getTime() + computation.get50_h * 3_600_000);
  } else if (times.length > 0) {
    // 오늘 남은 시각 중 가장 가까운 것.
    const now = new Date();
    for (const t of times) {
      const [h, m] = t.split(':').map(Number);
      const cand = new Date(now);
      cand.setHours(h, m, 0, 0);
      if (cand.getTime() > now.getTime()) {
        nextEta = cand;
        break;
      }
    }
    if (!nextEta && times.length > 0) {
      // 모두 지난 경우 → 내일 첫 시각.
      const [h, m] = times[0].split(':').map(Number);
      const cand = new Date(now);
      cand.setDate(cand.getDate() + 1);
      cand.setHours(h, m, 0, 0);
      nextEta = cand;
    }
  }
  return { times, nextEta };
}

// ── AutoModePanel — 자동(AI 운영) read-only 화면 ───────────────────────────────

interface AutoModePanelProps {
  tank: Tank | null;
  computation: FeedingPolicyComputation | null;
  recentCycles: FeedCycle[];
  onPauseAi: () => void;
  onAdjustSubmit: (bsfMode: BsfMode | null, reason: string) => Promise<void>;
}

function AutoModePanel({
  tank,
  computation,
  recentCycles,
  onPauseAi,
  onAdjustSubmit,
}: AutoModePanelProps) {
  const { tr } = useLanguage();
  const [adjustOpen, setAdjustOpen] = useState(false);
  const [adjustReason, setAdjustReason] = useState('');
  const [adjustBsfMode, setAdjustBsfMode] = useState<BsfMode | ''>('');
  const [adjustSaving, setAdjustSaving] = useState(false);
  const [adjustError, setAdjustError] = useState<string | null>(null);
  // 최근 조정 내역 — 운영자 인계 + AI 자연어 학습 자료.
  const [adjustHistory, setAdjustHistory] = useState<OperatorIntent[]>([]);
  const [adjustHistoryRefreshKey, setAdjustHistoryRefreshKey] = useState(0);

  // Phase 1d: backend AI-managed schedule. polling 으로 5 분마다 재동기.
  const [aiSchedule, setAiSchedule] = useState<FeedingSchedule | null>(null);

  useEffect(() => {
    if (!tank) { setAdjustHistory([]); return; }
    let cancelled = false;
    OperatorIntents.list(tank.tank_id, 30)
      .then(r => {
        if (cancelled) return;
        const filtered = (r.items ?? []).filter(
          (i): i is OperatorIntent => i.intent_type === 'change_pattern',
        );
        setAdjustHistory(filtered);
      })
      .catch(() => { if (!cancelled) setAdjustHistory([]); });
    return () => { cancelled = true; };
  }, [tank?.tank_id, adjustHistoryRefreshKey]);

  // Backend AI-managed schedule fetch (Phase 1d). 60 초마다 polling.
  useEffect(() => {
    if (!tank) { setAiSchedule(null); return; }
    let cancelled = false;
    const tankId = tank.tank_id;
    const fetchSchedule = () => {
      Schedules.list()
        .then(r => {
          if (cancelled) return;
          const items = r.items ?? [];
          const match = items.find(
            s =>
              s.schedule_id.startsWith('ai_') &&
              s.enabled &&
              (s.tank_ids ?? []).includes(tankId),
          );
          setAiSchedule(match ?? null);
        })
        .catch(() => { if (!cancelled) setAiSchedule(null); });
    };
    fetchSchedule();
    const id = setInterval(fetchSchedule, 60_000);
    return () => { cancelled = true; clearInterval(id); };
  }, [tank?.tank_id]);

  const lastCompleted = recentCycles.find(c => isCycleCompleted(c)) ?? null;
  // Backend schedule 우선. 없으면 frontend 정책 계산 fallback (초기 polling 전 표시용).
  const fallback = computeAiSchedule(
    computation,
    lastCompleted?.completed_at ?? null,
  );
  const { times, nextEta, scheduleSource } = (() => {
    if (aiSchedule && aiSchedule.times && aiSchedule.times.length > 0) {
      const sorted = [...aiSchedule.times].sort();
      const now = new Date();
      let eta: Date | null = null;
      for (const t of sorted) {
        const [h, m] = t.split(':').map(Number);
        const cand = new Date(now);
        cand.setHours(h, m, 0, 0);
        if (cand.getTime() > now.getTime()) { eta = cand; break; }
      }
      if (!eta && sorted.length > 0) {
        const [h, m] = sorted[0].split(':').map(Number);
        const cand = new Date(now);
        cand.setDate(cand.getDate() + 1);
        cand.setHours(h, m, 0, 0);
        eta = cand;
      }
      return { times: sorted, nextEta: eta, scheduleSource: 'backend' as const };
    }
    return { times: fallback.times, nextEta: fallback.nextEta, scheduleSource: 'fallback' as const };
  })();

  // 오늘 진행된 cycle 수.
  const todayStart = new Date();
  todayStart.setHours(0, 0, 0, 0);
  const todayCycles = recentCycles.filter(
    c => new Date(c.started_at).getTime() >= todayStart.getTime(),
  );

  const lastReasonText = lastCompleted
    ? lastCompleted.termination_reason
      ? `${tr('autoMode.abnormalEnd')} (${lastCompleted.termination_reason})`
      : tr('autoMode.normalEnd')
    : tr('autoMode.none');

  async function handleSaveAdjust() {
    if (!adjustReason.trim()) {
      setAdjustError(tr('autoMode.adjustReasonRequired'));
      return;
    }
    setAdjustSaving(true);
    setAdjustError(null);
    try {
      await onAdjustSubmit(
        adjustBsfMode === '' ? null : adjustBsfMode,
        adjustReason.trim(),
      );
      setAdjustOpen(false);
      setAdjustReason('');
      setAdjustBsfMode('');
      setAdjustHistoryRefreshKey(k => k + 1);   // 조정 내역 즉시 갱신
    } catch (err) {
      setAdjustError(err instanceof Error ? err.message : tr('autoMode.unknownError'));
    } finally {
      setAdjustSaving(false);
    }
  }

  return (
    <Card className="bg-gray-900/60 border-green-700/30">
      <CardHeader className="pb-2 border-b border-gray-800">
        <CardTitle className="text-sm font-semibold text-green-300 flex items-center gap-2">
          🤖 {tr('autoMode.title')}
          <span className="text-xs text-gray-500 font-normal">(read-only)</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="pt-4 space-y-4">
        {!tank ? (
          <p className="text-sm text-gray-500 py-2">{tr('autoMode.selectTank')}</p>
        ) : !computation ? (
          <p className="text-sm text-gray-500 py-2">
            {tr('autoMode.policyUnavailable')}
          </p>
        ) : (
          <>
            {/* 다음 예정 + 오늘 진행 */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              <div className="px-3 py-2 bg-green-500/10 border border-green-500/30 rounded">
                <p className="text-xs text-green-400 uppercase tracking-wide mb-0.5">
                  {tr('autoMode.nextScheduledCycle')}
                </p>
                <p className="text-sm text-green-200 font-mono">
                  {nextEta
                    ? nextEta.toLocaleString('ko-KR', {
                        month: '2-digit',
                        day: '2-digit',
                        hour: '2-digit',
                        minute: '2-digit',
                      })
                    : '—'}
                </p>
                {computation.get50_h != null && lastCompleted && (
                  <p className="text-xs text-gray-500 mt-0.5">
                    {tr('autoMode.lastCycleGet50')} {computation.get50_h.toFixed(1)}h
                  </p>
                )}
              </div>
              <div className="px-3 py-2 bg-gray-800/50 border border-gray-700/40 rounded">
                <p className="text-xs text-gray-400 uppercase tracking-wide mb-0.5">
                  {tr('autoMode.todayProgress')}
                </p>
                <p className="text-sm text-gray-200 font-mono">
                  {todayCycles.length} / {computation.daily_cycles ?? '?'} cycle
                </p>
                <p className="text-xs text-gray-500 mt-0.5">
                  {tr('autoMode.last')}: {lastReasonText}
                </p>
              </div>
              <div className="px-3 py-2 bg-gray-800/50 border border-gray-700/40 rounded">
                <p className="text-xs text-gray-400 uppercase tracking-wide mb-0.5">
                  {tr('autoMode.aiSchedule24h')}
                </p>
                <p className="text-xs text-gray-200 font-mono leading-relaxed">
                  {times.length > 0 ? times.join(' · ') : '—'}
                </p>
                <p className="text-[10px] text-gray-500 mt-0.5">
                  {scheduleSource === 'backend'
                    ? tr('autoMode.scheduleBackendReady')
                    : tr('autoMode.scheduleFallback')}
                </p>
              </div>
            </div>

            {/* AI 판단 근거 */}
            <div className="px-3 py-3 bg-blue-500/10 border border-blue-500/30 rounded text-xs space-y-1">
              <p className="text-blue-300 font-semibold">{tr('autoMode.aiRationale')}</p>
              <p className="text-gray-300">
                {tr('feedCycle.rationalePolicy')} <span className="text-gray-100 font-medium">{tr(BSF_MODE_LABEL[computation.bsf_mode])}</span>
                {' '}· {tr('feedCycle.rationaleStage')} <span className="text-gray-100">{computation.stage_label}</span>
                {' '}· BSF <span className="font-mono">{tr('feedCycle.bsfPercentPerDay', { v: computation.bsf_percent?.toFixed(1) ?? '—' })}</span>
              </p>
              <p className="text-gray-400">
                {tr('autoMode.dailyFeedAmount')} <span className="font-mono text-gray-200">
                  {computation.daily_feed_g?.toLocaleString('ko-KR')}g
                </span>
                {' '}÷ {tr('feedCycle.cyclesPerDay', { n: computation.daily_cycles ?? '—' })} ={' '}
                <span className="font-mono text-gray-200">{computation.cycle_target_amount_g}g/cycle</span>
              </p>
              <p className="text-gray-400">
                {tr('autoMode.temperature')} <span className="font-mono text-gray-200">{computation.temperature_c}℃</span>
                {' '}({computation.temperature_source === 'sensor' ? tr('autoMode.tempMeasured') : 'fallback'})
                {' '}· GET₅₀ <span className="font-mono text-gray-200">{computation.get50_h?.toFixed(1)}h</span>
              </p>
              <p className="text-gray-500 italic">
                {tr('feedCycle.schedulerNote')}
              </p>
            </div>

            {/* AI 일시정지 + 조정 */}
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={onPauseAi}
                className="border-amber-500/40 text-amber-300 hover:bg-amber-500/10"
                aria-label={tr('autoMode.pauseAiAriaLabel')}
              >
                {tr('autoMode.pauseAiBtn')}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setAdjustOpen(v => !v)}
                aria-label={tr('autoMode.adjustAriaLabel')}
              >
                {adjustOpen ? tr('autoMode.adjustClose') : tr('autoMode.adjust')}
              </Button>
            </div>

            {/* 조정 inline 패널 */}
            {adjustOpen && (
              <div className="px-3 py-3 bg-gray-800/50 border border-gray-700/40 rounded space-y-3">
                <p className="text-xs text-gray-300 font-semibold">
                  {tr('autoMode.adjustPanelTitle')}
                </p>
                {adjustError && (
                  <p className="text-xs text-red-400 font-mono px-2 py-1 bg-red-500/10 border border-red-500/20 rounded">
                    {adjustError}
                  </p>
                )}
                <div className="flex flex-col gap-1">
                  <label className="text-xs text-gray-400 font-medium">
                    {tr('autoMode.adjustIntentLabel')}
                  </label>
                  <textarea
                    rows={2}
                    value={adjustReason}
                    onChange={e => setAdjustReason(e.target.value)}
                    placeholder={tr('autoMode.adjustIntentPlaceholder')}
                    className="px-3 py-2 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500/50 resize-none"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <p className="text-xs text-gray-400 font-medium">
                    {tr('autoMode.bsfPolicyChange')}
                  </p>
                  <div className="flex flex-wrap gap-2">
                    {([
                      ['', tr('autoMode.bsfNoChange')],
                      ['aggressive', tr(BSF_MODE_LABEL.aggressive)],
                      ['standard', tr(BSF_MODE_LABEL.standard)],
                      ['conservative', tr(BSF_MODE_LABEL.conservative)],
                    ] as [string, string][]).map(([val, lbl]) => (
                      <label
                        key={val || 'none'}
                        className={
                          adjustBsfMode === val
                            ? 'px-3 py-1.5 rounded border border-blue-500/60 bg-blue-600/10 cursor-pointer text-xs text-white'
                            : 'px-3 py-1.5 rounded border border-gray-700 hover:border-gray-600 cursor-pointer text-xs text-gray-300'
                        }
                      >
                        <input
                          type="radio"
                          name="fc-adjust-bsf"
                          value={val}
                          checked={adjustBsfMode === val}
                          onChange={() => setAdjustBsfMode(val as BsfMode | '')}
                          className="accent-blue-500 mr-1.5"
                        />
                        {lbl}
                      </label>
                    ))}
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    onClick={() => void handleSaveAdjust()}
                    disabled={adjustSaving}
                    aria-label={tr('autoMode.adjustSaveAriaLabel')}
                  >
                    {adjustSaving ? tr('autoMode.saving') : tr('autoMode.save')}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setAdjustOpen(false)}
                    aria-label={tr('autoMode.adjustCancelAriaLabel')}
                  >
                    {tr('common.cancel')}
                  </Button>
                </div>
              </div>
            )}

            {/* 최근 조정 내역 — 사용자 입력 reason 만 표시 */}
            {adjustHistory.length > 0 && (
              <div className="px-3 py-3 bg-gray-800/40 border border-gray-700/40 rounded space-y-2">
                <p className="text-xs text-gray-400 uppercase tracking-wide">
                  {tr('autoMode.recentAdjustHistory')} ({adjustHistory.length})
                </p>
                <ul className="space-y-1.5">
                  {adjustHistory.slice(0, 5).map(intent => (
                    <li
                      key={intent.intent_id}
                      className="text-xs leading-relaxed border-l-2 border-amber-500/40 pl-2 py-0.5"
                    >
                      <div className="flex items-baseline gap-2 flex-wrap">
                        <span className="text-amber-300 font-mono text-[10px]">
                          {new Date(intent.recorded_at).toLocaleString('ko-KR', {
                            month: '2-digit', day: '2-digit',
                            hour: '2-digit', minute: '2-digit',
                          })}
                        </span>
                        {intent.operator_id && (
                          <span className="text-gray-500 text-[10px] font-mono">
                            {intent.operator_id}
                          </span>
                        )}
                      </div>
                      <p className="text-gray-300 mt-0.5">{intent.reason}</p>
                    </li>
                  ))}
                </ul>
                {adjustHistory.length > 5 && (
                  <p className="text-xs text-gray-600 italic">
                    + {adjustHistory.length - 5}{tr('autoMode.moreItems')}
                  </p>
                )}
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

// ── ManualScheduleSection — 수동 모드 시각 등록 (ScheduleManager 흡수) ──────────

interface ManualScheduleSectionProps {
  tankId: string;
}

type ScheduleFormState = {
  time: string;
  pulseDurationMs: string;
  gapMs: string;
  totalPulses: string;
  targetAmountG: string;
  enabled: boolean;
};

function emptyScheduleForm(): ScheduleFormState {
  return {
    time: '06:00',
    pulseDurationMs: '5000',
    gapMs: '60000',
    totalPulses: '3',
    targetAmountG: '',
    enabled: true,
  };
}

function ManualScheduleSection({ tankId }: ManualScheduleSectionProps) {
  const { tr } = useLanguage();
  const [schedules, setSchedules] = useState<FeedingSchedule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [form, setForm] = useState<ScheduleFormState>(emptyScheduleForm());
  const [useCron, setUseCron] = useState(false);
  const [cron, setCron] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!tankId) {
      setSchedules([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const res = await Schedules.list();
      // 단일 수조 컨텍스트 — 해당 tank 가 포함된 schedule 만.
      setSchedules((res.items ?? []).filter(s => s.tank_ids.includes(tankId)));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [tankId]);

  useEffect(() => {
    void load();
  }, [load]);

  function startCreate() {
    setEditingId(null);
    setForm(emptyScheduleForm());
    setUseCron(false);
    setCron('');
    setFormOpen(true);
  }

  function startEdit(s: FeedingSchedule) {
    setEditingId(s.schedule_id);
    setForm({
      time: s.times[0] ?? '06:00',
      pulseDurationMs: String(s.pattern.pulse_duration_ms),
      gapMs: String(s.pattern.gap_ms),
      totalPulses: String(s.pattern.total_pulses),
      targetAmountG: s.pattern.target_amount_g != null ? String(s.pattern.target_amount_g) : '',
      enabled: s.enabled,
    });
    setUseCron(s.cron !== '');
    setCron(s.cron ?? '');
    setFormOpen(true);
  }

  function buildBody(): NewScheduleBody {
    const body: NewScheduleBody = {
      tank_ids: [tankId],
      pattern: {
        pulse_duration_ms: parseInt(form.pulseDurationMs, 10) || 0,
        gap_ms: parseInt(form.gapMs, 10) || 0,
        total_pulses: parseInt(form.totalPulses, 10) || 0,
        target_amount_g:
          form.targetAmountG !== '' ? parseFloat(form.targetAmountG) : undefined,
      },
      priority: 'manual_override',
      enabled: form.enabled,
    };
    if (useCron) {
      body.cron = cron;
    } else {
      body.times = [form.time];
    }
    return body;
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const body = buildBody();
      if (editingId) {
        await Schedules.update(editingId, body);
      } else {
        await Schedules.create(body);
      }
      setFormOpen(false);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  async function handleToggle(s: FeedingSchedule) {
    try {
      if (s.enabled) await Schedules.disable(s.schedule_id);
      else await Schedules.enable(s.schedule_id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function handleDelete(s: FeedingSchedule) {
    if (!confirm(`schedule ${s.schedule_id.slice(0, 12)}… ${tr('manualSched.deleteConfirm')}`)) return;
    try {
      await Schedules.delete(s.schedule_id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <Card className="bg-gray-900/60 border-amber-700/30">
      <CardHeader className="pb-2 border-b border-gray-800 flex flex-row items-center justify-between">
        <CardTitle className="text-sm font-semibold text-amber-300 flex items-center gap-2">
          ⏰ {tr('manualSched.title')}
          <span className="text-xs text-gray-500 font-normal">
            {tankId} · {tr('feedCycle.countCases', { n: schedules.length })}
          </span>
        </CardTitle>
        {!formOpen && tankId && (
          <Button size="sm" onClick={startCreate} aria-label={tr('manualSched.addTime')}>
            + {tr('manualSched.addTime')}
          </Button>
        )}
      </CardHeader>
      <CardContent className="pt-4 space-y-3">
        {error && (
          <p className="text-xs text-red-400 font-mono px-2 py-1 bg-red-500/10 border border-red-500/20 rounded">
            {error}
          </p>
        )}

        {/* 등록/편집 폼 */}
        {formOpen && (
          <form
            onSubmit={handleSubmit}
            className="space-y-3 p-3 bg-gray-800/40 border border-gray-700/40 rounded"
            aria-label={tr('manualSched.formAriaLabel')}
          >
            {/* 시간 방식 */}
            <div className="flex items-center gap-3 text-xs">
              <label className="flex items-center gap-1 text-gray-300 cursor-pointer">
                <input
                  type="radio"
                  checked={!useCron}
                  onChange={() => setUseCron(false)}
                  className="accent-blue-500"
                />
                {tr('manualSched.timeHHMM')}
              </label>
              <label className="flex items-center gap-1 text-gray-300 cursor-pointer">
                <input
                  type="radio"
                  checked={useCron}
                  onChange={() => setUseCron(true)}
                  className="accent-blue-500"
                />
                {tr('manualSched.advancedCron')}
              </label>
            </div>

            {!useCron ? (
              <div className="flex flex-col gap-1">
                <label className="text-xs text-gray-400 font-medium">{tr('manualSched.feedTimeLabel')}</label>
                <input
                  type="time"
                  value={form.time}
                  onChange={e => setForm(f => ({ ...f, time: e.target.value }))}
                  className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white w-32 focus:outline-none focus:ring-2 focus:ring-blue-500/50"
                />
              </div>
            ) : (
              <div className="flex flex-col gap-1">
                <label className="text-xs text-gray-400 font-medium">
                  {tr('manualSched.cronExprLabel')} (<code className="text-gray-300">0 6,12,18 * * *</code>)
                </label>
                <input
                  type="text"
                  value={cron}
                  onChange={e => setCron(e.target.value)}
                  placeholder="0 6,12,18 * * *"
                  className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50"
                />
              </div>
            )}

            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <NumberField
                id="fc-sched-pulsedur"
                label={tr('manualSched.labelPulseMs')}
                value={form.pulseDurationMs}
                onChange={v => setForm(f => ({ ...f, pulseDurationMs: v }))}
              />
              <NumberField
                id="fc-sched-gap"
                label={tr('manualSched.labelGapMs')}
                value={form.gapMs}
                onChange={v => setForm(f => ({ ...f, gapMs: v }))}
              />
              <NumberField
                id="fc-sched-total"
                label={tr('manualSched.labelCount')}
                value={form.totalPulses}
                onChange={v => setForm(f => ({ ...f, totalPulses: v }))}
              />
              <NumberField
                id="fc-sched-target"
                label={tr('manualSched.labelTargetGOptional')}
                value={form.targetAmountG}
                onChange={v => setForm(f => ({ ...f, targetAmountG: v }))}
              />
            </div>

            <label className="flex items-center gap-2 text-xs text-gray-300 cursor-pointer">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={e => setForm(f => ({ ...f, enabled: e.target.checked }))}
                className="accent-blue-500"
              />
              {tr('manualSched.enabled')}
            </label>

            <div className="flex gap-2 pt-1">
              <Button type="submit" size="sm" disabled={saving} aria-label={tr('manualSched.saveBtnAriaLabel')}>
                {saving ? tr('manualSched.saving') : editingId ? tr('manualSched.edit') : tr('manualSched.add')}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setFormOpen(false)}
                aria-label={tr('manualSched.cancelAriaLabel')}
              >
                {tr('common.cancel')}
              </Button>
            </div>
          </form>
        )}

        {/* 시각 list */}
        {loading ? (
          <Skeleton className="h-12 w-full" />
        ) : schedules.length === 0 ? (
          <p className="text-xs text-gray-500 text-center py-3">
            {tr('manualSched.noSchedules')}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs text-gray-300">
              <thead>
                <tr className="border-b border-gray-700 text-gray-500">
                  <th className="text-left py-1.5 pr-3">{tr('manualSched.colTime')}</th>
                  <th className="text-left py-1.5 pr-3">{tr('manualSched.colFeed')}</th>
                  <th className="text-left py-1.5 pr-3">{tr('manualSched.colStop')}</th>
                  <th className="text-left py-1.5 pr-3">{tr('manualSched.colCount')}</th>
                  <th className="text-left py-1.5 pr-3">target</th>
                  <th className="text-center py-1.5 pr-3">{tr('manualSched.colEnabled')}</th>
                  <th className="text-left py-1.5">{tr('manualSched.colActions')}</th>
                </tr>
              </thead>
              <tbody>
                {schedules.map(s => (
                  <tr key={s.schedule_id} className="border-b border-gray-800 hover:bg-gray-800/30">
                    <td className="py-2 pr-3 font-mono whitespace-nowrap">
                      {s.cron ? `cron: ${s.cron}` : s.times.join(', ') || '—'}
                    </td>
                    <td className="py-2 pr-3 font-mono">{s.pattern.pulse_duration_ms}ms</td>
                    <td className="py-2 pr-3 font-mono">{s.pattern.gap_ms}ms</td>
                    <td className="py-2 pr-3 font-mono">{s.pattern.total_pulses}</td>
                    <td className="py-2 pr-3 font-mono">
                      {s.pattern.target_amount_g != null ? `${s.pattern.target_amount_g}g` : '—'}
                    </td>
                    <td className="py-2 pr-3 text-center">
                      <button
                        onClick={() => void handleToggle(s)}
                        className={[
                          'w-8 h-4 rounded-full transition-colors relative inline-block',
                          s.enabled ? 'bg-green-600' : 'bg-gray-600',
                        ].join(' ')}
                        title={s.enabled ? tr('manualSched.disable') : tr('manualSched.enable')}
                        aria-label={s.enabled ? tr('manualSched.disable') : tr('manualSched.enable')}
                      >
                        <span
                          className={[
                            'absolute top-0.5 w-3 h-3 bg-white rounded-full transition-transform',
                            s.enabled ? 'translate-x-4' : 'translate-x-0.5',
                          ].join(' ')}
                        />
                      </button>
                    </td>
                    <td className="py-2">
                      <div className="flex gap-2">
                        <button
                          onClick={() => startEdit(s)}
                          className="text-gray-400 hover:text-blue-300 text-xs"
                          aria-label={tr('manualSched.editBtn')}
                        >
                          {tr('manualSched.editBtn')}
                        </button>
                        <button
                          onClick={() => void handleDelete(s)}
                          className="text-gray-400 hover:text-red-400 text-xs"
                          aria-label={tr('manualSched.deleteBtn')}
                        >
                          {tr('manualSched.deleteBtn')}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── AutoQuickStart — 자동 모드 테스트 1회 공급 ────────────────────────────────

interface AutoQuickStartProps {
  tank: Tank | null;
  computation: FeedingPolicyComputation | null;
  disabledReason: string | null;
  onStarted: () => void;
}

function AutoQuickStart({ tank, computation, disabledReason, onStarted }: AutoQuickStartProps) {
  const { tr } = useLanguage();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // F.2 — LLM 분석 modal
  const [llmModal, setLlmModal] = useState<{ intentId: string; analysis: LLMAnalysis | null } | null>(null);

  async function handleStart() {
    if (!tank || !computation) {
      setError(tr('quickStart.errorPolicyUnavailable'));
      return;
    }
    if (computation.cycle_target_amount_g == null) {
      setError(tr('quickStart.errorTargetNotSet'));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      setLlmModal({ intentId: '', analysis: null });
      const intentRes = await OperatorIntents.create({
        tank_id: tank.tank_id,
        intent_type: 'feed_now',
        reason: tr('feedCycle.testOnceReason'),
      });
      // F.2 — LLM 분석이 있으면 modal 표시
      setLlmModal(intentRes.llm_analysis ? { intentId: intentRes.intent_id, analysis: intentRes.llm_analysis } : null);
      const pattern = computation.feeding_pattern_default;
      await FeedCycles.start({
        tank_id: tank.tank_id,
        mode: 'adaptive',
        intent_id: intentRes.intent_id,
        params: {
          target_amount_g: computation.cycle_target_amount_g,
          max_pulses: pattern?.total_pulses,
          max_duration_min: computation.max_duration_min ?? undefined,
          pulse_duration_ms: pattern?.pulse_duration_ms,
          gap_ms: pattern?.gap_ms,
          ...(pattern?.speed_rpm ? { speed_rpm: pattern.speed_rpm } : {}),
          ...(pattern?.amount ? { amount: pattern.amount } : {}),
        },
      });
      onStarted();
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'SAFETY_GATE_BLOCKED') {
          const reason = (err.details?.rejection_reason as string | undefined) ?? '';
          setError(`${tr('newCycle.errorSafetyGate')} ${safetyGateMessage(reason)}`);
        } else if (err.code === 'CYCLE_CONFLICT') {
          setError(tr('quickStart.errorCycleConflict'));
        } else {
          setError(`[${err.code}] ${err.message}`);
        }
      } else {
        setError(err instanceof Error ? err.message : tr('newCycle.errorUnknown'));
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      {llmModal && (
        <LLMAnalysisModal
          intentId={llmModal.intentId}
          analysis={llmModal.analysis}
          onApply={async (force: boolean) => {
            await OperatorIntents.apply(llmModal.intentId, { force });
          }}
          onClose={() => setLlmModal(null)}
        />
      )}
    <Card className="bg-gray-800/60 border-blue-500/20">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm text-blue-400">⚡ {tr('quickStart.title')}</CardTitle>
      </CardHeader>
      <CardContent className="pt-3 space-y-3">
        <p className="text-xs text-gray-400 leading-relaxed">
          {tr('quickStart.desc')}
          {computation && computation.cycle_target_amount_g != null && (
            <>
              {' '}({tr('feedCycle.targetWithPulses', {
                g: computation.cycle_target_amount_g,
                pulses: computation.feeding_pattern_default?.total_pulses ?? '?',
              })})
            </>
          )}
        </p>
        {error && (
          <p className="text-xs text-red-400 font-mono px-2 py-1 bg-red-500/10 border border-red-500/20 rounded">
            {error}
          </p>
        )}
        {disabledReason && (
          <p className="text-xs text-amber-300 px-2 py-1 bg-amber-500/10 border border-amber-500/30 rounded">
            ⚠ {disabledReason}
          </p>
        )}
        <Button
          size="sm"
          onClick={() => void handleStart()}
          disabled={submitting || disabledReason != null || !tank || !computation}
          aria-label={tr('quickStart.startBtnAriaLabel')}
        >
          {submitting ? tr('newCycle.starting') : tr('newCycle.startOnce')}
        </Button>
      </CardContent>
    </Card>
    </>
  );
}

// ── Main FeedCycleMonitor ─────────────────────────────────────────────────────

export function FeedCycleMonitor() {
  const { tr } = useLanguage();
  const [tanks, setTanks] = useState<Tank[]>([]);
  const [selectedTankId, setSelectedTankId] = useState<string>('');
  const [activeCycles, setActiveCycles] = useState<FeedCycle[]>([]);
  const [recentCycles, setRecentCycles] = useState<FeedCycle[]>([]);
  const [selectedCycle, setSelectedCycle] = useState<FeedCycle | null>(null);
  const [loadingTanks, setLoadingTanks] = useState(true);
  const [loadingCycles, setLoadingCycles] = useState(true);
  const [loadingRecent, setLoadingRecent] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [stopping, setStopping] = useState(false);
  // C-3l: 이의 제기 모달 + 제기 완료 사이클 추적 (세션 한정 배지용).
  const [disputeTarget, setDisputeTarget] = useState<FeedCycle | null>(null);
  const [disputedCycleIds, setDisputedCycleIds] = useState<Set<string>>(new Set());
  // 모드 토글 — getEffectiveTankPolicy 와 동기.
  const [operatingMode, setOperatingMode] = useState<OperatingMode>('auto');
  // 정책 변경 이벤트로 mode 재로드.
  const [policyVersion, setPolicyVersion] = useState(0);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // F.2 — LLM 분석 modal (handleAdjustSubmit 에서 사용)
  const [llmModal, setLlmModal] = useState<{ intentId: string; analysis: LLMAnalysis | null } | null>(null);

  // Load tanks once on mount
  useEffect(() => {
    setLoadingTanks(true);
    Tanks.list()
      .then(res => {
        const multiTankEnabled = res.items.filter(t => t.site_id);
        setTanks(multiTankEnabled);
        if (multiTankEnabled.length > 0) {
          setSelectedTankId(multiTankEnabled[0].tank_id);
        }
      })
      .catch((err: unknown) => {
        // Non-fatal: show all tanks or empty
        const msg = err instanceof Error ? err.message : '알 수 없는 오류';
        console.warn('[FeedCycleMonitor] tanks list failed:', msg);
        setTanks([]);
      })
      .finally(() => setLoadingTanks(false));
  }, []);

  // 정책 변경 이벤트 구독 — operating_mode 동기.
  useEffect(() => {
    const onChange = () => setPolicyVersion(v => v + 1);
    window.addEventListener('bluei:feeding-policy-changed', onChange);
    return () => window.removeEventListener('bluei:feeding-policy-changed', onChange);
  }, []);

  // 선택 수조의 operating_mode 동기.
  const selectedTank = tanks.find(t => t.tank_id === selectedTankId) ?? null;
  useEffect(() => {
    if (!selectedTank) return;
    const eff = getEffectiveTankPolicy(selectedTank.tank_id, selectedTank.group_id ?? '');
    setOperatingMode(eff.operating_mode);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedTankId, policyVersion, selectedTank?.group_id]);

  // 선택 수조의 정책 계산 (자동 모드 안내용).
  const computation = selectedTank ? (() => {
    const eff = getEffectiveTankPolicy(selectedTank.tank_id, selectedTank.group_id ?? '');
    return computeFeedingPolicy({
      species: selectedTank.species,
      fish_count: selectedTank.fish_count ?? null,
      avg_weight_g: selectedTank.avg_weight_g ?? null,
      volume_m3: selectedTank.volume_m3 ?? null,
      bsf_mode: eff.bsf_mode,
      temperature_c: null,
      max_daily_cycles: eff.max_daily_cycles,
    });
  })() : null;

  // Fetch active cycles
  const fetchActive = useCallback(async () => {
    setError(null);
    try {
      const res = await FeedCycles.listActive();
      setActiveCycles(res.items);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('newCycle.errorUnknown');
      setError(msg);
    } finally {
      setLoadingCycles(false);
    }
  }, []);

  // 수조 변경 시 사이클 상세 reset — 다른 수조의 cycle 이 잔류하지 않도록.
  useEffect(() => {
    setSelectedCycle(null);
  }, [selectedTankId]);

  // Fetch recent cycles for selected tank (완료/미완료 모두 — 자동 모드의 "마지막 cycle" 추적용)
  const fetchRecent = useCallback(async (tankId: string) => {
    if (!tankId) return;
    setLoadingRecent(true);
    try {
      const res = await FeedCycles.listForTank(tankId, 20);
      setRecentCycles(res.items);
    } catch {
      // non-fatal
      setRecentCycles([]);
    } finally {
      setLoadingRecent(false);
    }
  }, []);

  // active/recent 갱신 시 선택된 cycle 의 최신 상태로 동기화 (현 수조에 한정).
  // selectedCycle 이 active 에서 빠졌는데 recent 에도 없으면 종료된 직후라 recent 가 stale —
  // fetchRecent 를 강제 호출해 다음 tick 에 종료 상태로 갱신되도록 한다 (status badge stale 방지).
  useEffect(() => {
    let needsRecentRefresh = false;
    setSelectedCycle(prev => {
      if (!prev) return prev;
      if (prev.tank_id !== selectedTankId) return null;     // 다른 수조면 강제 reset
      const fromActive = activeCycles.find(c => c.cycle_id === prev.cycle_id);
      if (fromActive) return fromActive;
      const fromRecent = recentCycles.find(c => c.cycle_id === prev.cycle_id);
      if (!fromRecent) needsRecentRefresh = true;
      return fromRecent ?? prev;
    });
    if (needsRecentRefresh && selectedTankId) {
      void fetchRecent(selectedTankId);
    }
  }, [activeCycles, recentCycles, selectedTankId, fetchRecent]);

  // Auto-refresh active cycles every 3 seconds while mounted
  useEffect(() => {
    setLoadingCycles(true);
    void fetchActive();
    intervalRef.current = setInterval(() => { void fetchActive(); }, 3000);
    return () => {
      if (intervalRef.current) clearInterval(intervalRef.current);
    };
  }, [fetchActive]);

  // Reload recent when tank selection changes
  useEffect(() => {
    if (selectedTankId) void fetchRecent(selectedTankId);
  }, [selectedTankId, fetchRecent]);

  async function handleStop(cycleId: string) {
    setStopping(true);
    try {
      await FeedCycles.stop(cycleId);
      await fetchActive();
      if (selectedTankId) await fetchRecent(selectedTankId);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('newCycle.errorUnknown');
      setError(msg);
    } finally {
      setStopping(false);
    }
  }

  function handleCycleStarted() {
    void fetchActive();
    if (selectedTankId) void fetchRecent(selectedTankId);
  }

  function handleModeToggle(next: OperatingMode) {
    if (!selectedTank) return;
    setTankOverride(selectedTank.tank_id, { operating_mode: next });
    setOperatingMode(next);
    // bluei:feeding-policy-changed 이벤트는 setTankOverride 내부에서 dispatch.
  }

  async function handleAdjustSubmit(bsfMode: BsfMode | null, reason: string) {
    if (!selectedTank) throw new Error(tr('monitor.errorNoTankSelected'));
    setLlmModal({ intentId: '', analysis: null });
    const intentRes = await OperatorIntents.create({
      tank_id: selectedTank.tank_id,
      intent_type: 'change_pattern',
      reason,
    });
    // F.2 — LLM 분석이 있으면 modal 표시
    setLlmModal(intentRes.llm_analysis ? { intentId: intentRes.intent_id, analysis: intentRes.llm_analysis } : null);
    if (bsfMode != null) {
      setTankOverride(selectedTank.tank_id, { bsf_mode: bsfMode });
    }
  }

  const multiTankEnabledTanks = tanks.length > 0 ? tanks : [];

  // 활성 사이클이 있는 경우 quick-start 비활성화 사유.
  const activeForThisTank = activeCycles.find(c => c.tank_id === selectedTankId) ?? null;
  const quickStartBlock = activeForThisTank
    ? tr('feedCycle.quickStartBlocked', { id: activeForThisTank.cycle_id.slice(-12) })
    : computation?.block_reason ?? null;

  // 완료된 사이클만 "최근 완료 사이클" 표시.
  const completedCycles = recentCycles.filter(c => isCycleCompleted(c));

  return (
    <div className="space-y-4">
      {/* F.2 — LLM 분석 modal (handleAdjustSubmit 에서 열림) */}
      {llmModal && (
        <LLMAnalysisModal
          intentId={llmModal.intentId}
          analysis={llmModal.analysis}
          onApply={async (force: boolean) => {
            await OperatorIntents.apply(llmModal.intentId, { force });
            void fetchActive();
            if (selectedTankId) void fetchRecent(selectedTankId);
          }}
          onClose={() => setLlmModal(null)}
        />
      )}
      {/* ── Header: 모드 토글 + 수조 선택 ────────────────────────────────────── */}
      <div className="flex flex-wrap items-center gap-3 px-3 py-2 bg-gray-900/40 border border-gray-700/40 rounded">
        <span className="text-sm font-semibold text-gray-200">{tr('monitor.feedingTitle')}</span>
        <div className="flex gap-1.5">
          {(['auto', 'manual'] as OperatingMode[]).map(m => (
            <button
              key={m}
              type="button"
              onClick={() => handleModeToggle(m)}
              className={[
                'px-3 py-1.5 rounded text-xs border transition-colors',
                operatingMode === m
                  ? m === 'auto'
                    ? 'bg-green-600/20 border-green-500/60 text-green-200'
                    : 'bg-amber-600/20 border-amber-500/60 text-amber-200'
                  : 'bg-gray-800/50 border-gray-700 text-gray-400 hover:border-gray-600',
              ].join(' ')}
              aria-pressed={operatingMode === m}
              aria-label={m === 'auto' ? tr('monitor.modeAutoLabel') : tr('monitor.modeManualLabel')}
            >
              {operatingMode === m ? '●' : '○'}{' '}
              {m === 'auto' ? tr('monitor.modeAutoLabel') : tr('monitor.modeManualLabel')}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2 ml-auto">
          <label htmlFor="fc-tank-filter" className="text-xs text-gray-400 font-medium">
            {tr('monitor.selectTank')}
          </label>
          {loadingTanks ? (
            <Skeleton className="h-8 w-40" />
          ) : (
            <select
              id="fc-tank-filter"
              value={selectedTankId}
              onChange={e => setSelectedTankId(e.target.value)}
              className="h-8 px-3 rounded-md border border-gray-700 bg-gray-900 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500/50"
            >
              {multiTankEnabledTanks.length === 0 ? (
                <option value="">{tr('monitor.noTanks')}</option>
              ) : (
                multiTankEnabledTanks.map(t => (
                  <option key={t.tank_id} value={t.tank_id}>
                    {t.lifecycle_stage && t.lot_no
                      ? `${t.tank_id} — ${t.lifecycle_stage} · Lot ${t.lot_no}`
                      : t.lifecycle_stage
                      ? `${t.tank_id} — ${t.lifecycle_stage}`
                      : t.lot_no
                      ? `${t.tank_id} · Lot ${t.lot_no}`
                      : t.display_name || t.tank_id}
                  </option>
                ))
              )}
            </select>
          )}
        </div>
      </div>

      {/* ── Error banner ────────────────────────────────────────────────────── */}
      {error && (
        <div className="px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive font-mono">
          {tr('monitor.errorPrefix')}: {error}
        </div>
      )}

      {/* ── Section 1: 모드별 화면 ──────────────────────────────────────────── */}
      {operatingMode === 'auto' ? (
        <AutoModePanel
          tank={selectedTank}
          computation={computation}
          recentCycles={recentCycles}
          onPauseAi={() => handleModeToggle('manual')}
          onAdjustSubmit={handleAdjustSubmit}
        />
      ) : (
        selectedTankId && <ManualScheduleSection tankId={selectedTankId} />
      )}

      {/* ── Section 2: 활성 사이클 (좌) + 사이클 상세 (우) — 양쪽 공통 ──────── */}
      {(() => {
        // 선택된 수조의 활성 cycle 만 필터링 (다른 수조 cycle 은 안 보이게).
        const activeForTank = selectedTankId
          ? activeCycles.filter(c => c.tank_id === selectedTankId)
          : activeCycles;
        return (
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Card className="bg-gray-900/60 border-gray-700/50">
          <CardHeader className="pb-0 border-b border-gray-800">
            <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide py-1">
              ▶ {tr('monitor.activeCycles')}
              {!loadingCycles && (
                <span className="ml-2 text-gray-500 font-normal">({activeForTank.length})</span>
              )}
            </CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            {loadingCycles ? (
              <div className="p-5 space-y-3">
                {[1, 2].map(i => <Skeleton key={i} className="h-12 w-full" />)}
                <p className="text-xs text-gray-500 text-center">{tr('monitor.loadingActiveCycles')}</p>
              </div>
            ) : activeForTank.length === 0 ? (
              <p className="py-10 text-center text-sm text-gray-500">
                {tr('monitor.noActiveCycles')}
              </p>
            ) : (
              <ul aria-label={tr('monitor.activeCyclesListAriaLabel')}>
                {activeForTank.map(cycle => (
                  <li
                    key={cycle.cycle_id}
                    role="button"
                    tabIndex={0}
                    onClick={() => setSelectedCycle(cycle)}
                    onKeyDown={e => e.key === 'Enter' && setSelectedCycle(cycle)}
                    className={[
                      'flex items-center gap-3 px-4 py-3 border-b border-gray-800/50 cursor-pointer transition-colors',
                      selectedCycle?.cycle_id === cycle.cycle_id
                        ? 'bg-blue-500/10'
                        : 'hover:bg-gray-800/30',
                    ].join(' ')}
                  >
                    <div className="flex-1 min-w-0">
                      <p className="text-xs font-mono text-gray-300 truncate">
                        {cycle.cycle_id.slice(-12)}
                      </p>
                      <p className="text-xs text-gray-500 truncate">{cycle.tank_id}</p>
                    </div>
                    <CycleStatusBadge
                      status={cycle.status}
                      terminationReason={cycle.termination_reason}
                    />
                    <div className="text-right text-xs text-gray-500 shrink-0">
                      <p>
                        {cycle.pulses_executed}/{cycle.max_pulses ?? '?'} {tr('monitor.pulseUnit')}
                      </p>
                      <p>
                        {cycle.total_amount_g}
                        {cycle.target_amount_g != null ? `/${cycle.target_amount_g}` : ''}g
                      </p>
                      <p>{ageString(cycle.started_at, tr)}</p>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        {/* Selected cycle detail */}
        <Card className="bg-gray-900/60 border-gray-700/50">
          <CardHeader className="pb-0 border-b border-gray-800">
            <div className="flex items-center justify-between py-1">
              <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
                {tr('monitor.cycleDetail')}
              </CardTitle>
              {selectedCycle && (
                <button
                  type="button"
                  onClick={() => setSelectedCycle(null)}
                  className="text-gray-500 hover:text-gray-200 text-xl leading-none px-2 -my-1"
                  aria-label={tr('monitor.closeCycleDetail')}
                  title={tr('monitor.closeCycleDetail')}
                >
                  ×
                </button>
              )}
            </div>
          </CardHeader>
          <CardContent className="pt-4">
            {selectedCycle ? (
              <CycleDetail
                cycle={selectedCycle}
                onStop={handleStop}
                stopping={stopping}
                tank={tanks.find(t => t.tank_id === selectedCycle.tank_id) ?? null}
                intentReason={selectedCycle.intent_reason ?? null}
                onDispute={c => setDisputeTarget(c)}
                disputed={disputedCycleIds.has(selectedCycle.cycle_id)}
              />
            ) : (
              <p className="text-sm text-gray-500 py-6 text-center">
                {tr('monitor.selectCycleHint')}
              </p>
            )}
          </CardContent>
        </Card>
      </div>
        );
      })()}

      {/* ── Section 3: 테스트 1회 공급 — 모드별 다름 ────────────────────────── */}
      {operatingMode === 'auto' ? (
        <AutoQuickStart
          tank={selectedTank}
          computation={computation}
          disabledReason={quickStartBlock}
          onStarted={handleCycleStarted}
        />
      ) : (
        selectedTankId && (
          <NewCycleForm
            tanks={multiTankEnabledTanks}
            fixedTankId={selectedTankId}
            defaultMode="fixed"
            onSuccess={handleCycleStarted}
          />
        )
      )}

      {/* ── Section 4: 최근 완료 사이클 — 양쪽 공통 ──────────────────────────── */}
      <Card className="bg-gray-900/60 border-gray-700/50 overflow-hidden">
        <CardHeader className="pb-0 border-b border-gray-800">
          <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide py-1">
            📋 {tr('monitor.recentCompletedCycles')}
            {!loadingRecent && (
              <span className="ml-2 text-gray-500 font-normal">({completedCycles.length})</span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loadingRecent ? (
            <div className="p-4 space-y-2">
              {[1, 2].map(i => <Skeleton key={i} className="h-8 w-full" />)}
            </div>
          ) : completedCycles.length === 0 ? (
            <p className="py-8 text-center text-sm text-gray-500">
              {tr('monitor.noCompletedCycles')}
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" aria-label={tr('monitor.recentCyclesTableAriaLabel')}>
                <thead>
                  <tr className="border-b border-gray-800 text-xs text-gray-500 uppercase tracking-wide">
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colCycleId')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colMode')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colFeedAmount')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colPulses')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colStatus')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colTerminationReason')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('monitor.colCompletedAt')}</th>
                  </tr>
                </thead>
                <tbody>
                  {completedCycles.map(cycle => (
                    <tr
                      key={cycle.cycle_id}
                      className="border-b border-gray-800/50 hover:bg-gray-800/30 transition-colors"
                    >
                      <td className="px-4 py-3 font-mono text-xs text-gray-300">
                        {cycle.cycle_id.slice(-12)}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-400">
                        {cycle.mode === 'adaptive' ? tr('monitor.modeAdaptive') : tr('monitor.modeFixed')}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-300">
                        {cycle.total_amount_g}
                        {cycle.target_amount_g != null
                          ? ` / ${cycle.target_amount_g}`
                          : ''}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-gray-400">
                        {cycle.pulses_executed}
                        {cycle.max_pulses != null ? ` / ${cycle.max_pulses}` : ''}
                      </td>
                      <td className="px-4 py-3">
                        <CycleStatusBadge
                          status={cycle.status}
                          terminationReason={cycle.termination_reason}
                        />
                      </td>
                      <td className="px-4 py-3 text-xs">
                        {cycle.termination_reason ? (
                          <span
                            className={
                              cycle.intent_reason
                                ? 'font-mono text-gray-300 border-b border-dotted border-amber-500/60 cursor-help'
                                : 'font-mono text-gray-400'
                            }
                            title={cycle.intent_reason ?? undefined}
                          >
                            {cycle.termination_reason}
                            {cycle.intent_reason && (
                              <span className="text-amber-400 ml-1">💬</span>
                            )}
                          </span>
                        ) : (
                          <span className="text-gray-500">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-500">
                        {cycle.completed_at
                          ? new Date(cycle.completed_at).toLocaleString('ko-KR')
                          : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* C-3l: 이의 제기 모달 — decision_id 가 있는 사이클에 한해 사용 */}
      <DisputeModal
        open={disputeTarget !== null}
        decisionId={disputeTarget?.decision_id ?? ''}
        tankId={disputeTarget?.tank_id ?? ''}
        onClose={() => setDisputeTarget(null)}
        onSubmitted={() => {
          if (disputeTarget) {
            setDisputedCycleIds(prev => {
              const next = new Set(prev);
              next.add(disputeTarget.cycle_id);
              return next;
            });
          }
        }}
      />

    </div>
  );
}
