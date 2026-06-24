import { useState } from 'react';
import { Vision } from '../../lib/api';
import type { VisionTrainingStatus, VisionAlgorithmApplyResponse } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { useLanguage } from '../../lib/language-context';

/**
 * 5/9 ai-training.js 의 promoteAlgorithm + rollbackAlgorithm.
 *
 * 학습 완료된 job 만 promote 가능. 안 그러면 422.
 * 이미 ModelRegistry 의 promote/rollback 과 같은 endpoint — 두 화면 동기 폴링됨.
 */
export function PromoteCard({
  status, onChanged,
}: {
  status: VisionTrainingStatus | null;
  onChanged: () => void;
}) {
  const { tr } = useLanguage();
  const [confirm, setConfirm] = useState<null | 'promote' | 'rollback'>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<VisionAlgorithmApplyResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (!status) return null;
  const job = status.current_job;

  async function handlePromote() {
    if (!status?.algorithm_id) return;
    setBusy(true);
    setError(null);
    try {
      const candidate = job?.candidate_path ? `${job.candidate_path}/best.pt` : '';
      const r = await Vision.promote(status.algorithm_id, {
        job_id: job?.job_id,
        candidate_path: candidate,
        operator_id: 'operator',
      });
      setResult(r);
      setConfirm(null);
      onChanged();
    } catch (e) {
      setError(tr('promoteCard.errorApplyFailed') + (e instanceof Error ? e.message : 'unknown'));
    } finally {
      setBusy(false);
    }
  }

  async function handleRollback() {
    if (!status?.algorithm_id) return;
    setBusy(true);
    setError(null);
    try {
      const r = await Vision.rollback(status.algorithm_id, {
        operator_id: 'operator',
      });
      setResult(r);
      setConfirm(null);
      onChanged();
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'unknown';
      if (msg.includes('NO_HISTORY') || msg.includes('되돌릴 수 없')) {
        setError(tr('promoteCard.errorNoHistory'));
      } else {
        setError(tr('promoteCard.errorRollbackFailed') + msg);
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{tr('promoteCard.title')}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="text-xs text-gray-400 space-y-0.5">
          <p>{tr('promoteCard.warningLine1')}</p>
          <p>{tr('promoteCard.warningLine2')}</p>
        </div>

        {error && (
          <div className="px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
            {error}
          </div>
        )}

        <div className="flex items-center gap-2">
          <button
            disabled={busy}
            onClick={() => { setError(null); setConfirm('rollback'); }}
            className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 text-gray-200 transition-colors"
          >
            {tr('promoteCard.btnRollback')}
          </button>
          <button
            disabled={busy}
            onClick={() => { setError(null); setConfirm('promote'); }}
            className="px-4 py-2 text-sm rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
          >
            {tr('promoteCard.btnPromote')}
          </button>
        </div>

        {result && (
          <div className="text-xs text-gray-300 space-y-0.5 pt-2 border-t border-gray-700/30">
            {result.applied?.action === 'promote' ? (
              <p>{tr('promoteCard.resultPromoted')}</p>
            ) : (
              <p>{tr('promoteCard.resultRolledBack')}</p>
            )}
            <p className="font-mono text-gray-400 break-all">
              weights: {result.active?.active_weights_path ?? '-'}
            </p>
            {result.note && (
              <p className="text-gray-500">{result.note}</p>
            )}
          </div>
        )}

        <ConfirmDialog
          open={confirm === 'promote'}
          title={tr('promoteCard.confirmPromoteTitle')}
          message={tr('promoteCard.confirmPromoteMessage')}
          confirmLabel={tr('promoteCard.confirmPromoteLabel')}
          destructive={false}
          busy={busy}
          onConfirm={handlePromote}
          onCancel={() => { setConfirm(null); setError(null); }}
        />
        <ConfirmDialog
          open={confirm === 'rollback'}
          title={tr('promoteCard.confirmRollbackTitle')}
          message={tr('promoteCard.confirmRollbackMessage')}
          confirmLabel={tr('promoteCard.confirmRollbackLabel')}
          destructive
          busy={busy}
          onConfirm={handleRollback}
          onCancel={() => { setConfirm(null); setError(null); }}
        />
      </CardContent>
    </Card>
  );
}
