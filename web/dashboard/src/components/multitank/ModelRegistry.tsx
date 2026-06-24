import { useCallback, useEffect, useState } from 'react';
import { Cpu, Layers, History, RotateCcw, CheckCircle2, Clock } from 'lucide-react';
import { Vision } from '../../lib/api';
import type {
  VisionAlgorithm, VisionTankApplication,
  VisionAlgorithmActiveState, VisionTrainingStatus,
} from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { relativeTime } from '../../lib/format';
import { useLanguage } from '../../lib/language-context';

const POLL_INTERVAL_MS = 30_000;

function statusBadge(status: string): string {
  switch (status) {
    case 'validated':
      return 'bg-green-500/20 text-green-300 border border-green-500/40';
    case 'candidate':
      return 'bg-amber-500/20 text-amber-300 border border-amber-500/40';
    case 'deprecated':
      return 'bg-gray-500/20 text-gray-400 border border-gray-500/40';
    default:
      return 'bg-blue-500/20 text-blue-300 border border-blue-500/40';
  }
}

// returns a tr key, or null when status is unknown (caller falls back to raw status).
function statusLabelKey(status: string): string | null {
  switch (status) {
    case 'validated': return 'modelRegistry.statusValidated';
    case 'candidate': return 'modelRegistry.statusCandidate';
    case 'deprecated': return 'modelRegistry.statusDeprecated';
    default: return null;
  }
}

function MiniProgress({
  label, current, required, ready,
}: {
  label: string;
  current: number;
  required: number;
  ready: boolean;
}) {
  const pct = required > 0 ? Math.min(100, (current / required) * 100) : 0;
  const barColor = ready ? 'bg-green-500' : pct >= 50 ? 'bg-amber-500' : 'bg-gray-500';
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between text-xs">
        <span className="text-gray-400">{label}</span>
        <span className="font-mono">
          <span className={ready ? 'text-green-300' : 'text-gray-200'}>
            {current.toLocaleString()}
          </span>
          <span className="text-gray-500"> / {required.toLocaleString()}</span>
          {ready && <span className="ml-1 text-green-400">✓</span>}
        </span>
      </div>
      <div className="h-1 w-full bg-gray-700/50 rounded-full overflow-hidden">
        <div className={`h-full ${barColor} transition-all`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

function ActiveStateBlock({ state }: { state: VisionAlgorithmActiveState | null }) {
  const { tr } = useLanguage();
  if (!state || !state.active_weights_path) {
    return (
      <div className="px-3 py-2 bg-gray-900/40 border border-gray-700/40 rounded text-xs text-gray-500">
        <Clock className="w-3 h-3 inline mr-1.5 -mt-0.5" />
        {tr('modelRegistry.noWeightsApplied')}
      </div>
    );
  }
  return (
    <div className="px-3 py-2 bg-green-900/10 border border-green-700/30 rounded text-xs space-y-0.5">
      <div className="flex items-center gap-1.5">
        <CheckCircle2 className="w-3.5 h-3.5 text-green-400" />
        <span className="text-green-300 font-medium">{tr('modelRegistry.currentlyApplied')}</span>
        {state.applied_at && (
          <span className="text-gray-500 ml-1">· {relativeTime(state.applied_at)}</span>
        )}
      </div>
      <div className="text-gray-300 font-mono break-all pl-5">
        {state.active_weights_path}
      </div>
      <div className="flex items-center gap-3 text-gray-500 pl-5">
        {state.active_job_id && <span>job: <span className="text-gray-300 font-mono">{state.active_job_id}</span></span>}
        {state.operator_id && <span>by: <span className="text-gray-300">{state.operator_id}</span></span>}
      </div>
    </div>
  );
}

function HistoryBlock({ state }: { state: VisionAlgorithmActiveState | null }) {
  const { tr } = useLanguage();
  const history = state?.history ?? [];
  if (history.length === 0) return null;
  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1.5 text-xs text-gray-400">
        <History className="w-3 h-3" />
        {tr('modelRegistry.historyLabel')} ({tr('modelRegistry.historyCountRecent', { count: history.length })})
      </div>
      <ul className="space-y-1 pl-4">
        {[...history].reverse().slice(0, 5).map((h, i) => (
          <li key={i} className="text-xs text-gray-500 border-l border-gray-700/50 pl-2 py-0.5">
            <div className="font-mono text-gray-400 break-all">{h.weights_path}</div>
            <div className="flex items-center gap-2 text-gray-600 text-[10px]">
              {h.applied_at && <span>{tr('modelRegistry.appliedAt', { time: relativeTime(h.applied_at) })}</span>}
              {h.removed_at && <span>· {tr('modelRegistry.replacedAt', { time: relativeTime(h.removed_at) })}</span>}
              {h.operator_id && <span>· {h.operator_id}</span>}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

type ConfirmKind = null | 'promote' | 'rollback' | 'train';

// R12 — algorithm 별 학습 kind 결정.
//   classical_cv runtime 또는 LRCN 외 → vision-detector (YOLO)
//   pytorch_lrcn (R11) → lrcn-finetune (algorithm_id suffix 로 phase 분기)
function trainingKindFor(algo: VisionAlgorithm): 'vision-detector' | 'lrcn-finetune' {
  if (algo.model?.runtime === 'pytorch_lrcn') return 'lrcn-finetune';
  return 'vision-detector';
}

function AlgorithmCard({
  algo, applications, activeState, training, onPromoted, onRolledBack,
}: {
  algo: VisionAlgorithm;
  applications: VisionTankApplication[];
  activeState: VisionAlgorithmActiveState | null;
  training: VisionTrainingStatus | null;
  onPromoted: () => void;
  onRolledBack: () => void;
}) {
  const applied = applications.filter(
    a => a.applied_vision_algorithm_id === algo.vision_algorithm_id,
  );

  const { tr } = useLanguage();
  const [confirm, setConfirm] = useState<ConfirmKind>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  // 학습 진척 — 같은 algorithm 일 때만 표시.
  const showTraining = training && training.algorithm_id === algo.vision_algorithm_id;
  const hasHistory = (activeState?.history?.length ?? 0) > 0;
  const hasActive = !!activeState?.active_weights_path;

  // R12 — 카드별 학습 상태. backend training/status 가 *전체 1 job* 만 추적이라
  // 다른 알고리즘 학습 중이면 본 카드 학습 disabled.
  const isThisAlgoTraining = !!(training?.is_running && training.algorithm_id === algo.vision_algorithm_id);
  const isOtherAlgoTraining = !!(training?.is_running && training.algorithm_id !== algo.vision_algorithm_id);
  const isDeprecated = algo.status === 'deprecated';
  const trainKind = trainingKindFor(algo);

  // Promote 가능 조건: 백엔드가 candidate_path 결정 (running job completed 또는 명시). UI 는 막지 않고 시도하게 둠.
  const promoteDisabled = busy || isThisAlgoTraining;
  // Rollback 은 history 있어야.
  const rollbackDisabled = busy || !hasHistory || isThisAlgoTraining;
  // 학습 시작 disabled — 진행 중이거나 deprecated.
  const trainDisabled = busy || isThisAlgoTraining || isOtherAlgoTraining || isDeprecated;

  async function handlePromote() {
    setBusy(true); setActionError(null);
    try {
      await Vision.promote(algo.vision_algorithm_id, { operator_id: 'operator' });
      setConfirm(null);
      onPromoted();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : tr('modelRegistry.errorPromote'));
    } finally {
      setBusy(false);
    }
  }

  async function handleRollback() {
    setBusy(true); setActionError(null);
    try {
      await Vision.rollback(algo.vision_algorithm_id, { operator_id: 'operator' });
      setConfirm(null);
      onRolledBack();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : tr('modelRegistry.errorRollback'));
    } finally {
      setBusy(false);
    }
  }

  // R12 — 학습 시작. lrcn-finetune 또는 vision-detector kind 자동 분기.
  async function handleTrain() {
    setBusy(true); setActionError(null);
    try {
      await Vision.trainingStart({
        kind: trainKind,
        algorithm_id: algo.vision_algorithm_id,
      });
      setConfirm(null);
      onPromoted(); // status 새로고침 (polling 도 있지만 즉시)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : tr('modelRegistry.errorTrain'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="p-4 bg-gray-800/60 border border-gray-700/50 rounded-lg space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-0.5">
          <div className="flex items-center gap-2 flex-wrap">
            <Cpu className="w-4 h-4 text-green-400 flex-shrink-0" />
            <span className="text-sm font-semibold text-white">{algo.display_name}</span>
            <span className={`px-2 py-0.5 rounded text-xs ${statusBadge(algo.status)}`}>
              {statusLabelKey(algo.status) ? tr(statusLabelKey(algo.status)!) : algo.status}
            </span>
          </div>
          <p className="text-xs text-gray-500 font-mono">{algo.vision_algorithm_id}</p>
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-x-3 gap-y-1.5 text-xs">
        {algo.species && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelSpecies')}</span> <span className="text-gray-200">{algo.species}</span></div>
        )}
        {algo.growth_stage && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelGrowthStage')}</span> <span className="text-gray-200">{algo.growth_stage}</span></div>
        )}
        {algo.tank_shape && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelTankShape')}</span> <span className="text-gray-200">{algo.tank_shape}</span></div>
        )}
        {algo.camera_position && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelCamera')}</span> <span className="text-gray-200">{algo.camera_position}</span></div>
        )}
        {algo.feeder_type && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelFeeder')}</span> <span className="text-gray-200">{algo.feeder_type}</span></div>
        )}
        {algo.size_range_g && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelWeight')}</span> <span className="text-gray-200 font-mono">{algo.size_range_g[0]}~{algo.size_range_g[1]}g</span></div>
        )}
        {algo.density_range && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelDensity')}</span> <span className="text-gray-200">{algo.density_range}</span></div>
        )}
        {algo.model?.runtime && (
          <div><span className="text-gray-500">{tr('modelRegistry.labelRuntime')}</span> <span className="text-gray-200">{algo.model.runtime}</span></div>
        )}
      </div>

      {algo.outputs && algo.outputs.length > 0 && (
        <div>
          <div className="text-xs text-gray-500 mb-1">{tr('modelRegistry.behaviorOutputs')}</div>
          <div className="flex flex-wrap gap-1.5">
            {algo.outputs.map(o => (
              <span key={o} className="px-2 py-0.5 text-xs rounded bg-gray-900/60 border border-gray-700/50 text-gray-300 font-mono">
                {o}
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Active state */}
      <div className="pt-2 border-t border-gray-700/30 space-y-2">
        <ActiveStateBlock state={activeState} />
        <HistoryBlock state={activeState} />
      </div>

      {/* Training progress (학습 데이터 탭과 동일한 게이트 — 즉시 promote 판단) */}
      {showTraining && training && (
        <div className="pt-2 border-t border-gray-700/30 space-y-2">
          <div className="text-xs text-gray-400">{tr('modelRegistry.trainingProgress')}</div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <MiniProgress
              label={tr('modelRegistry.labelDetectorBoxes')}
              current={training.bootstrap?.box_count ?? 0}
              required={training.bootstrap?.required_boxes ?? 0}
              ready={!!training.bootstrap?.can_start_training}
            />
            <MiniProgress
              label={tr('modelRegistry.labelUniqueFrames')}
              current={training.bootstrap?.frame_count ?? 0}
              required={training.bootstrap?.required_frames ?? 0}
              ready={!!training.bootstrap?.can_start_training}
            />
            <MiniProgress
              label={tr('modelRegistry.labelDisputeLabels')}
              current={training.dispute?.label_count ?? 0}
              required={training.dispute?.required ?? 0}
              ready={!!training.dispute?.can_validate}
            />
          </div>
          {training.is_running && (
            <div className="text-xs text-amber-300">
              {tr('modelRegistry.trainingRunningNote')}
            </div>
          )}
        </div>
      )}

      {/* Applied tanks */}
      <div className="pt-2 border-t border-gray-700/30">
        <div className="text-xs text-gray-500 mb-1.5">
          {tr('modelRegistry.appliedTanks')} ({applied.length})
        </div>
        {applied.length === 0 ? (
          <p className="text-xs text-gray-600 italic">{tr('modelRegistry.noAppliedTanks')}</p>
        ) : (
          <ul className="space-y-1">
            {applied.map(a => (
              <li key={`${a.tank_id}_${a.camera_id}`} className="flex items-center gap-2 text-xs">
                <Layers className="w-3 h-3 text-gray-500" />
                <span className="text-gray-300 font-mono">{a.tank_id}</span>
                <span className="text-gray-500">·</span>
                <span className="text-gray-400 font-mono">{a.camera_id}</span>
                {a.current_growth_stage && (
                  <span className="text-gray-500 ml-1">({a.current_growth_stage}, {a.current_avg_weight_g ?? '?'}g)</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* R12 — 카드 학습 진행 stripe */}
      {isThisAlgoTraining && (
        <div className="px-2.5 py-1.5 bg-amber-900/20 border border-amber-500/30 rounded text-xs">
          <div className="flex items-baseline justify-between gap-2 mb-1">
            <span className="text-amber-200 font-medium">
              🎓 {tr('modelRegistry.trainingInProgress')} — {training?.current_job?.stage_label ?? tr('modelRegistry.stageReady')}
            </span>
            <span className="text-amber-300 font-mono">
              {Math.round(training?.current_job?.progress_pct ?? 0)}%
            </span>
          </div>
          <div className="h-1 w-full bg-gray-800 rounded-full overflow-hidden">
            <div
              className="h-full bg-amber-500 transition-all"
              style={{ width: `${Math.round(training?.current_job?.progress_pct ?? 0)}%` }}
            />
          </div>
        </div>
      )}

      {/* Actions — R12: 학습 시작 + promote + rollback */}
      <div className="pt-2 border-t border-gray-700/30 flex flex-wrap items-center gap-2">
        <button
          onClick={() => { setActionError(null); setConfirm('train'); }}
          disabled={trainDisabled}
          className="px-3 py-1.5 text-xs rounded font-medium bg-amber-600 hover:bg-amber-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
          title={
            isDeprecated
              ? tr('modelRegistry.titleTrainDeprecated')
              : isOtherAlgoTraining
                ? tr('modelRegistry.titleTrainOtherRunning')
                : isThisAlgoTraining
                  ? tr('modelRegistry.titleTrainAlreadyRunning')
                  : `${tr('modelRegistry.titleTrainStart')} (kind=${trainKind})`
          }
        >
          🎓 {tr('modelRegistry.trainStart')} ({trainKind === 'lrcn-finetune' ? 'LRCN' : 'YOLO'})
        </button>
        <button
          onClick={() => { setActionError(null); setConfirm('promote'); }}
          disabled={promoteDisabled}
          className="px-3 py-1.5 text-xs rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 disabled:cursor-not-allowed text-white transition-colors"
          title={hasActive ? tr('modelRegistry.titlePromoteOverwrite') : tr('modelRegistry.titlePromoteFirst')}
        >
          {tr('modelRegistry.btnPromote')}
        </button>
        <button
          onClick={() => { setActionError(null); setConfirm('rollback'); }}
          disabled={rollbackDisabled}
          className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 disabled:text-gray-600 disabled:cursor-not-allowed text-gray-200 transition-colors flex items-center gap-1.5"
          title={hasHistory ? tr('modelRegistry.titleRollbackHas') : tr('modelRegistry.titleRollbackNone')}
        >
          <RotateCcw className="w-3 h-3" />
          {tr('modelRegistry.btnRollback')}
        </button>
        {actionError && (
          <span className="text-xs text-red-400 font-mono ml-2 truncate" title={actionError}>
            {actionError}
          </span>
        )}
      </div>

      <ConfirmDialog
        open={confirm === 'promote'}
        title={tr('modelRegistry.confirmPromoteTitle')}
        message={
          <div className="space-y-2">
            <p>{tr('modelRegistry.confirmPromoteBody')}</p>
            <p className="text-xs text-gray-400">
              {tr('modelRegistry.confirmPromoteNote1')}
              <br />{tr('modelRegistry.confirmPromoteNote2')}
              <br />{tr('modelRegistry.confirmPromoteNote3')}
            </p>
            {actionError && (
              <div className="mt-2 px-2.5 py-2 bg-red-900/30 border border-red-500/50 rounded text-xs text-red-300 font-mono break-words">
                {actionError}
              </div>
            )}
          </div>
        }
        confirmLabel={tr('modelRegistry.confirmLabelApply')}
        destructive={false}
        busy={busy}
        onConfirm={handlePromote}
        onCancel={() => { setConfirm(null); setActionError(null); }}
      />
      <ConfirmDialog
        open={confirm === 'rollback'}
        title={tr('modelRegistry.confirmRollbackTitle')}
        message={
          <div className="space-y-2">
            <p>{tr('modelRegistry.confirmRollbackBody')}</p>
            <p className="text-xs text-gray-400">
              {tr('modelRegistry.confirmRollbackCurrentLabel')}: <span className="font-mono text-gray-300">{activeState?.active_weights_path || tr('modelRegistry.none')}</span>
            </p>
            {actionError && (
              <div className="mt-2 px-2.5 py-2 bg-red-900/30 border border-red-500/50 rounded text-xs text-red-300 font-mono break-words">
                {actionError}
              </div>
            )}
          </div>
        }
        confirmLabel={tr('modelRegistry.confirmLabelRollback')}
        destructive
        busy={busy}
        onConfirm={handleRollback}
        onCancel={() => { setConfirm(null); setActionError(null); }}
      />
      {/* R12 — 학습 시작 ConfirmDialog */}
      <ConfirmDialog
        open={confirm === 'train'}
        title={tr('modelRegistry.confirmTrainTitle')}
        message={
          <div className="space-y-2">
            <p>{tr('modelRegistry.confirmTrainBody')} (kind = <span className="font-mono text-amber-300">{trainKind}</span>).</p>
            <p className="text-xs text-gray-400">
              {tr('modelRegistry.confirmTrainNote1')}
              <br />{tr('modelRegistry.confirmTrainNote2')}
              <br />{tr('modelRegistry.confirmTrainNote3')}
              {trainKind === 'lrcn-finetune' && (
                <>
                  <br />{tr('modelRegistry.confirmTrainNoteLrcn')}
                </>
              )}
            </p>
            {actionError && (
              <div className="mt-2 px-2.5 py-2 bg-red-900/30 border border-red-500/50 rounded text-xs text-red-300 font-mono break-words">
                {actionError}
              </div>
            )}
          </div>
        }
        confirmLabel={tr('modelRegistry.confirmLabelStart')}
        destructive={false}
        busy={busy}
        onConfirm={handleTrain}
        onCancel={() => { setConfirm(null); setActionError(null); }}
      />
    </div>
  );
}

export function ModelRegistry() {
  const { tr } = useLanguage();
  const [algorithms, setAlgorithms] = useState<VisionAlgorithm[]>([]);
  const [applications, setApplications] = useState<VisionTankApplication[]>([]);
  const [libraryVersion, setLibraryVersion] = useState<number | null>(null);
  const [states, setStates] = useState<Record<string, VisionAlgorithmActiveState>>({});
  const [training, setTraining] = useState<VisionTrainingStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastLoadedAt, setLastLoadedAt] = useState<string | null>(null);

  const load = useCallback(async (silent: boolean) => {
    if (!silent) setLoading(true);
    try {
      const [algoRes, appRes, trainRes] = await Promise.all([
        Vision.algorithms(),
        Vision.tankApplications(),
        Vision.trainingStatus().catch(() => null),
      ]);
      setAlgorithms(algoRes.items);
      setLibraryVersion(algoRes.algorithm_library_version);
      setApplications(appRes.items);
      setTraining(trainRes);

      // 알고리즘별 manifest state 병렬 조회 — 실패 항목은 무시 (zero state 유지).
      const stateEntries = await Promise.all(
        algoRes.items.map(async (a) => {
          try {
            const st = await Vision.algorithmState(a.vision_algorithm_id);
            return [a.vision_algorithm_id, st] as const;
          } catch {
            return [a.vision_algorithm_id, { active_weights_path: '' }] as const;
          }
        }),
      );
      setStates(Object.fromEntries(stateEntries));
      setLastLoadedAt(new Date().toISOString());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : tr('modelRegistry.errorLoad'));
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(false);
    const id = window.setInterval(() => { void load(true); }, POLL_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [load]);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2">
          <Cpu className="w-4 h-4 text-green-400" />
          {tr('modelRegistry.cardTitle')}
        </CardTitle>
        <div className="flex items-center gap-3 text-xs text-gray-500 font-mono">
          {libraryVersion !== null && (
            <span>{tr('modelRegistry.libraryVersion', { version: libraryVersion, count: algorithms.length })}</span>
          )}
          {lastLoadedAt && (
            <span title={lastLoadedAt}>{tr('modelRegistry.updatedAt', { time: relativeTime(lastLoadedAt) })}</span>
          )}
          <button
            onClick={() => void load(false)}
            className="px-2 py-0.5 rounded border border-gray-700/50 hover:bg-gray-800 text-gray-300"
          >
            {tr('modelRegistry.btnRefresh')}
          </button>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-xs text-gray-500">
          {tr('modelRegistry.description')}
          ({' '}<span className="font-mono text-gray-400">local-ai/models/active/manifest.json</span>) {tr('modelRegistry.descriptionSuffix')}
        </p>

        {loading && (
          <div className="space-y-3">
            <Skeleton className="h-40 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}

        {error && (
          <div className="px-3 py-2 bg-destructive/10 border border-destructive/30 rounded text-sm text-destructive font-mono">
            {error}
          </div>
        )}

        {!loading && !error && algorithms.length === 0 && (
          <p className="text-sm text-gray-500 py-6 text-center">
            {tr('modelRegistry.emptyAlgorithms')}
          </p>
        )}

        {!loading && !error && algorithms.length > 0 && (
          <div className="space-y-3">
            {algorithms.map(algo => (
              <AlgorithmCard
                key={algo.vision_algorithm_id}
                algo={algo}
                applications={applications}
                activeState={states[algo.vision_algorithm_id] ?? null}
                training={training}
                onPromoted={() => void load(true)}
                onRolledBack={() => void load(true)}
              />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
