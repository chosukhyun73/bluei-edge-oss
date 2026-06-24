import { useEffect, useState } from 'react';
import { GraduationCap, Database, AlertCircle } from 'lucide-react';
import { Vision } from '../../lib/api';
import { useLanguage } from '../../lib/language-context';
import type {
  VisionTrainingStatus, VisionTrainingJob, VisionBootstrapLabel,
} from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';

function Progress({
  label, current, required, hint,
}: {
  label: string;
  current: number;
  required: number;
  hint?: string;
}) {
  const { tr } = useLanguage();
  const pct = required > 0 ? Math.min(100, (current / required) * 100) : 0;
  const ready = current >= required;
  const barColor = ready ? 'bg-green-500' : pct >= 50 ? 'bg-amber-500' : 'bg-gray-500';
  return (
    <div className="space-y-1.5">
      <div className="flex items-baseline justify-between">
        <span className="text-sm text-gray-300">{label}</span>
        <span className="text-sm font-mono">
          <span className={ready ? 'text-green-300' : 'text-gray-200'}>{current.toLocaleString()}</span>
          <span className="text-gray-500"> / {required.toLocaleString()}</span>
          {ready && <span className="ml-1.5 text-green-400 text-xs">✓ {tr('learningDataCollector.satisfied')}</span>}
        </span>
      </div>
      <div className="h-1.5 w-full bg-gray-700/50 rounded-full overflow-hidden">
        <div className={`h-full ${barColor} transition-all`} style={{ width: `${pct}%` }} />
      </div>
      {hint && <p className="text-xs text-gray-500 leading-relaxed">{hint}</p>}
    </div>
  );
}

function StatusBanner({ status }: { status: VisionTrainingStatus }) {
  const { tr } = useLanguage();
  const activePath = status.active_weights.active_weights_path;
  return (
    <div className="p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg space-y-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-sm text-gray-300">
          {tr('learningDataCollector.currentAlgorithm')} <span className="text-white font-medium">{status.algorithm_display}</span>
        </span>
        <span className="text-xs text-gray-500 font-mono">{status.algorithm_id}</span>
      </div>
      <div className="flex items-center gap-3 text-xs">
        <span>
          <span className="text-gray-500">{tr('learningDataCollector.statusLabel')}</span>{' '}
          <span className={status.is_running ? 'text-amber-300' : 'text-gray-300'}>
            {status.is_running ? tr('learningDataCollector.statusRunning') : tr('learningDataCollector.statusIdle')}
          </span>
        </span>
        <span className="text-gray-600">·</span>
        <span>
          <span className="text-gray-500">{tr('learningDataCollector.activeWeights')}</span>{' '}
          <span className="text-gray-200 font-mono">{activePath || tr('learningDataCollector.noWeightsApplied')}</span>
        </span>
      </div>
    </div>
  );
}

function JobsList({ jobs, currentJob }: {
  jobs: VisionTrainingJob[];
  currentJob: VisionTrainingJob | null;
}) {
  const { tr } = useLanguage();
  const all = currentJob ? [currentJob, ...jobs.filter(j => j.job_id !== currentJob.job_id)] : jobs;
  if (all.length === 0) {
    return (
      <p className="text-xs text-gray-600 italic py-2">{tr('learningDataCollector.noJobHistory')}</p>
    );
  }
  return (
    <ul className="space-y-1.5">
      {all.slice(0, 5).map(j => (
        <li key={j.job_id} className="flex items-center gap-2 text-xs">
          <span className="text-gray-300 font-mono">{j.job_id}</span>
          {j.status && (
            <span className="text-gray-500">· {j.status}</span>
          )}
          {typeof j.progress === 'number' && (
            <span className="text-gray-400 font-mono">· {(j.progress * 100).toFixed(0)}%</span>
          )}
          {j.started_at && (
            <span className="text-gray-600 ml-auto">{new Date(j.started_at).toLocaleString('ko-KR')}</span>
          )}
        </li>
      ))}
    </ul>
  );
}

function LabelsList({ labels }: { labels: VisionBootstrapLabel[] }) {
  const { tr } = useLanguage();
  if (labels.length === 0) {
    return (
      <p className="text-xs text-gray-600 italic py-2">{tr('learningDataCollector.noLabelsYet')}</p>
    );
  }
  return (
    <ul className="space-y-1">
      {labels.slice(0, 6).map((l, i) => (
        <li key={l.label_id ?? i} className="flex items-center gap-2 text-xs">
          <span className="text-gray-300 font-mono">{l.camera_id ?? '?'}</span>
          {l.tank_id && <span className="text-gray-500">· {l.tank_id}</span>}
          {typeof l.box_count === 'number' && (
            <span className="text-gray-400 font-mono">· {tr('learningDataCollector.boxCount')} {l.box_count}</span>
          )}
          {l.recorded_at && (
            <span className="text-gray-600 ml-auto">{new Date(l.recorded_at).toLocaleString('ko-KR')}</span>
          )}
        </li>
      ))}
    </ul>
  );
}

export function LearningDataCollector() {
  const { tr } = useLanguage();
  const [status, setStatus] = useState<VisionTrainingStatus | null>(null);
  const [jobs, setJobs] = useState<VisionTrainingJob[]>([]);
  const [labels, setLabels] = useState<VisionBootstrapLabel[]>([]);
  const [totalBoxes, setTotalBoxes] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const [s, j, l] = await Promise.all([
          Vision.trainingStatus(),
          Vision.trainingJobs(),
          Vision.bootstrapLabels(),
        ]);
        if (cancelled) return;
        setStatus(s);
        setJobs(j.items);
        setLabels(l.items);
        setTotalBoxes(l.total_boxes);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : tr('learningDataCollector.loadFailed'));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => { cancelled = true; };
  }, []);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <GraduationCap className="w-4 h-4 text-green-400" />
          {tr('learningDataCollector.cardTitle')}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        {loading && (
          <div className="space-y-3">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-32 w-full" />
            <Skeleton className="h-32 w-full" />
          </div>
        )}

        {error && (
          <div className="px-3 py-2 bg-destructive/10 border border-destructive/30 rounded text-sm text-destructive font-mono">
            {error}
          </div>
        )}

        {!loading && !error && status && (
          <>
            <StatusBanner status={status} />

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4 p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
              <Progress
                label={tr('learningDataCollector.progressBoxLabel')}
                current={status.bootstrap.box_count}
                required={status.bootstrap.required_boxes}
                hint={status.bootstrap.hint}
              />
              <Progress
                label={tr('learningDataCollector.progressFrameLabel')}
                current={status.bootstrap.frame_count}
                required={status.bootstrap.required_frames}
              />
              <Progress
                label={tr('learningDataCollector.progressDisputeLabel')}
                current={status.dispute.label_count}
                required={status.dispute.required}
                hint={status.dispute.hint}
              />
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
                <div className="flex items-center gap-1.5 mb-2">
                  <Database className="w-3.5 h-3.5 text-gray-400" />
                  <span className="text-sm font-medium text-gray-200">{tr('learningDataCollector.trainingJobs')}</span>
                </div>
                <JobsList jobs={jobs} currentJob={status.current_job} />
              </div>
              <div className="p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-1.5">
                    <Database className="w-3.5 h-3.5 text-gray-400" />
                    <span className="text-sm font-medium text-gray-200">{tr('learningDataCollector.bootstrapLabels')}</span>
                  </div>
                  <span className="text-xs text-gray-500 font-mono">
                    {tr('learningDataCollector.totalBoxes')} {totalBoxes.toLocaleString()}
                  </span>
                </div>
                <LabelsList labels={labels} />
              </div>
            </div>

            {status.safety_note && (
              <div className="flex items-start gap-2 px-3 py-2 bg-blue-500/10 border border-blue-500/30 rounded text-xs text-blue-200">
                <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
                <span className="leading-relaxed">{status.safety_note}</span>
              </div>
            )}

            {/* ── 누적 학습 5 영역 — 본선엔 인프라/안내, 실 학습은 Phase β ── */}
            <div className="space-y-2 pt-2">
              <h4 className="text-sm font-semibold text-gray-200 flex items-center gap-2">
                <GraduationCap className="w-4 h-4 text-green-400" />
                {tr('learningDataCollector.cumulativeLearningTitle')}
              </h4>
              <p className="text-xs text-gray-500">
                {tr('learningDataCollector.cumulativeLearningDesc')}
              </p>
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
                <LearningAreaCard
                  num="①"
                  title={tr('learningDataCollector.area1Title')}
                  input={tr('learningDataCollector.area1Input')}
                  output={tr('learningDataCollector.area1Output')}
                  source="tank_weight_history + feed_cycles"
                />
                <LearningAreaCard
                  num="②"
                  title={tr('learningDataCollector.area2Title')}
                  input={tr('learningDataCollector.area2Input')}
                  output={tr('learningDataCollector.area2Output')}
                  source={tr('learningDataCollector.area2Source')}
                />
                <LearningAreaCard
                  num="③"
                  title={tr('learningDataCollector.area3Title')}
                  input={tr('learningDataCollector.area3Input')}
                  output={tr('learningDataCollector.area3Output')}
                  source={tr('learningDataCollector.area3Source')}
                />
                <LearningAreaCard
                  num="④"
                  title={tr('learningDataCollector.area4Title')}
                  input={tr('learningDataCollector.area4Input')}
                  output={tr('learningDataCollector.area4Output')}
                  source={tr('learningDataCollector.area4Source')}
                />
                <LearningAreaCard
                  num="⑤"
                  title={tr('learningDataCollector.area5Title')}
                  input={tr('learningDataCollector.area5Input')}
                  output={tr('learningDataCollector.area5Output')}
                  source="operator_intents + arbiter_decisions + dispute"
                />
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function LearningAreaCard({
  num, title, input, output, source,
}: {
  num: string;
  title: string;
  input: string;
  output: string;
  source: string;
}) {
  return (
    <div className="p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg space-y-1.5">
      <div className="flex items-baseline gap-2">
        <span className="text-green-400 font-mono">{num}</span>
        <span className="text-sm font-medium text-white">{title}</span>
      </div>
      <div className="text-xs space-y-0.5">
        <div><span className="text-gray-500">input</span> <span className="text-gray-300">{input}</span></div>
        <div><span className="text-gray-500">output</span> <span className="text-gray-300">{output}</span></div>
        <div><span className="text-gray-500">source</span> <span className="text-gray-400 font-mono">{source}</span></div>
      </div>
    </div>
  );
}
