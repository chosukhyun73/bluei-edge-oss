import { useEffect, useState } from 'react';
import { dateLocale } from '../../lib/format';
import { GraduationCap, Database, AlertCircle, Lightbulb } from 'lucide-react';
import { Vision } from '../../lib/api';
import { useLanguage } from '../../lib/language-core';
import type {
  VisionTrainingStatus, VisionTrainingJob, VisionBootstrapLabel,
} from '../../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../ui/card';
import { Skeleton } from '../ui/skeleton';

// NeedsNow — 커버리지 숫자를 "지금 무엇을 몇 장 해야 하는가" 행동 지침으로 번역.
// 운영자가 표를 해석하지 않아도 다음 라벨링 행동을 알 수 있게 한다 (2026-07-17).
function NeedsNow({ status }: { status: VisionTrainingStatus }) {
  const { tr } = useLanguage();
  const cov = status.coverage;
  if (!cov) return null;

  const items: { key: string; text: string }[] = [];
  const target = cov.target_frames_per_class;

  // 1. 클래스별 부족분 (부족한 순, 상위 3개) — 같은 장면 몰림이면 "새 장면으로"
  // 안내를 붙인다. 나머지는 한 줄 요약 (표에서 확인).
  const deficits = cov.classes
    .filter((c) => c.frames < target)
    .sort((a, b) => a.frames - b.frames);
  for (const c of deficits.slice(0, 3)) {
    let text = tr('learningDataCollector.needClassFrames', {
      cls: tr(`learningDataCollector.class.${c.class}`),
      need: target - c.frames,
      frames: c.frames,
    });
    if (c.boxes >= 50 && c.top5_box_share > 0.5) {
      text += tr('learningDataCollector.needClassSameScene', { boxes: c.boxes });
    }
    items.push({ key: `cls-${c.class}`, text });
  }
  if (deficits.length > 3) {
    items.push({
      key: 'cls-rest',
      text: tr('learningDataCollector.needClassRest', { count: deficits.length - 3 }),
    });
  }

  // 2. 전체 프레임 목표 (재학습 권장 150).
  const frameCount = status.bootstrap.frame_count;
  if (frameCount < cov.recommended_frames) {
    items.push({
      key: 'total',
      text: tr('learningDataCollector.needFramesTotal', {
        need: cov.recommended_frames - frameCount,
        cur: frameCount,
        target: cov.recommended_frames,
      }),
    });
  }

  // 3. 시간대 편중 — 주간 3구간 중 크게 빈 구간이 있으면 안내.
  const tod = cov.time_of_day;
  const day3: [string, number][] = (['morning', 'day', 'evening'] as const).map((k) => [k, tod[k] ?? 0]);
  const daySum = day3.reduce((s, [, n]) => s + n, 0);
  if (daySum >= 10) {
    const [minKey, minN] = day3.reduce((a, b) => (b[1] < a[1] ? b : a));
    if (minN < daySum * 0.2) {
      items.push({
        key: 'tod',
        text: tr('learningDataCollector.needTimeOfDay', {
          bucket: tr(`learningDataCollector.tod.${minKey}`),
          count: minN,
        }),
      });
    }
  }

  // 4. LRCN 세그먼트 게이트 (별도 트랙).
  if (status.segments && status.segments.valid_rows < status.segments.gate) {
    items.push({
      key: 'segments',
      text: tr('learningDataCollector.needSegments', {
        need: status.segments.gate - status.segments.valid_rows,
        valid: status.segments.valid_rows,
        gate: status.segments.gate,
      }),
    });
  }

  return (
    <div className="p-2.5 bg-amber-500/5 border border-amber-500/20 rounded space-y-1.5">
      <div className="flex items-center gap-1.5">
        <Lightbulb className="w-3.5 h-3.5 text-amber-300" />
        <span className="text-xs font-medium text-amber-200">{tr('learningDataCollector.needNowTitle')}</span>
      </div>
      {items.length === 0 ? (
        <div className="text-xs text-green-300">{tr('learningDataCollector.needAllGood')}</div>
      ) : (
        <ul className="space-y-1 text-xs text-gray-300">
          {items.map((it, i) => (
            <li key={it.key} className="flex gap-1.5">
              <span className="text-amber-300/70 font-mono shrink-0">{i + 1}.</span>
              <span>{it.text}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="text-[11px] text-gray-500">{tr('learningDataCollector.needWhere')}</div>
    </div>
  );
}

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
          {/* status 는 완료 후에도 'progress' 로 남는다 — stage_label 이 실제 상태 (7-16 실측). */}
          {(j.stage_label || j.status) && (
            <span className="text-gray-500">· {j.stage_label ?? j.status}</span>
          )}
          {typeof j.metrics?.map50 === 'number' && (
            <span className="text-gray-400 font-mono">· mAP50 {j.metrics.map50.toFixed(3)}</span>
          )}
          {typeof j.progress_pct === 'number' && j.progress_pct < 100 && (
            <span className="text-gray-400 font-mono">· {j.progress_pct.toFixed(0)}%</span>
          )}
          {(j.updated_at ?? j.started_at ?? (j.recorded_at as string | undefined)) && (
            <span className="text-gray-600 ml-auto">
              {new Date((j.updated_at ?? j.started_at ?? j.recorded_at) as string).toLocaleString(dateLocale())}
            </span>
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
            <span className="text-gray-600 ml-auto">{new Date(l.recorded_at).toLocaleString(dateLocale())}</span>
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
        // 이벤트 래퍼 평탄화 — 한 job 의 update 이벤트가 여러 건이라 최신(목록 앞)만.
        const seen = new Set<string>();
        const jobRows: VisionTrainingJob[] = [];
        for (const it of j.items) {
          const p = { ...it.payload, recorded_at: it.recorded_at };
          if (!p.job_id || seen.has(p.job_id)) continue;
          seen.add(p.job_id);
          jobRows.push(p);
        }
        setJobs(jobRows);
        setLabels(l.items.map((it) => ({
          ...it.payload,
          recorded_at: it.recorded_at,
          box_count: Array.isArray(it.payload.boxes) ? it.payload.boxes.length : undefined,
        })));
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
                hint={status.bootstrap.hint ? tr('learningDataCollector.bootstrapHintText') : undefined}
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
                hint={status.dispute.hint ? tr('learningDataCollector.disputeHintText') : undefined}
              />
              <div className="md:col-span-3 text-[11px] text-gray-500">
                {tr('learningDataCollector.minGateCaption')}
              </div>
            </div>

            {status.coverage && (
              <div className="p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg space-y-2.5">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium text-gray-200">{tr('learningDataCollector.coverageTitle')}</span>
                  <span className="text-xs">
                    <span className="text-gray-400">{tr('learningDataCollector.coverageFramesTarget')} </span>
                    <span className={status.bootstrap.frame_count >= status.coverage.recommended_frames ? 'text-green-300' : 'text-amber-300'}>
                      {status.bootstrap.frame_count} / {status.coverage.recommended_frames}
                    </span>
                  </span>
                </div>
                <NeedsNow status={status} />
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-gray-500">
                      <th className="text-left font-normal pb-1">{tr('learningDataCollector.covClass')}</th>
                      <th className="text-right font-normal pb-1">{tr('learningDataCollector.covBoxes')}</th>
                      <th className="text-right font-normal pb-1">
                        {tr('learningDataCollector.covFrames')} (≥{status.coverage.target_frames_per_class})
                      </th>
                      <th className="text-right font-normal pb-1">{tr('learningDataCollector.covConcentration')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {status.coverage.classes.map((c) => (
                      <tr key={c.class} className="border-t border-gray-800/60">
                        <td className="py-1 text-gray-300">{tr(`learningDataCollector.class.${c.class}`)}</td>
                        <td className="py-1 text-right font-mono text-gray-300">{c.boxes.toLocaleString()}</td>
                        <td className={`py-1 text-right font-mono ${c.boxes === 0 ? 'text-gray-500' : c.frames >= status.coverage!.target_frames_per_class ? 'text-green-300' : 'text-amber-300'}`}>
                          {c.frames}
                        </td>
                        <td className={`py-1 text-right font-mono ${c.boxes > 0 && c.top5_box_share > 0.5 ? 'text-amber-300' : 'text-gray-400'}`}>
                          {c.boxes > 0 ? `${(c.top5_box_share * 100).toFixed(0)}%` : '—'}
                          {c.boxes > 0 && c.top5_box_share > 0.5 && (
                            <span className="ml-1 text-[10px]">{tr('learningDataCollector.covSameScene')}</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-400">
                  <span>
                    {tr('learningDataCollector.covTimeOfDay')}{' '}
                    {(['morning', 'day', 'evening', 'night'] as const).map((k, i) => (
                      <span key={k}>
                        {i > 0 && ' · '}
                        {tr(`learningDataCollector.tod.${k}`)}{' '}
                        <span className="font-mono text-gray-300">{status.coverage!.time_of_day[k] ?? 0}</span>
                      </span>
                    ))}
                  </span>
                  <span>
                    {tr('learningDataCollector.covLastRun')}{' '}
                    {status.coverage.last_run ? (
                      typeof status.coverage.last_run.metrics?.map50 === 'number' ? (
                        <span className="font-mono text-gray-200">
                          mAP50 {(status.coverage.last_run.metrics.map50 as number).toFixed(3)}
                          <span className="text-gray-500"> · {status.coverage.last_run.completed_at?.slice(5, 10)}</span>
                        </span>
                      ) : (
                        <span className="text-gray-500">{tr('learningDataCollector.covLastRunNoMetric')}</span>
                      )
                    ) : (
                      <span className="text-gray-500">—</span>
                    )}
                  </span>
                </div>
                {status.segments && (
                  <div className="pt-1 border-t border-gray-800/60 text-xs text-gray-400">
                    {tr('learningDataCollector.segmentsGateLabel')}{' '}
                    <span className={`font-mono ${status.segments.valid_rows >= status.segments.gate ? 'text-green-300' : 'text-gray-200'}`}>
                      {status.segments.valid_rows.toLocaleString()} / {status.segments.gate.toLocaleString()}
                    </span>
                    <span className="text-gray-500"> ({tr('learningDataCollector.segmentsExcluded')} {status.segments.excluded})</span>
                  </div>
                )}
              </div>
            )}

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
                <span className="leading-relaxed">{tr('aiTraining.safetyNote')}</span>
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
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 3xl:grid-cols-5 gap-3">
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
