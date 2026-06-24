import { useCallback, useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../lib/language-context';
import { Vision } from '../../lib/api';
import type {
  VisionTrainingStatus,
  VisionTrainingPoolItem,
  VisionTrainingPoolPhase,
} from '../../lib/types';

const BASE = ''; // vite proxy / Tauri 가 host 결정

// R11 — phase 별 LRCN 모델 카드 매핑. 운영자는 phase 만 의식, algorithm_id 는 backend 자동.
const PHASE_TO_ALGORITHM_ID: Record<'feeding' | 'baseline', string> = {
  feeding: 'vision_lrcn_blueocean_reaction_v1',
  baseline: 'vision_lrcn_blueocean_stability_v1',
};

/**
 * R7 — LRCN 지도학습 라벨링 (한 영상 / 한 phase / 한 슬라이더).
 *
 * 사용자 정정 (2026-05-21):
 *   - R5: 한 영상의 시간 분할 (앞 3.5초/뒤 3.5초) — *영상 캡처 모델과 어긋남* (잘못)
 *   - R6: 두 풀 동시 표시 (PoolCard × 2) — *화면 부담, 한 번에 한 영상이 맞음* (과도)
 *   - R7: 한 영상 + phase 따라 슬라이더 1개 + 두 호출 버튼 + 건너뛰기 ✅
 *
 * 흐름:
 *   1. 운영자가 [📥 사료 반응] 또는 [📥 안정성] 버튼 클릭 → 해당 풀에서 랜덤 1건 영상 로딩
 *   2. 영상 보고 phase 에 맞는 슬라이더 (사료 반응 OR 안정성) 로 점수 매김
 *   3. [💾 저장 + 새 영상] → 같은 phase 의 다음 영상
 *      또는 [⏭ 건너뛰기] → 점수 없이 같은 phase 다음 영상
 *      또는 다른 phase 버튼 → 풀 전환
 */
export function DisputeTab({
  status,
  onSubmitted,
}: {
  status: VisionTrainingStatus | null;
  onSubmitted: () => void;
}) {
  const { tr } = useLanguage();
  const videoRef = useRef<HTMLVideoElement>(null);

  // 현재 표시 중인 영상 + 풀 큐
  const [current, setCurrent] = useState<VisionTrainingPoolItem | null>(null);
  const [phase, setPhase] = useState<VisionTrainingPoolPhase | null>(null);
  const [queue, setQueue] = useState<VisionTrainingPoolItem[]>([]);
  const [queueIndex, setQueueIndex] = useState(0);

  // 평가
  const [score, setScore] = useState(0.5);
  const [memo, setMemo] = useState('');

  // R8: 판단 불가 격리
  const [excludeReason, setExcludeReason] = useState<'occlusion' | 'low_visibility' | 'other'>('occlusion');

  // 상태
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // phase 별 풀 size (마지막 조회 응답에서)
  const [poolStats, setPoolStats] = useState<Record<VisionTrainingPoolPhase, number>>({
    feeding: 0,
    baseline: 0,
  });

  // 영상 새로 표시될 때 슬라이더/메모 reset + video reload
  useEffect(() => {
    setScore(0.5);
    setMemo('');
    if (videoRef.current) {
      videoRef.current.currentTime = 0;
      videoRef.current.load();
    }
  }, [current?.payload.clip_id]);

  // 새 phase 풀에서 새 영상 1건 호출 (queue 새로 받음)
  const requestPhase = useCallback(async (p: VisionTrainingPoolPhase) => {
    setLoading(true);
    setError(null);
    try {
      const res = await Vision.trainingPool(p, 10);
      setPoolStats(prev => ({ ...prev, [p]: res.count }));
      if (res.items.length === 0) {
        setQueue([]);
        setQueueIndex(0);
        setCurrent(null);
        setPhase(p);
        return;
      }
      setQueue(res.items);
      setQueueIndex(0);
      setCurrent(res.items[0]);
      setPhase(p);
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('disputeTab.errorPoolLoad'));
    } finally {
      setLoading(false);
    }
  }, []);

  // 같은 phase 의 다음 영상 — queue 안에 있으면 인덱스 +1, 끝나면 새로 fetch
  const nextInQueue = useCallback(async () => {
    if (!phase) return;
    const nextIdx = queueIndex + 1;
    if (nextIdx < queue.length) {
      setQueueIndex(nextIdx);
      setCurrent(queue[nextIdx]);
      return;
    }
    // 큐 소진 — 같은 phase 새로 fetch
    await requestPhase(phase);
  }, [phase, queueIndex, queue, requestPhase]);

  // 저장 + 다음
  async function saveAndNext(verdict: 'correct' | 'wrong' | 'unsure') {
    if (!current || !phase) return;
    setSubmitting(true);
    setError(null);
    try {
      await fetch(
        `${BASE}/v1/vision/observations/${encodeURIComponent(current.payload.clip_id)}/disputes`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            camera_id: current.payload.camera_id,
            tank_id: current.payload.tank_id ?? '',
            operator_id: 'operator',
            verdict,
            reason: `[R7 ${phase}] ${memo}`,
            // phase 별 점수 분기
            ...(phase === 'feeding'
              ? { during_score: score }
              : { pre_score: score }),
            operator_score: score,
            clip_ref: current.payload.uri,
            // R11 — phase 별 LRCN 모델 카드로 자동 매핑
            algorithm_id: PHASE_TO_ALGORITHM_ID[phase],
            disputed_at: new Date().toISOString(),
          }),
        },
      );
      onSubmitted();
      await nextInQueue();
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('disputeTab.errorSave'));
    } finally {
      setSubmitting(false);
    }
  }

  // skip (점수 없음, 다음 영상)
  async function skip() {
    if (!current || !phase) return;
    await nextInQueue();
  }

  // R8: 판단 불가 격리 — 파일을 captures/excluded/<reason>/ 로 이동 + 풀에서 제외
  async function excludeClip() {
    if (!current) return;
    setSubmitting(true);
    setError(null);
    try {
      const res = await fetch(
        `${BASE}/v1/media/clips/${encodeURIComponent(current.payload.clip_id)}/exclude`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            reason: excludeReason,
            memo,
            operator_id: 'operator',
          }),
        },
      );
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body?.error?.message ?? `HTTP ${res.status}`);
      }
      onSubmitted();
      await nextInQueue();
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('disputeTab.errorExclude'));
    } finally {
      setSubmitting(false);
    }
  }

  // 메타
  const clipID = current?.payload.clip_id ?? '';
  const tankID = current?.payload.tank_id ?? '';
  const cameraID = current?.payload.camera_id ?? '';
  const startedAt = current?.payload.started_at ?? '';
  const cycleID = (current?.payload.evidence?.cycle_id as string | undefined) ?? '';
  const clipURL = clipID
    ? `${BASE}/v1/media/clips/${encodeURIComponent(clipID)}/play.mp4`
    : '';

  const phaseLabel = phase === 'feeding' ? tr('disputeTab.phaseFeeding') : phase === 'baseline' ? tr('disputeTab.phaseBaseline') : '-';
  const phaseColor = phase === 'feeding' ? 'amber' : phase === 'baseline' ? 'blue' : 'gray';
  const phaseDesc =
    phase === 'feeding'
      ? tr('disputeTab.phaseDescFeeding')
      : phase === 'baseline'
        ? tr('disputeTab.phaseDescBaseline')
        : tr('disputeTab.phaseDescNone');
  const scoreLowLabel = phase === 'feeding'
    ? tr('disputeTab.scoreLowFeeding')
    : tr('disputeTab.scoreLowBaseline');
  const scoreHighLabel = phase === 'feeding'
    ? tr('disputeTab.scoreHighFeeding')
    : tr('disputeTab.scoreHighBaseline');

  // dispute 라벨 진척 progress bar
  const dispLabel = `${status?.dispute.label_count ?? 0} / ${status?.dispute.required ?? 30}`;
  const dispPct = status
    ? Math.min(100, (status.dispute.label_count / status.dispute.required) * 100)
    : 0;

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <h3 className="text-sm font-semibold text-gray-100">
          {tr('disputeTab.headingTitle')}
        </h3>
        <p className="text-xs text-gray-500">
          {tr('disputeTab.headingDesc')}
        </p>
      </div>

      {/* 두 호출 버튼 — 영상 위 (운영자가 먼저 선택) */}
      <div className="flex flex-wrap items-center gap-2 p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
        <button
          onClick={() => void requestPhase('feeding')}
          disabled={loading}
          className="px-3 py-1.5 text-sm rounded font-medium bg-amber-600 hover:bg-amber-500 disabled:bg-gray-700 text-white transition-colors"
        >
          {tr('disputeTab.btnFeedingPool')}
        </button>
        <button
          onClick={() => void requestPhase('baseline')}
          disabled={loading}
          className="px-3 py-1.5 text-sm rounded font-medium bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 text-white transition-colors"
        >
          {tr('disputeTab.btnBaselinePool')}
        </button>
        <div className="text-xs text-gray-500 ml-auto font-mono">
          {tr('dispute.poolSize', { feeding: poolStats.feeding, baseline: poolStats.baseline })}
        </div>
      </div>

      {error && (
        <div className="px-3 py-2 bg-red-900/30 border border-red-500/40 rounded text-sm text-red-300 font-mono">
          {error}
        </div>
      )}

      {/* 초기 안내 (영상 호출 전) */}
      {!current && !loading && !error && (
        <div className="text-sm text-gray-500 italic py-12 text-center space-y-1">
          <p>{tr('disputeTab.emptyPrompt')}</p>
          <p className="text-xs">
            {tr('disputeTab.emptyPromptSub')}
          </p>
        </div>
      )}

      {/* 풀 empty */}
      {!current && !loading && phase && poolStats[phase] === 0 && (
        <div className="text-sm text-gray-500 italic py-8 text-center space-y-1">
          <p>{phaseLabel} {tr('disputeTab.poolEmpty')}</p>
          <p className="text-xs">
            {tr('disputeTab.poolEmptySub')}
          </p>
        </div>
      )}

      {/* 로딩 */}
      {loading && (
        <div className="text-sm text-gray-500 italic py-8 text-center">
          {tr('disputeTab.loadingPool')}
        </div>
      )}

      {/* 영상 + 평가 (R5 한 영상 패턴 유지, 슬라이더는 phase 별 1개) */}
      {current && clipURL && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
          {/* 좌: 영상 */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className={`px-2 py-0.5 rounded text-xs bg-${phaseColor}-500/20 text-${phaseColor}-300 border border-${phaseColor}-500/40`}>
                {tr('disputeTab.currentClip')} {phaseLabel}
              </span>
              <small className="text-xs text-gray-500 font-mono">
                {tr('dispute.queuePosition', { pos: queueIndex + 1, total: queue.length })}
              </small>
            </div>
            <video
              ref={videoRef}
              key={clipID}
              src={clipURL}
              controls
              preload="metadata"
              className="w-full rounded border border-gray-700/50 bg-black"
            />
            <div className="flex items-center justify-between text-xs text-gray-500 font-mono">
              <span>{tankID} · {cameraID}</span>
              <span>{startedAt.slice(0, 19)}</span>
            </div>
            {cycleID && (
              <div className="text-xs text-gray-500 font-mono break-all">
                cycle_id: {cycleID}
              </div>
            )}
            <div className="text-xs text-gray-500 font-mono break-all">
              clip_id: {clipID}
            </div>
          </div>

          {/* 우: 평가 */}
          <div className="space-y-4">
            <p className="text-xs text-gray-400">{phaseDesc}</p>

            {/* phase 별 단일 슬라이더 */}
            {phase && (
              <div className="space-y-1.5">
                <div className="flex items-baseline justify-between">
                  <b className={`text-sm text-${phaseColor}-200`}>{phaseLabel} {tr('disputeTab.scoreLabel')}</b>
                  <span className={`text-sm font-mono text-${phaseColor}-300`}>
                    {score.toFixed(2)}
                  </span>
                </div>
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.05}
                  value={score}
                  onChange={e => setScore(parseFloat(e.target.value))}
                  className={`w-full accent-${phaseColor}-500`}
                />
                <div className="flex justify-between text-xs text-gray-500 font-mono">
                  <span>0 = {scoreLowLabel}</span>
                  <span>1 = {scoreHighLabel}</span>
                </div>
              </div>
            )}

            {/* memo */}
            <div>
              <label className="text-xs text-gray-400 block mb-1">{tr('disputeTab.memoLabel')}</label>
              <textarea
                value={memo}
                onChange={e => setMemo(e.target.value)}
                rows={2}
                placeholder={
                  phase === 'feeding'
                    ? tr('disputeTab.memoPlaceholderFeeding')
                    : tr('disputeTab.memoPlaceholderBaseline')
                }
                className="w-full px-2 py-1 text-xs bg-gray-900 border border-gray-600 rounded text-white placeholder:text-gray-600"
              />
            </div>

            {/* 저장 / 건너뛰기 */}
            <div className="flex flex-wrap items-center gap-2 pt-1">
              <button
                disabled={submitting}
                onClick={() => void saveAndNext('correct')}
                className="px-3 py-1.5 text-xs rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 text-white transition-colors"
              >
                {tr('disputeTab.btnSaveNext')}
              </button>
              <button
                disabled={submitting}
                onClick={() => void saveAndNext('unsure')}
                className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 text-gray-200 transition-colors"
                title={tr('disputeTab.btnUnsureTitle')}
              >
                {tr('disputeTab.btnUnsure')}
              </button>
              <button
                disabled={submitting || loading}
                onClick={() => void skip()}
                className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 text-gray-200 transition-colors"
                title={tr('disputeTab.btnSkipTitle')}
              >
                {tr('disputeTab.btnSkip')}
              </button>
            </div>

            {/* R8: 판단 불가 격리 — 학습 데이터로 부적합한 영상 (전체 가림/어둠 등) */}
            <div className="pt-3 mt-2 border-t border-red-500/20 space-y-1.5">
              <label className="text-xs text-red-300 font-semibold block">
                {tr('disputeTab.excludeLabel')}
              </label>
              <div className="flex flex-wrap items-center gap-2">
                <select
                  value={excludeReason}
                  onChange={e => setExcludeReason(e.target.value as 'occlusion' | 'low_visibility' | 'other')}
                  className="px-2 py-1 text-xs bg-gray-900 border border-red-500/40 rounded text-white [&>option]:bg-gray-900 [&>option]:text-white"
                >
                  <option value="occlusion">{tr('disputeTab.excludeOcclusion')}</option>
                  <option value="low_visibility">{tr('disputeTab.excludeLowVisibility')}</option>
                  <option value="other">{tr('disputeTab.excludeOther')}</option>
                </select>
                <button
                  disabled={submitting || loading}
                  onClick={() => void excludeClip()}
                  className="px-3 py-1.5 text-xs rounded font-medium bg-red-700 hover:bg-red-600 disabled:bg-gray-700 text-white transition-colors"
                  title={tr('disputeTab.btnExcludeTitle')}
                >
                  {tr('disputeTab.btnExclude')}
                </button>
                <small className="text-xs text-gray-500 ml-auto">
                  {tr('disputeTab.excludeRetentionNote')}
                </small>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* dispute 라벨 진척 progress (모든 상태에서 표시) */}
      <div className="space-y-1 pt-2 border-t border-gray-700/30">
        <div className="h-1.5 w-full bg-gray-700/50 rounded-full overflow-hidden">
          <div className="h-full bg-blue-500 transition-all" style={{ width: `${dispPct}%` }} />
        </div>
        <small className="text-xs text-gray-500">
          {tr('disputeTab.progressLabel')} {dispLabel} {tr('disputeTab.progressGate')}
        </small>
      </div>
    </div>
  );
}
