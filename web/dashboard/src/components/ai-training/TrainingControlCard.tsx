import { useState } from 'react';
import { Vision } from '../../lib/api';
import type { VisionTrainingStatus } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { useLanguage } from '../../lib/language-context';

/**
 * 5/9 ai-training.html 의 "가르치기" 카드 — startTraining + cancelTraining + 진행률.
 */
export function TrainingControlCard({
  status, onChanged,
}: {
  status: VisionTrainingStatus | null;
  onChanged: () => void;
}) {
  const { tr } = useLanguage();
  const [confirm, setConfirm] = useState<null | 'start' | 'cancel'>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!status) return null;

  const boot = status.bootstrap;
  const job = status.current_job;
  const canTrain = boot.can_start_training;
  const isRunning = status.is_running;

  const needBoxes = Math.max(0, boot.required_boxes - boot.box_count);
  const needFrames = Math.max(0, boot.required_frames - boot.frame_count);
  const startBtnLabel = canTrain
    ? tr('trainingControl.startBtnLabel')
    : `${tr('trainingControl.startBtnLabel')} ${tr('trainingControl.needMore', { boxes: needBoxes, frames: needFrames })}`;

  let tag = tr('trainingControl.tagIdle');
  let tagColor = 'bg-gray-500/20 text-gray-300 border-gray-500/40';
  if (isRunning) {
    tag = tr('trainingControl.tagRunning');
    tagColor = 'bg-amber-500/20 text-amber-300 border-amber-500/40';
  } else if (job?.status === 'completed') {
    tag = tr('trainingControl.tagCompleted');
    tagColor = 'bg-green-500/20 text-green-300 border-green-500/40';
  } else if (job?.status === 'failed') {
    tag = tr('trainingControl.tagFailed');
    tagColor = 'bg-red-500/20 text-red-300 border-red-500/40';
  }

  const progressPct = Math.round(job?.progress_pct ?? 0);

  async function handleStart() {
    if (!status?.algorithm_id) {
      setError(tr('trainingControl.errNoAlgorithm'));
      setConfirm(null);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await Vision.trainingStart({ algorithm_id: status.algorithm_id });
      setConfirm(null);
      onChanged();
    } catch (e) {
      setError(tr('trainingControl.errStartFailed') + (e instanceof Error ? e.message : 'unknown'));
    } finally {
      setBusy(false);
    }
  }

  async function handleCancel() {
    if (!job) return;
    setBusy(true);
    setError(null);
    try {
      await Vision.trainingCancel(job.job_id);
      setConfirm(null);
      onChanged();
    } catch (e) {
      setError(tr('trainingControl.errCancelFailed') + (e instanceof Error ? e.message : 'unknown'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-baseline justify-between gap-3">
          <span>{tr('trainingControl.title')}</span>
          <span className={`px-2 py-0.5 rounded text-xs border ${tagColor}`}>{tag}</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {error && (
          <div className="px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
            {error}
          </div>
        )}

        {!isRunning && (
          <>
            <p className="text-xs text-gray-500">
              {tr('trainingControl.hintBoxes')}
            </p>
            <button
              disabled={!canTrain || busy}
              onClick={() => { setError(null); setConfirm('start'); }}
              className="px-4 py-2.5 text-sm rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
            >
              {startBtnLabel}
            </button>
            <div className="text-xs text-gray-500 space-y-0.5">
              <p>{tr('trainingControl.hintTime')}</p>
              <p>{tr('trainingControl.hintCurrentAI')}</p>
            </div>
          </>
        )}

        {isRunning && (
          <div className="space-y-2">
            <div className="flex items-baseline justify-between text-sm">
              <b className="text-gray-200">{tr('trainingControl.currentStage')}</b>
              <span className="text-gray-300">{job?.stage_label ?? tr('trainingControl.stageFallback')}</span>
            </div>
            <div className="h-2 w-full bg-gray-700/50 rounded-full overflow-hidden">
              <div className="h-full bg-amber-500 transition-all" style={{ width: `${progressPct}%` }} />
            </div>
            <small className="text-xs text-gray-500">{progressPct}%</small>
            <button
              disabled={busy}
              onClick={() => { setError(null); setConfirm('cancel'); }}
              className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 text-gray-200 transition-colors"
            >
              {tr('trainingControl.cancelBtn')}
            </button>
          </div>
        )}

        {job?.status === 'failed' && (
          <div className="text-xs text-red-300 font-mono">
            {tr('trainingControl.failedPrefix')} {job.error ?? tr('trainingControl.unknownError')}
          </div>
        )}

        <ConfirmDialog
          open={confirm === 'start'}
          title={tr('trainingControl.confirmStartTitle')}
          message={tr('trainingControl.confirmStartMessage')}
          confirmLabel={tr('trainingControl.confirmStartLabel')}
          destructive={false}
          busy={busy}
          onConfirm={handleStart}
          onCancel={() => { setConfirm(null); setError(null); }}
        />
        <ConfirmDialog
          open={confirm === 'cancel'}
          title={tr('trainingControl.confirmCancelTitle')}
          message={tr('trainingControl.confirmCancelMessage')}
          confirmLabel={tr('trainingControl.confirmCancelLabel')}
          destructive
          busy={busy}
          onConfirm={handleCancel}
          onCancel={() => { setConfirm(null); setError(null); }}
        />
      </CardContent>
    </Card>
  );
}
