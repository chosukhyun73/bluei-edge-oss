import { useCallback, useEffect, useMemo, useState } from 'react';
import { Vision } from '../../lib/api';
import { Button } from '../ui/button';
import type { VisionObservation, VisionDisputeVerdict } from '../../lib/types';
import { useLanguage } from '../../lib/language-context';

// G-4 hook (useCycleCompletionDispute) 은 별도 파일 (useCycleCompletionDispute.ts) 분리 —
// vite React Fast Refresh 가 컴포넌트+비컴포넌트 export 동거를 invalidate 하기 때문.

interface CycleCompletionDisputeProps {
  open: boolean;
  observation: VisionObservation | null;
  cycleId: string;
  tankId: string;
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
  onClose,
  onSubmitted,
  onSkip,
  operatorId = 'operator_default',
}: CycleCompletionDisputeProps) {
  const { tr } = useLanguage();
  const lrcnScore = useMemo(() => lrcnScoreFrom(observation), [observation]);
  const [operatorScore, setOperatorScore] = useState<number>(0.5);
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 모달 open 시 state reset
  useEffect(() => {
    if (open) {
      setOperatorScore(Number(lrcnScore.toFixed(2)));
      setReason('');
      setError(null);
      setSubmitting(false);
    }
  }, [open, lrcnScore]);

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
      await Vision.dispute(observation.observation_id, {
        camera_id: cameraId,
        tank_id: tankId,
        operator_id: operatorId,
        verdict,
        reason: reasonText,
        operator_score: operatorScore,
      });
      onSubmitted?.();
      onClose();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('cycleDispute.errorUnknown');
      setError(msg);
      setSubmitting(false);
    }
  }, [observation, operatorScore, lrcnScore, reason, tankId, operatorId, onSubmitted, onClose, tr]);

  const handleSkip = useCallback(() => {
    onSkip?.();
    onClose();
  }, [onSkip, onClose]);

  // 키보드 단축키: 1-5 score 선택 + Enter submit + Escape skip
  useEffect(() => {
    if (!open) return;
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
      }
    }
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, submitting, handleSubmit, handleSkip]);

  if (!open || !observation) return null;

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
      <div className="bg-gray-900 border border-gray-700 rounded-xl shadow-2xl p-6 w-full max-w-md space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-white">{tr('cycleDispute.title')}</h2>
            <p className="text-xs text-gray-500 mt-0.5">
              Tank: <span className="font-mono text-gray-400">{tankId}</span>
            </p>
            <p className="text-xs text-gray-500 mt-0.5 font-mono break-all">
              cycle: {cycleId}
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
          {/* LRCN AI score 비교 */}
          <div className="flex items-center justify-between text-xs">
            <span className="text-gray-400">{tr('cycleDispute.aiScore')}</span>
            <span className="font-mono text-blue-300">{lrcnScore.toFixed(2)}</span>
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
              onChange={e => setOperatorScore(Number(e.target.value))}
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
