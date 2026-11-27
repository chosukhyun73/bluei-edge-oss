import { useCallback, useEffect, useMemo, useState } from 'react';
import { dateLocale } from '../../lib/format';
import { Vision } from '../../lib/api';
import { Button } from '../ui/button';
import type { VisionObservation, VisionDisputeVerdict, VisionSegmentsClip } from '../../lib/types';
import { useLanguage } from '../../lib/language-core';

// 펄스 세그먼트 학습 알고리즘 (docs/50, DisputeTab 과 동일).
const SEGMENT_ALGORITHM_ID = 'vision_lrcn_blueocean_reaction_v1';

// 펄스별 라벨 상태 — pulse_no 별 점수/출처/배출여부.
// discharged=false(무배출) → 활동성 학습에서 제외(격리)되고, 사이클 전체 배출 집계에 반영.
type PulseLabel = { score: number; source: 'prior' | 'operator'; discharged: boolean };

// G-4 hook (useCycleCompletionDispute) 은 별도 파일 (useCycleCompletionDispute.ts) 분리 —
// vite React Fast Refresh 가 컴포넌트+비컴포넌트 export 동거를 invalidate 하기 때문.

interface CycleCompletionDisputeProps {
  open: boolean;
  observation: VisionObservation | null;
  cycleId: string;
  tankId: string;
  startedAt?: string; // 사이클 시작 시각 — 사람용 표기
  ordinal?: number | null; // 오늘 몇 번째 급이 — 사람용 표기 (null = 미상)
  pulsesExecuted?: number | null; // 이번 급이의 펄스(사이클) 발수
  suspectedNoDischarge?: number | null; // 시스템 무배출 의심 발수
  pulseVerdicts?: string[]; // 펄스별 판정 (yes|unclear|no|unknown) — 어느 발이 의심인지
  onClose: () => void;
  onSubmitted?: () => void;
  onSkip?: () => void;
  operatorId?: string; // 기본 'operator_default'
}

const KEY_TO_SCORE: Record<string, number> = {
  '1': 0.1,
  '2': 0.3,
  '3': 0.5,
  '4': 0.7,
  '5': 0.9,
};

function verdictFromGap(operatorScore: number, lrcnScore: number): VisionDisputeVerdict {
  const gap = Math.abs(operatorScore - lrcnScore);
  if (gap < 0.15) return 'correct';
  if (gap > 0.35) return 'wrong';
  return 'unsure';
}

function lrcnScoreFrom(observation: VisionObservation | null): number {
  if (!observation) return 0.5;
  const fromConfidence = typeof observation['confidence'] === 'number' ? (observation['confidence'] as number) : NaN;
  if (Number.isFinite(fromConfidence)) return fromConfidence;
  const scores = (observation['scores'] as Record<string, number> | undefined) ?? {};
  if (typeof scores['feeding_activity'] === 'number') return scores['feeding_activity'];
  return 0.5;
}

export function CycleCompletionDispute({
  open,
  observation,
  cycleId,
  tankId,
  startedAt,
  ordinal = null,
  pulsesExecuted = null,
  suspectedNoDischarge = null,
  pulseVerdicts = [],
  onClose,
  onSubmitted,
  onSkip,
  operatorId = 'operator_default',
}: CycleCompletionDisputeProps) {
  const { tr } = useLanguage();
  const bootstrapScore = useMemo(() => lrcnScoreFrom(observation), [observation]);
  const [operatorScore, setOperatorScore] = useState<number>(0.5);
  // 운영자가 점수를 실제로 조작했는가 (슬라이더/단축키). AI 프리필을 그대로 수락한
  // 제출은 독립 라벨이 아니므로(모델이 자기 출력을 재학습 → 편향 자기강화) 학습·보정에서
  // 걸러내야 한다. 값 비교(operator_score==ai_score)로는 우연 일치와 구분이 안 돼 별도 기록.
  // 2026-07-23: 프리필 수락률 급증(오늘 8건 중 4건) 관측이 계기.
  const [scoreTouched, setScoreTouched] = useState(false);
  // 배출 ✓/✗ 프리필을 실제로 고쳤는가 (2026-07-30) — scoreTouched 는 점수만 추적해서
  // 배출 라벨이 운영자 판단인지 시스템 판정 수락인지 구분이 안 됐다.
  const [dischargeTouched, setDischargeTouched] = useState(false);
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 펄스별 세그먼트 (docs/50) — 급이 중단/지속 판단을 위해 펄스 단위로 평가.
  const [segClip, setSegClip] = useState<VisionSegmentsClip | null>(null);
  const [pulseLabels, setPulseLabels] = useState<Record<number, PulseLabel>>({});
  // 세그먼트 조회가 settle 된 cycle — 로드 완료 전엔 모달을 띄우지 않는다
  // (2026-07-19: 로드 전 컴팩트 레이아웃이 먼저 떴다가 펄스별 UI 로 커지는 2단계 표시 방지).
  const [segFor, setSegFor] = useState<string | null>(null);
  // 펄스별 로드 상태 (2026-09-16) — 조회 실패 시 조용히 칩 폴백으로 확정되던 것을 드러낸다.
  // 실패하면 그 사이클의 펄스별 정답지를 영구히 잃으므로(모달 1회) 재시도 + 표시가 필요하다.
  const [segRetry, setSegRetry] = useState(0); // 진행 중인 재시도 회차 (0 = 재시도 아님)
  const [segFailed, setSegFailed] = useState(false); // 재시도 소진 — 칩 폴백 확정

  // 사이클 전체 AI = r3 펄스별 사전 채점 평균 (2026-07-19 운영자 결정).
  // bootstrap 통클립 점수는 7일 168건이 0.469~0.509 로 무정보 — 표시·gap 분류 기준에서
  // 교체. 모델 점수가 없으면(LRCN 다운 등) 기존 통클립 점수 폴백.
  const modelMean = useMemo(() => {
    const xs = (segClip?.segments ?? [])
      .map(sg => sg.model_score)
      .filter((v): v is number => v != null);
    return xs.length > 0 ? xs.reduce((a, b) => a + b, 0) / xs.length : null;
  }, [segClip]);
  const lrcnScore = modelMean ?? bootstrapScore;

  // 모달 open 시 state reset
  useEffect(() => {
    if (open) {
      setOperatorScore(Number(lrcnScore.toFixed(2)));
      setScoreTouched(false); // 프리필 적용 = 아직 미조작
      setDischargeTouched(false);
      setReason('');
      setError(null);
      setSubmitting(false);
    }
  }, [open, lrcnScore]);

  // 모달 open 시 그 사이클의 펄스 세그먼트 로드 (요동 prior 초기값).
  useEffect(() => {
    if (!open || !cycleId) {
      setSegClip(null);
      setPulseLabels({});
      setSegFor(null);
      return;
    }
    let cancelled = false;
    setSegRetry(0);
    setSegFailed(false);
    // 조회 실패·빈 응답 재시도 (2026-09-16). 종전에는 한 번 실패하면 그대로 칩 폴백으로
    // 확정돼 그 사이클의 펄스별 정답지를 영구히 잃었다 (모달은 사이클당 1회).
    // 9-16 15:37 tank_01 실측: 세그먼트는 모달 40초 전에 적재됐는데 화면은 칩만 떴다.
    // 조회가 느려 앱 타임아웃(20s, api.ts)에 걸린 것으로 추정 — 보고 직후 14.7s 실측.
    // 늦게 채워져도 되므로(운영자 지시) 모달은 먼저 띄우고 뒤에서 계속 재시도한다.
    const MAX_FAIL_RETRY = 12;
    const FAIL_RETRY_MS = 5000;
    // 사전 채점(vision.segment.model_scores.recorded)은 보고보다 ~7-12s 늦게 적재된다 —
    // 모델 점수가 아직 없으면 잠시 후 재조회 (최대 3회). 모달은 settle 후에만 뜬다.
    const load = (attempt: number, failAttempt: number) => {
      const retryLater = (reason: string, err?: unknown) => {
        if (failAttempt >= MAX_FAIL_RETRY) {
          console.error('[bluei-edge] 펄스 세그먼트 조회 실패 — 재시도 소진', { cycleId, reason, err });
          setSegFailed(true);
          setSegRetry(0);
          return;
        }
        console.error('[bluei-edge] 펄스 세그먼트 조회 실패 — 재시도', { cycleId, reason, attempt: failAttempt + 1, err });
        setSegRetry(failAttempt + 1);
        setSegFor(cycleId); // 모달은 먼저 띄운다 — 펄스별 UI 는 채워지는 대로 교체
        setTimeout(() => { if (!cancelled) load(attempt, failAttempt + 1); }, FAIL_RETRY_MS);
      };
      Vision.segments(cycleId)
        .then(res => {
          if (cancelled) return;
          const clips = res.clips ?? [];
          // 관측 카메라(탑뷰) 클립 우선 → 요동곡선 있는 클립 → 첫 클립
          const obsCam = (observation?.['camera_id'] as string | undefined) ?? '';
          const clip =
            clips.find(c => c.camera_id === obsCam) ??
            clips.find(c => c.has_agitation) ??
            clips[0] ??
            null;
          const segs = clip?.segments ?? [];
          if (segs.length === 0) {
            retryLater('empty');
            return;
          }
          if (!segs.some(sg => sg.model_score != null) && attempt < 3) {
            setTimeout(() => { if (!cancelled) load(attempt + 1, failAttempt); }, 4000);
            return;
          }
          setSegRetry(0);
          setSegFailed(false);
          setSegFor(cycleId);
          setSegClip(clip);
          const init: Record<number, PulseLabel> = {};
          for (const sg of segs) {
            // 배출 여부 초기값 = 시스템 판정(discharge_check). 'no' 면 무배출로 시작, 운영자가 교정.
            const sysVerdict = pulseVerdicts[sg.pulse_no - 1];
            // 프리필 = 2-head 모델 사전 채점 우선(2026-07-18), 없으면 요동 prior 폴백.
            init[sg.pulse_no] = { score: sg.model_score ?? sg.agitation_prior ?? 0.5, source: 'prior', discharged: sysVerdict !== 'no' };
          }
          setPulseLabels(init);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          retryLater('error', err);
        });
    };
    load(0, 0);
    return () => { cancelled = true; };
  }, [open, cycleId, observation, pulseVerdicts]);

  const setPulseScore = useCallback((pulseNo: number, score: number) => {
    setPulseLabels(prev => ({ ...prev, [pulseNo]: { ...prev[pulseNo], score, source: 'operator' } }));
  }, []);
  // 배출 여부 토글 — 무배출(false)로 바꾸면 활동성 학습 제외 + 전체 배출 집계에 반영.
  const togglePulseDischarged = useCallback((pulseNo: number) => {
    setDischargeTouched(true);
    setPulseLabels(prev => ({ ...prev, [pulseNo]: { ...prev[pulseNo], discharged: !prev[pulseNo]?.discharged } }));
  }, []);

  // 사이클 전체 배출 집계 — 펄스별 무배출 수로 자동 산출 (전부/일부/안나옴 버튼 대체).
  const dischargeAgg = useMemo<'' | 'full' | 'partial' | 'none'>(() => {
    const segs = segClip?.segments ?? [];
    if (segs.length === 0) return '';
    const noCount = segs.filter(sg => pulseLabels[sg.pulse_no]?.discharged === false).length;
    if (noCount === 0) return 'full';
    if (noCount >= segs.length) return 'none';
    return 'partial';
  }, [segClip, pulseLabels]);

  const handleSubmit = useCallback(async () => {
    if (!observation?.observation_id) {
      setError(tr('cycleDispute.errorNoObservationId'));
      return;
    }
    const cameraId = (observation['camera_id'] as string | undefined) ?? 'fixture';
    setSubmitting(true);
    setError(null);
    const verdict = verdictFromGap(operatorScore, lrcnScore);
    const reasonText = reason.trim()
      ? tr('cycleDispute.reasonWithMemo', { memo: reason.trim() })
      : tr('cycleDispute.reasonDefault', { gap: (operatorScore - lrcnScore).toFixed(2) });
    try {
      // 1) 펄스별 세그먼트 라벨 (docs/50 — 사람 점수 = 학습 target). 세그먼트 있을 때만.
      if (segClip && segClip.segments.length > 0) {
        await Vision.saveSegmentLabels({
          cycle_id: cycleId,
          clip_id: segClip.clip_id,
          camera_id: segClip.camera_id,
          tank_id: tankId,
          operator_id: operatorId,
          algorithm_id: SEGMENT_ALGORITHM_ID,
          // 배출 분류는 시스템 판정이 프리필된다 — 운영자가 실제로 고쳤는지 함께 남겨야
          // 이 라벨을 정답지로 쓸 수 있는지 판별된다 (2026-08-12).
          discharge_modified: dischargeTouched,
          segments: segClip.segments.map(sg => {
            const lbl = pulseLabels[sg.pulse_no];
            // 무배출(discharged=false) → 활동성 학습 제외(격리).
            if (lbl && !lbl.discharged) {
              return {
                pulse_no: sg.pulse_no,
                offset_start_ms: sg.offset_start_ms,
                offset_end_ms: sg.offset_end_ms,
                score: 0,
                prior: sg.agitation_prior,
                source: 'prior' as const,
                excluded: true,
                exclude_reason: 'feed_not_delivered',
              };
            }
            return {
              pulse_no: sg.pulse_no,
              offset_start_ms: sg.offset_start_ms,
              offset_end_ms: sg.offset_end_ms,
              score: lbl?.score ?? sg.model_score ?? sg.agitation_prior ?? 0.5,
              prior: sg.agitation_prior,
              source: lbl?.source ?? 'prior',
            };
          }),
        });
      }
      // 2) 사이클 전체(통클립) dispute — 병행 저장. 배출은 펄스별 무배출 집계에서 자동 산출.
      await Vision.dispute(observation.observation_id, {
        camera_id: cameraId,
        tank_id: tankId,
        operator_id: operatorId,
        verdict,
        reason: reasonText,
        operator_score: operatorScore,
        ai_score: Number(lrcnScore.toFixed(2)), // 프리필 AI값 — 격차(op-ai) 수렴 지표 원천
        prefill_modified: scoreTouched, // 프리필을 실제로 고쳤는가 (false=수락 → 학습·보정 제외 대상)
        discharge_modified: dischargeTouched, // 배출 ✓/✗ 를 고쳤는가 (false=시스템 판정 수락)
        ...(dischargeAgg ? { operator_discharge: dischargeAgg } : {}),
      });
      onSubmitted?.();
      onClose();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('cycleDispute.errorUnknown');
      setError(msg);
      setSubmitting(false);
    }
  }, [observation, operatorScore, scoreTouched, dischargeTouched, lrcnScore, reason, dischargeAgg, tankId, operatorId, onSubmitted, onClose, tr, segClip, pulseLabels, cycleId]);

  const handleSkip = useCallback(() => {
    onSkip?.();
    onClose();
  }, [onSkip, onClose]);

  // 키보드 단축키: 1-5 score 선택 + Enter submit + Escape skip
  useEffect(() => {
    if (!open || segFor !== cycleId) return; // 모달 미표시(세그먼트 로드 중) 동안 오입력 방지
    function onKey(e: KeyboardEvent) {
      if (submitting) return;
      if (e.key === 'Escape') {
        e.preventDefault();
        handleSkip();
        return;
      }
      if (e.key === 'Enter' && !e.shiftKey) {
        // Enter on textarea 는 무시 (memo 줄바꿈 허용은 Shift+Enter)
        if ((e.target as HTMLElement | null)?.tagName === 'TEXTAREA') return;
        e.preventDefault();
        handleSubmit();
        return;
      }
      const score = KEY_TO_SCORE[e.key];
      if (score !== undefined) {
        e.preventDefault();
        setOperatorScore(score);
        setScoreTouched(true);
      }
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, submitting, handleSubmit, handleSkip, segFor, cycleId]);

  if (!open || !observation || segFor !== cycleId) return null;

  const verdict = verdictFromGap(operatorScore, lrcnScore);
  const verdictLabel = verdict === 'correct' ? tr('cycleDispute.verdictCorrect') : verdict === 'wrong' ? tr('cycleDispute.verdictWrong') : tr('cycleDispute.verdictUnsure');
  const verdictColor =
    verdict === 'correct'
      ? 'text-green-300'
      : verdict === 'wrong'
        ? 'text-red-300'
        : 'text-yellow-300';

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={tr('cycleDispute.dialogAriaLabel')}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
    >
      <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl p-6 w-full max-w-md space-y-4 max-h-[92vh] overflow-y-auto">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-white">{tr('cycleDispute.title')}</h2>
            {/* 사람용 식별: 수조 · 시각 · 오늘 몇 번째 (2026-07-17 운영자 요청 —
                cycle_id 는 시스템용이라 어느 급이인지 특정 불가) */}
            <p className="text-sm text-yellow-200 mt-1 font-medium">
              {tankId}
              {startedAt && ` · ${new Date(startedAt).toLocaleTimeString(dateLocale(), { hour: '2-digit', minute: '2-digit', hour12: false })}`}
              {ordinal != null && ` · ${tr('cycleDispute.todayOrdinal', { n: ordinal })}`}
            </p>
            {pulsesExecuted != null && (
              <p className="text-xs text-gray-400 mt-0.5">
                {tr('cycleDispute.pulseContext', {
                  pulses: pulsesExecuted,
                  suspected: suspectedNoDischarge ?? '?',
                })}
              </p>
            )}
            {/* 펄스별 판정 스트립 — 세그먼트(펄스별 행)가 없는 구 사이클 폴백 요약만. */}
            {pulseVerdicts.length > 0 && !(segClip && segClip.segments.length > 0) && (
              <div className="flex flex-wrap gap-1 mt-1">
                {pulseVerdicts.map((v, i) => (
                  <span
                    key={i}
                    title={`${i + 1}/${pulseVerdicts.length}`}
                    className={`px-1.5 py-0.5 rounded text-[10px] font-mono border ${
                      v === 'no'
                        ? 'bg-red-500/15 text-red-300 border-red-500/50'
                        : v === 'unclear'
                          ? 'bg-yellow-500/10 text-yellow-300 border-yellow-500/40'
                          : v === 'yes'
                            ? 'bg-emerald-500/10 text-emerald-300 border-emerald-500/30'
                            : 'bg-gray-800 text-gray-500 border-gray-700'
                    }`}
                  >
                    {i + 1}/{pulseVerdicts.length} {v === 'no' ? '✗' : v === 'unclear' ? '?' : v === 'yes' ? '✓' : '–'}
                  </span>
                ))}
              </div>
            )}
            <p className="text-[10px] text-gray-600 mt-0.5 font-mono break-all" title={tr('cycleDispute.systemIdHint')}>
              {cycleId}
            </p>
          </div>
          <button
            type="button"
            aria-label={tr('cycleDispute.skip')}
            disabled={submitting}
            onClick={handleSkip}
            className="text-gray-500 hover:text-white transition-colors text-lg leading-none disabled:opacity-50"
          >
            ✕
          </button>
        </div>

        {error && (
          <p className="text-xs text-red-400 font-mono px-3 py-2 bg-red-500/10 border border-red-500/20 rounded-md">
            {error}
          </p>
        )}

        <div className="space-y-3">
          {/* 펄스별 로드 상태 — 조용한 칩 폴백을 드러낸다 (2026-09-16) */}
          {segRetry > 0 && (
            <p className="text-[11px] text-amber-300/80 font-mono px-2 py-1 bg-amber-500/10 border border-amber-500/20 rounded">
              {tr('cycleDispute.perPulseRetrying', { n: segRetry })}
            </p>
          )}
          {segFailed && (
            <p className="text-[11px] text-red-400 font-mono px-2 py-1 bg-red-500/10 border border-red-500/20 rounded">
              {tr('cycleDispute.perPulseFailed')}
            </p>
          )}

          {/* 펄스별 평가 (docs/50 — 급이 중단/지속 판단용, 사람 점수 = 학습 target) */}
          {segClip && segClip.segments.length > 0 && (
            <div className="space-y-1.5">
              <div className="flex flex-col">
                <span className="text-xs font-semibold text-yellow-200">{tr('cycleDispute.perPulseTitle')}</span>
                <span className="text-[10px] text-gray-500">{tr('cycleDispute.perPulseHint')}</span>
              </div>
              {segClip.segments.map(sg => {
                const lbl = pulseLabels[sg.pulse_no] ?? { score: 0.5, source: 'prior' as const, discharged: true };
                const noFeed = !lbl.discharged;
                return (
                  <div key={sg.pulse_no} className={`flex items-center gap-2 px-2 py-1 rounded bg-gray-800/40 ${noFeed ? 'opacity-60' : ''}`}>
                    <span className="text-xs font-mono text-gray-300 w-6">#{sg.pulse_no}</span>
                    {/* 배출 여부 토글 — 클릭해 운영자가 교정. ✓=배출, ✗=무배출(활동성 제외) */}
                    <button
                      type="button"
                      disabled={submitting}
                      onClick={() => togglePulseDischarged(sg.pulse_no)}
                      title={tr('cycleDispute.dischargeToggle')}
                      className={`text-xs font-mono w-5 rounded border ${noFeed ? 'bg-red-500/15 text-red-300 border-red-500/50' : 'bg-emerald-500/10 text-emerald-300 border-emerald-500/40'}`}
                    >
                      {noFeed ? '✗' : '✓'}
                    </button>
                    {sg.model_score != null ? (
                      <span className="text-[10px] text-cyan-300 font-mono w-12" title={tr('cycleDispute.modelLabel')}>AI {sg.model_score.toFixed(2)}</span>
                    ) : sg.agitation_prior != null && (
                      <span className="text-[10px] text-gray-500 font-mono w-12" title={tr('cycleDispute.priorLabel')}>~{sg.agitation_prior.toFixed(2)}</span>
                    )}
                    <input
                      type="range" min={0} max={1} step={0.05}
                      value={lbl.score}
                      disabled={submitting || noFeed}
                      onChange={e => setPulseScore(sg.pulse_no, Number(e.target.value))}
                      className="flex-1 accent-yellow-300"
                      aria-label={tr('cycleDispute.pulseScoreAria', { n: sg.pulse_no })}
                    />
                    <span className={`text-xs font-mono w-8 text-right ${noFeed ? 'text-red-300' : lbl.source === 'operator' ? 'text-yellow-200' : 'text-gray-500'}`}>
                      {noFeed ? tr('cycleDispute.noFeedShort') : lbl.score.toFixed(2)}
                    </span>
                  </div>
                );
              })}
            </div>
          )}

          {/* 사이클 전체 (참고 · 별도 저장) — 통클립 LRCN 점수 */}
          <div className="pt-2 border-t border-gray-800">
            <div className="text-[10px] text-gray-500 mb-1">{tr('cycleDispute.wholeCycleSection')}</div>
          </div>
          {/* AI score 비교 — r3 펄스 평균 우선, 없으면 통클립 LRCN 폴백 */}
          <div className="flex items-center justify-between text-xs">
            <span className="text-gray-400">{tr(modelMean != null ? 'cycleDispute.aiScorePulseMean' : 'cycleDispute.aiScore')}</span>
            <span className={`font-mono ${modelMean != null ? 'text-cyan-300' : 'text-blue-300'}`}>{lrcnScore.toFixed(2)}</span>
          </div>
          <div className="h-1.5 rounded-full bg-gray-800 relative overflow-hidden">
            <div
              className="absolute top-0 left-0 h-full bg-blue-500/60"
              style={{ width: `${lrcnScore * 100}%` }}
            />
            <div
              className="absolute top-0 h-full w-0.5 bg-yellow-300"
              style={{ left: `${operatorScore * 100}%` }}
              title={tr('cycleDispute.operatorScore')}
            />
          </div>

          {/* 운영자 슬라이더 */}
          <div className="flex flex-col gap-1.5">
            <label htmlFor="op-score" className="flex items-center justify-between text-xs">
              <span className="text-gray-400">{tr('cycleDispute.operatorScoreLabel')}</span>
              <span className="font-mono text-yellow-200">{operatorScore.toFixed(2)}</span>
            </label>
            <input
              id="op-score"
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={operatorScore}
              onChange={e => { setOperatorScore(Number(e.target.value)); setScoreTouched(true); }}
              disabled={submitting}
              className="accent-yellow-300"
              aria-label={tr('cycleDispute.activityScore')}
            />
            <div className="flex justify-between text-[10px] text-gray-600 font-mono">
              <span>{tr('cycleDispute.scoreMin')}</span>
              <span>0.5</span>
              <span>{tr('cycleDispute.scoreMax')}</span>
            </div>
          </div>

          {/* 자동 verdict */}
          <div className="flex items-center justify-between text-xs px-2 py-1.5 rounded-md bg-gray-800/60 border border-gray-700/60">
            <span className="text-gray-500">{tr('cycleDispute.autoClassify')}</span>
            <span className={`font-semibold ${verdictColor}`}>{verdictLabel}</span>
          </div>

          {/* 실배출 — 펄스별 무배출(✗) 집계에서 자동 산출 (2026-07-18 통합, 전부/일부/안나옴 버튼 대체) */}
          {segClip && segClip.segments.length > 0 && (
            <div className="flex items-center justify-between text-xs px-2 py-1.5 rounded-md bg-gray-800/40 border border-gray-700/40">
              <span className="text-gray-400">{tr('cycleDispute.dischargeLabel')}</span>
              <span className={`font-mono ${dischargeAgg === 'none' ? 'text-red-300' : dischargeAgg === 'partial' ? 'text-yellow-300' : 'text-emerald-300'}`}>
                {tr('cycleDispute.dischargeSummary', {
                  n: segClip.segments.filter(sg => pulseLabels[sg.pulse_no]?.discharged !== false).length,
                  total: segClip.segments.length,
                })}
                {' · '}
                {dischargeAgg === 'full' ? tr('cycleDispute.dischargeFull') : dischargeAgg === 'none' ? tr('cycleDispute.dischargeNone') : tr('cycleDispute.dischargePartial')}
              </span>
            </div>
          )}

          {/* memo */}
          <div className="flex flex-col gap-1">
            <label htmlFor="dispute-memo" className="text-xs text-gray-400">
              {tr('cycleDispute.memoLabel')}
            </label>
            <textarea
              id="dispute-memo"
              rows={2}
              value={reason}
              onChange={e => setReason(e.target.value)}
              disabled={submitting}
              placeholder={tr('cycleDispute.memoPlaceholder')}
              className="px-3 py-1.5 rounded-md border border-gray-700 bg-gray-900 text-sm text-white placeholder-gray-600 focus:outline-none focus:ring-2 focus:ring-yellow-300/40 resize-none disabled:opacity-50"
            />
          </div>
        </div>

        <div className="flex gap-2 pt-1">
          <Button
            type="button"
            size="sm"
            onClick={handleSubmit}
            disabled={submitting}
            aria-label={tr('cycleDispute.submitAriaLabel')}
          >
            {submitting ? tr('cycleDispute.submitting') : tr('cycleDispute.submit')}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleSkip}
            disabled={submitting}
            aria-label={tr('cycleDispute.skipAriaLabel')}
          >
            {tr('cycleDispute.skipEsc')}
          </Button>
        </div>

        <p className="text-[10px] text-gray-600 leading-tight">
          {tr('cycleDispute.shortcutHint')}
        </p>
      </div>
    </div>
  );
}
