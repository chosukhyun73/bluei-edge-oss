import { useCallback, useEffect, useState } from 'react';
import { Vision, Tanks } from '../../lib/api';
import type { VisionTrainingStatus, Tank, StateVector } from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { useLanguage } from '../../lib/language-context';

/**
 * 5/9 ai-training.js 의 baseline 학습 카드 — Cage/Tank 별 autoencoder 이상 감지.
 * 자기지도 — 라벨 불필요. 7일 이상 운영 데이터 누적 후 학습.
 */
export function TankBaselineCard({
  status, onChanged,
}: {
  status: VisionTrainingStatus | null;
  onChanged: () => void;
}) {
  const { tr } = useLanguage();
  const [tanks, setTanks] = useState<Tank[]>([]);
  const [tankId, setTankId] = useState<string>('');
  const [statusText, setStatusText] = useState<string>(() => tr('tankBaseline.statusChecking'));
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    (async () => {
      try {
        const res = await Tanks.list();
        const items = res.items ?? [];
        setTanks(items);
        if (items.length > 0 && !tankId) setTankId(items[0].tank_id);
      } catch (e) {
        setError(tr('tankBaseline.errorLoadTanks') + (e instanceof Error ? e.message : 'unknown'));
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const refreshStatus = useCallback(async () => {
    if (!tankId) {
      setStatusText(tr('tankBaseline.selectTankPrompt'));
      return;
    }
    try {
      const sv = await Tanks.stateVector(tankId);
      const an = (sv as StateVector & { anomaly: Record<string, unknown> }).anomaly ?? {};
      if (an.has_model) {
        const score = typeof an.latest_anomaly_score === 'number'
          ? an.latest_anomaly_score.toFixed(3)
          : '-';
        const verdict = (an.latest_verdict as string | undefined) ?? '-';
        setStatusText(`${tr('tankBaseline.statusActiveModel')} ${score} (verdict: ${verdict})`);
      } else {
        setStatusText(tr('tankBaseline.statusNoModel'));
      }
    } catch {
      setStatusText(tr('tankBaseline.statusFetchFail'));
    }
  }, [tankId]);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  async function handleStart() {
    if (!tankId) return;
    setBusy(true);
    setError(null);
    try {
      await Vision.trainingStart({ kind: 'tank-baseline', tank_id: tankId });
      setConfirm(false);
      onChanged();
    } catch (e) {
      setError(tr('tankBaseline.errorTrainingStart') + (e instanceof Error ? e.message : 'unknown'));
    } finally {
      setBusy(false);
    }
  }

  const isRunning = status?.is_running ?? false;
  const job = status?.current_job;
  const isThisBaseline = job?.kind === 'tank-baseline' && (job as { tank_id?: string }).tank_id === tankId;
  const showProgress = isRunning && isThisBaseline;
  const progressPct = Math.round(job?.progress_pct ?? 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-baseline justify-between gap-3">
          <span>🔍 {tr('tankBaseline.cardTitle')}</span>
          <span className="px-2 py-0.5 rounded text-xs bg-blue-500/20 text-blue-300 border border-blue-500/40">
            {tr('tankBaseline.badgeSelfSupervised')}
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-xs text-gray-500">
          {tr('tankBaseline.description')}
        </p>

        {error && (
          <div className="px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
            {error}
          </div>
        )}

        <div className="flex items-center gap-2">
          <label className="text-xs text-gray-400">{tr('tankBaseline.labelTargetTank')}</label>
          <select
            value={tankId}
            onChange={e => setTankId(e.target.value)}
            className="px-2 py-1 text-sm bg-gray-900 border border-gray-600 rounded text-white [&>option]:bg-gray-900 [&>option]:text-white"
          >
            {tanks.length === 0 ? (
              <option value="">{tr('tankBaseline.optionNoTanks')}</option>
            ) : (
              <>
                <option value="">{tr('tankBaseline.optionSelectTank')}</option>
                {tanks.map(t => (
                  <option key={t.tank_id} value={t.tank_id}>
                    {t.display_name ?? t.tank_id}
                  </option>
                ))}
              </>
            )}
          </select>
        </div>

        <div className="text-xs text-gray-400">{statusText}</div>

        <button
          disabled={!tankId || isRunning || busy}
          onClick={() => { setError(null); setConfirm(true); }}
          className="px-3 py-1.5 text-sm rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
        >
          {tr('tankBaseline.buttonStartTraining')}
        </button>

        {showProgress && (
          <div className="space-y-1 pt-2 border-t border-gray-700/30">
            <div className="flex items-baseline justify-between text-sm">
              <b className="text-gray-200">{tr('tankBaseline.progressStageLabel')}</b>
              <span className="text-gray-300">{job?.stage_label ?? tr('tankBaseline.progressTraining')}</span>
            </div>
            <div className="h-1.5 w-full bg-gray-700/50 rounded-full overflow-hidden">
              <div className="h-full bg-amber-500 transition-all" style={{ width: `${progressPct}%` }} />
            </div>
            <small className="text-xs text-gray-500">{progressPct}%</small>
          </div>
        )}

        <ConfirmDialog
          open={confirm}
          title={tr('tankBaseline.confirmTitle')}
          message={tr('tankBaseline.confirmMessage')}
          confirmLabel={tr('tankBaseline.confirmLabel')}
          destructive={false}
          busy={busy}
          onConfirm={handleStart}
          onCancel={() => { setConfirm(false); setError(null); }}
        />
      </CardContent>
    </Card>
  );
}
