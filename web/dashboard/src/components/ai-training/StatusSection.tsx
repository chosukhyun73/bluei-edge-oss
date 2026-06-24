import { useLanguage } from '../../lib/language-context';
import type { VisionTrainingStatus } from '../../lib/types';

type PhaseStatus = 'done' | 'active' | 'pending';

function PhaseStep({
  num, title, detail, status,
}: {
  num: string;
  title: string;
  detail: string;
  status: PhaseStatus;
}) {
  const dotColor = status === 'done'
    ? 'bg-green-500 text-white'
    : status === 'active'
      ? 'bg-amber-500 text-black'
      : 'bg-gray-700 text-gray-400';
  return (
    <div className="flex items-start gap-2.5">
      <div className={`w-7 h-7 rounded-full flex items-center justify-center text-sm font-bold flex-shrink-0 ${dotColor}`}>
        {num}
      </div>
      <div className="space-y-0.5">
        <b className="text-sm text-gray-200">{title}</b>
        <div className="text-xs text-gray-500">{detail}</div>
      </div>
    </div>
  );
}

/**
 * 5/9 ai-training.js 의 refreshAIState() + updatePhaseChecklist() 로직 정합.
 */
export function StatusSection({
  status, cameraOK,
}: {
  status: VisionTrainingStatus | null;
  cameraOK: boolean;
}) {
  const { tr } = useLanguage();
  if (!status) {
    return <div className="text-sm text-gray-400">{tr('statusSection.loading')}</div>;
  }

  const boot = status.bootstrap;
  const aw = status.active_weights;
  const job = status.current_job;

  // 5/9 refreshAIState 의 phase / capability / tag 결정 로직
  let phase: string;
  let capability: string;
  let tag: string;
  if (!boot.can_start_training) {
    const remainBoxes = Math.max(0, boot.required_boxes - boot.box_count);
    const remainFrames = Math.max(0, boot.required_frames - boot.frame_count);
    phase = `${tr('statusSection.phaseBootstrapPrefix')} — ${tr('aiStatus.phaseBootstrapNeed', { boxes: remainBoxes, frames: remainFrames })}`;
    capability = tr('statusSection.capabilityBootstrap');
    tag = tr('statusSection.tagBootstrap');
  } else if (status.is_running) {
    phase = tr('statusSection.phaseRunning');
    capability = tr('statusSection.capabilityRunning');
    tag = tr('statusSection.tagRunning');
  } else if (job?.status === 'completed') {
    phase = tr('statusSection.phaseCompleted');
    capability = tr('statusSection.capabilityCompleted');
    tag = tr('statusSection.tagCompleted');
  } else {
    phase = tr('statusSection.phaseReady');
    capability = tr('statusSection.capabilityReady');
    tag = tr('statusSection.tagReady');
  }

  // 4단계 phase status (updatePhaseChecklist)
  const phase1Status: PhaseStatus = cameraOK ? 'done' : 'active';
  const phase1Detail = cameraOK
    ? tr('statusSection.phase1DetailOk')
    : tr('statusSection.phase1DetailNotOk');

  const boxRatio =
    boot.box_count >= boot.required_boxes && boot.frame_count >= boot.required_frames;
  const phase2Status: PhaseStatus = boxRatio ? 'done' : cameraOK ? 'active' : 'pending';
  const phase2Detail = tr('aiStatus.progress', {
    boxes: boot.box_count,
    reqBoxes: boot.required_boxes,
    frames: boot.frame_count,
    reqFrames: boot.required_frames,
  });

  let phase3Status: PhaseStatus;
  let phase3Detail: string;
  if (job?.status === 'completed') {
    phase3Status = 'done';
    phase3Detail = `${tr('statusSection.phase3DetailCompleted')} (${(job.updated_at as string | undefined)?.slice(0, 16) ?? ''})`;
  } else if (job?.status === 'failed') {
    phase3Status = 'active';
    phase3Detail = tr('statusSection.phase3DetailFailed');
  } else if (job?.status === 'started' || job?.status === 'progress') {
    phase3Status = 'active';
    phase3Detail = `${tr('statusSection.phase3DetailRunning')} (${Math.round(job.progress_pct ?? 0)}%)`;
  } else {
    phase3Status = boxRatio ? 'active' : 'pending';
    phase3Detail = tr('statusSection.phase3DetailNone');
  }

  let phase4Status: PhaseStatus;
  let phase4Detail: string;
  if (aw.active_weights_path) {
    phase4Status = 'done';
    phase4Detail = `${tr('statusSection.phase4DetailAppliedAt')}: ${((aw.applied_at as string | undefined) ?? '').slice(0, 16)}`;
  } else {
    phase4Status = job?.status === 'completed' ? 'active' : 'pending';
    phase4Detail = tr('statusSection.phase4DetailNotApplied');
  }

  return (
    <div className="space-y-4">
      {/* "지금 AI 는 어디까지 보고 있나요?" 헤더 */}
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-base font-semibold text-gray-100">{tr('statusSection.heading')}</h3>
        <span className="px-2 py-0.5 rounded text-xs bg-amber-500/20 text-amber-300 border border-amber-500/40">
          {tag}
        </span>
      </div>

      {/* 4단계 체크리스트 */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-3 p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
        <PhaseStep num="①" title={tr('statusSection.phase1Title')} detail={phase1Detail} status={phase1Status} />
        <PhaseStep num="②" title={tr('statusSection.phase2Title')} detail={phase2Detail} status={phase2Status} />
        <PhaseStep num="③" title={tr('statusSection.phase3Title')} detail={phase3Detail} status={phase3Status} />
        <PhaseStep num="④" title={tr('statusSection.phase4Title')} detail={phase4Detail} status={phase4Status} />
      </div>

      {/* Summary list */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-1.5 text-xs">
        <div className="flex justify-between gap-2">
          <b className="text-gray-400">{tr('statusSection.labelAlgorithm')}</b>
          <span className="text-gray-200">{status.algorithm_display || status.algorithm_id || tr('statusSection.algorithmNone')}</span>
        </div>
        <div className="flex justify-between gap-2">
          <b className="text-gray-400">{tr('statusSection.labelWeights')}</b>
          <span className={aw.active_weights_path ? 'text-gray-200 font-mono truncate' : 'text-gray-500'}>
            {aw.active_weights_path
              ? `${aw.active_weights_path} (job ${(aw.active_job_id as string | undefined) ?? '-'})`
              : tr('statusSection.weightsNone')}
          </span>
        </div>
        <div className="flex justify-between gap-2">
          <b className="text-gray-400">{tr('statusSection.labelAppliedAt')}</b>
          <span className={aw.applied_at ? 'text-gray-200' : 'text-gray-500'}>
            {(aw.applied_at as string | undefined) ?? '-'}
          </span>
        </div>
        <div className="flex justify-between gap-2">
          <b className="text-gray-400">{tr('statusSection.labelPhase')}</b>
          <span className="text-gray-200">{phase}</span>
        </div>
        <div className="flex justify-between gap-2 md:col-span-2">
          <b className="text-gray-400">{tr('statusSection.labelCapability')}</b>
          <span className="text-gray-200">{capability}</span>
        </div>
      </div>

      {/* 첫날 안내 (bootstrap 미충족 시만) */}
      {!boot.can_start_training && (
        <div className="p-3 bg-blue-900/20 border border-blue-500/30 rounded text-xs space-y-1.5">
          <p className="text-sm text-blue-200 font-semibold">📌 {tr('statusSection.bootstrapTitle')}</p>
          <p className="text-gray-300">{tr('statusSection.bootstrapIntro')}</p>
          <ul className="list-disc list-inside text-gray-300 space-y-0.5 pl-1">
            <li>
              <b>{tr('aiStatus.bootstrapItem1Bold')}</b>{tr('aiStatus.bootstrapItem1Post')}
            </li>
            <li>
              <b>{tr('aiStatus.bootstrapItem2Bold1')}</b>{tr('aiStatus.bootstrapItem2Mid')}<b>{tr('aiStatus.bootstrapItem2Bold2')}</b>
            </li>
          </ul>
          <p className="text-gray-300">
            <b>{tr('aiStatus.bootstrapWhyBold')}</b>{tr('aiStatus.bootstrapWhyPost')}
          </p>
          <p className="text-gray-500">
            {tr('statusSection.bootstrapHint')}
          </p>
        </div>
      )}
    </div>
  );
}
