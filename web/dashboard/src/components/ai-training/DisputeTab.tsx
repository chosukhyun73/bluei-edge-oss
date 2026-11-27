import { useCallback, useEffect, useRef, useState } from 'react';
import { useLanguage } from '../../lib/language-core';
import { Vision } from '../../lib/api';
import type {
  VisionTrainingStatus,
  VisionTrainingPoolItem,
  VisionTrainingPoolPhase,
  VisionAgitationResponse,
  VisionSegmentsResponse,
  VisionClipSegment,
  SegmentLabelInput,
  SegmentLabelSource,
} from '../../lib/types';

const BASE = ''; // vite proxy / Tauri 가 host 결정

// R11 — phase 별 LRCN 모델 카드 매핑. 운영자는 phase 만 의식, algorithm_id 는 backend 자동.
const PHASE_TO_ALGORITHM_ID: Record<'feeding' | 'baseline', string> = {
  feeding: 'vision_lrcn_blueocean_reaction_v1',
  baseline: 'vision_lrcn_blueocean_stability_v1',
};

// 요동곡선 미니 스파크라인 — series 를 SVG polyline 으로. 기저선(점선) + 피크 지점 표시.
function AgitationSparkline({
  series, baseline, peakAtS,
}: {
  series: number[];
  baseline: number;
  peakAtS: number;
}) {
  if (series.length < 2) return null;
  const W = 100, H = 30;
  const lo = Math.min(...series, baseline);
  const hi = Math.max(...series, baseline);
  const span = hi - lo || 1;
  const x = (i: number) => (i / (series.length - 1)) * W;
  const y = (v: number) => H - ((v - lo) / span) * H;
  const pts = series.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
  const baseY = y(baseline).toFixed(1);
  const peakX = peakAtS >= 0 && peakAtS < series.length ? x(peakAtS) : null;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="w-full h-8">
      <line x1={0} y1={baseY} x2={W} y2={baseY} stroke="#6b7280" strokeWidth={0.5} strokeDasharray="2 2" />
      <polyline points={pts} fill="none" stroke="#f59e0b" strokeWidth={1} vectorEffect="non-scaling-stroke" />
      {peakX != null && (
        <line x1={peakX} y1={0} x2={peakX} y2={H} stroke="#f59e0b" strokeWidth={0.5} strokeDasharray="1 2" opacity={0.6} />
      )}
    </svg>
  );
}

// docs/50 — 클립 재생 URL (기존 DisputeTab clipURL 생성 로직과 동일 경로, 임의 clip_id 용).
function mediaClipURL(clipID: string): string {
  return `${BASE}/v1/media/clips/${encodeURIComponent(clipID)}/play.mp4`;
}

// 세그먼트 라벨 상태 — 슬라이더/격리/출처(prior|operator) 를 pulse_no 별로 보관.
type SegLabelEntry = { score: number; source: SegmentLabelSource; excluded: boolean; excludeReason: string; prior?: number };

// 격리 사유 — 주 사유는 "실제 사료 미급이(전달 안 됨) → 먹이반응 판단 근거 없음".
const SEG_EXCLUDE_REASONS = ['feed_not_delivered', 'occlusion', 'other'] as const;

// 펄스 타임라인 — 막대 높이 = 제안값(prior). 클릭 시 seek, 현재 재생 위치 펄스 하이라이트.
function PulseTimeline({
  segments, activePulseNo, onSeek,
}: {
  segments: VisionClipSegment[];
  activePulseNo: number | null;
  onSeek: (offsetStartMs: number) => void;
}) {
  const { tr } = useLanguage();
  if (segments.length === 0) return null;
  return (
    <div className="flex items-end gap-0.5 h-10 px-1 bg-gray-950/60 rounded border border-gray-700/40">
      {segments.map(sg => {
        const h = Math.max(8, Math.round((sg.agitation_prior ?? 0.5) * 100));
        const active = sg.pulse_no === activePulseNo;
        return (
          <button
            key={sg.pulse_no}
            type="button"
            onClick={() => onSeek(sg.offset_start_ms)}
            title={tr('disputeTab.pulseTitle', { n: sg.pulse_no, score: (sg.agitation_prior ?? 0).toFixed(2) })}
            style={{ height: `${h}%`, flexGrow: sg.duration_ms || 1 }}
            className={`min-w-[6px] rounded-sm transition-colors ${
              active ? 'bg-amber-400 ring-1 ring-amber-200' : 'bg-amber-600/60 hover:bg-amber-500/80'
            }`}
          />
        );
      })}
    </div>
  );
}

// 세그먼트(펄스) 채점 카드 — 미니 슬라이더로 직접 끌기 + 상태칩 + 격리 토글.
function SegmentCard({
  segment, label, active, onScoreChange, onToggleIsolate, onSetReason, tr,
}: {
  segment: VisionClipSegment;
  label: SegLabelEntry;
  active: boolean;
  onScoreChange: (pulseNo: number, score: number) => void;
  onToggleIsolate: (pulseNo: number) => void;
  onSetReason: (pulseNo: number, reason: string) => void;
  tr: (key: string, vars?: Record<string, string | number>) => string;
}) {
  const chip = label.excluded
    ? { text: tr('disputeTab.seg.chipExcluded'), cls: 'bg-red-500/20 text-red-300 border-red-500/40' }
    : label.source === 'operator'
      ? { text: tr('disputeTab.seg.chipConfirmed'), cls: 'bg-green-500/20 text-green-300 border-green-500/40' }
      : { text: tr('disputeTab.seg.chipSuggested'), cls: 'bg-amber-500/20 text-amber-300 border-amber-500/40' };
  return (
    <div
      className={`w-36 shrink-0 space-y-1.5 p-2 rounded border transition-colors ${
        active ? 'border-amber-400/70 bg-amber-500/5' : 'border-gray-700/50 bg-gray-900/40'
      } ${label.excluded ? 'opacity-50' : ''}`}
    >
      <div className="flex items-center justify-between">
        <b className="text-xs text-gray-200 font-mono">#{segment.pulse_no}</b>
        <span className={`px-1.5 py-0.5 text-[10px] rounded border ${chip.cls}`}>{chip.text}</span>
      </div>
      {segment.agitation_prior != null && (
        <div className="text-[10px] text-gray-500 font-mono">
          peak {(segment.seg_peak ?? 0).toFixed(2)} / base {(segment.seg_baseline ?? 0).toFixed(2)}
          {segment.prior_camera_id && (
            <span className="ml-1 text-sky-400/80" title={segment.prior_camera_id}>
              {tr('disputeTab.seg.priorFromTop')}
            </span>
          )}
          {segment.prior_unreliable && (
            <span className="ml-1 text-red-400/80">{tr('disputeTab.seg.priorUnreliable')}</span>
          )}
        </div>
      )}
      <input
        type="range"
        min={0}
        max={1}
        step={0.05}
        value={label.score}
        disabled={label.excluded}
        onChange={e => onScoreChange(segment.pulse_no, parseFloat(e.target.value))}
        className="w-full accent-amber-500"
      />
      <div className="flex items-center justify-between">
        <span className="text-xs font-mono text-amber-200">{label.score.toFixed(2)}</span>
        <button
          type="button"
          onClick={() => onToggleIsolate(segment.pulse_no)}
          className={`px-1.5 py-0.5 text-[10px] rounded border transition-colors ${
            label.excluded
              ? 'bg-red-700 border-red-500 text-white'
              : 'bg-gray-800 border-gray-600 text-gray-400 hover:bg-red-900/40 hover:text-red-300'
          }`}
          title={tr('disputeTab.seg.isolateTitle')}
        >
          {tr('disputeTab.seg.isolateBtn')}
        </button>
      </div>
      {/* 격리 사유 — 주 사유 "사료 미급이(전달 안 됨) → 판단 근거 없음" */}
      {label.excluded && (
        <select
          value={label.excludeReason || 'feed_not_delivered'}
          onChange={e => onSetReason(segment.pulse_no, e.target.value)}
          className="w-full text-[10px] bg-gray-900 border border-red-500/40 rounded text-red-200 px-1 py-0.5"
        >
          {SEG_EXCLUDE_REASONS.map(rk => (
            <option key={rk} value={rk}>{tr(`disputeTab.seg.reason.${rk}`)}</option>
          ))}
        </select>
      )}
    </div>
  );
}

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
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const feedingVideoRef = useRef<HTMLVideoElement | null>(null);

  // WebKitGTK 는 key 교체로 <video> 가 unmount 돼도 GStreamer 파이프라인을 GC 까지
  // 붙들어, 클립을 넘길 때마다 렌더러 메모리가 계단식으로 남는다 (2026-07-14 멈춤
  // 원인 실측 — 라벨 확정 버스트당 +300~430MB). detach 시점에 명시적으로 해제한다.
  const releaseVideo = (v: HTMLVideoElement) => {
    v.pause();
    v.removeAttribute('src');
    v.load();
  };
  const setVideoEl = useCallback((el: HTMLVideoElement | null) => {
    if (!el && videoRef.current) releaseVideo(videoRef.current);
    videoRef.current = el;
  }, []);
  const setFeedingVideoEl = useCallback((el: HTMLVideoElement | null) => {
    if (!el && feedingVideoRef.current) releaseVideo(feedingVideoRef.current);
    feedingVideoRef.current = el;
  }, []);

  // 현재 표시 중인 영상 + 풀 큐 (feeding 은 사이클 대표 클립 1건 — 같은 cycle_id 중복 제거된 큐)
  const [current, setCurrent] = useState<VisionTrainingPoolItem | null>(null);
  const [phase, setPhase] = useState<VisionTrainingPoolPhase | null>(null);
  const [queue, setQueue] = useState<VisionTrainingPoolItem[]>([]);
  const [queueIndex, setQueueIndex] = useState(0);

  // 평가 (baseline 전용 — 통클립 1슬라이더)
  const [score, setScore] = useState(0.5);
  const [memo, setMemo] = useState('');

  // 요동곡선 요약 + 제안 점수(먹이반응 prior) — 참고용. baseline 클립 기준.
  const [agitation, setAgitation] = useState<VisionAgitationResponse | null>(null);

  // R8: 판단 불가 격리 (baseline 전용 — 통클립 격리)
  const [excludeReason, setExcludeReason] = useState<'occlusion' | 'low_visibility' | 'other'>('occlusion');

  // 카메라 라인 분리 (2026-07-17 운영자 확정): 탑뷰=LRCN 학습 라인 / 수중=별도 라인.
  // 채점·저장 모두 선택된 라인의 클립만 대상 — 안 본 라인에 prior 복사 라벨 방지.
  const [cameraLine, setCameraLine] = useState<'top' | 'underwater'>('top');
  const inLine = useCallback((cam: string) =>
    cameraLine === 'underwater' ? cam.includes('underwater') : !cam.includes('underwater'),
  [cameraLine]);

  // docs/50 — feeding 전용 펄스 세그먼트 채점 상태
  const [segmentsData, setSegmentsData] = useState<VisionSegmentsResponse | null>(null);
  const [segmentsLoading, setSegmentsLoading] = useState(false);
  const [activeClipId, setActiveClipId] = useState('');
  const [labelState, setLabelState] = useState<Record<string, Record<number, SegLabelEntry>>>({});
  // 격리(=무배출) 토글을 실제로 만진 클립. 초기값이 항상 excluded=false 라, 안 만진
  // 라벨은 "전부 배출"이 기본값 수락일 뿐 운영자 판단이 아니다 — 정답지 판별용 (2026-08-12).
  const [isolateTouched, setIsolateTouched] = useState<Set<string>>(new Set());
  const [playPosMs, setPlayPosMs] = useState(0);

  // 상태
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // phase 별 풀 size (마지막 조회 응답에서)
  const [poolStats, setPoolStats] = useState<Record<VisionTrainingPoolPhase, number>>({
    feeding: 0,
    baseline: 0,
  });

  // 영상 새로 표시될 때 슬라이더/메모 reset + video reload (baseline 전용)
  useEffect(() => {
    setScore(0.5);
    setMemo('');
    if (videoRef.current) {
      videoRef.current.currentTime = 0;
      videoRef.current.load();
    }
  }, [current?.payload.clip_id]);

  // 현재 클립의 요동곡선 요약 fetch (참고용 제안 점수). baseline 전용 — feeding 은 segments 가 대체.
  useEffect(() => {
    const clipID = current?.payload.clip_id;
    if (!clipID || phase === 'feeding') {
      setAgitation(null);
      return;
    }
    let alive = true;
    Vision.agitation(clipID)
      .then(res => { if (alive) setAgitation(res.found ? res : null); })
      .catch(() => { if (alive) setAgitation(null); });
    return () => { alive = false; };
  }, [current?.payload.clip_id, phase]);

  // docs/50 — feeding 사이클의 펄스 세그먼트 + 제안값 fetch. current 는 그 사이클의 대표 클립.
  const feedingCycleID = phase === 'feeding' ? current?.payload.evidence?.cycle_id : undefined;
  useEffect(() => {
    if (!feedingCycleID) {
      setSegmentsData(null);
      return;
    }
    let alive = true;
    setSegmentsLoading(true);
    Vision.segments(feedingCycleID)
      .then(res => { if (alive) setSegmentsData(res); })
      .catch(() => { if (alive) setSegmentsData(null); })
      .finally(() => { if (alive) setSegmentsLoading(false); });
    return () => { alive = false; };
  }, [feedingCycleID]);

  // 세그먼트 응답 도착 시 — 카메라 탭(top 우선) + pulse_no 별 라벨 상태(초기값=prior) 초기화
  useEffect(() => {
    if (!segmentsData?.found || !segmentsData.clips || segmentsData.clips.length === 0) {
      setLabelState({});
      setActiveClipId('');
      return;
    }
    const init: Record<string, Record<number, SegLabelEntry>> = {};
    for (const clip of segmentsData.clips) {
      const perPulse: Record<number, SegLabelEntry> = {};
      for (const seg of clip.segments) {
        perPulse[seg.pulse_no] = {
          // 프리필 = 2-head 모델 사전 채점 우선(2026-07-18), 없으면 요동 prior 폴백.
          score: seg.model_score ?? seg.agitation_prior ?? 0.5,
          source: 'prior',
          excluded: false,
          excludeReason: '',
          prior: seg.agitation_prior,
        };
      }
      init[clip.clip_id] = perPulse;
    }
    setLabelState(init);
    setPlayPosMs(0);
  }, [segmentsData]);

  // 라인/사이클 변경 시 해당 라인의 첫 클립 선택 — 라벨 상태는 건드리지 않음
  // (토글로 라인을 오가도 채점 중인 값 보존).
  useEffect(() => {
    const lineClips = (segmentsData?.clips ?? []).filter(c => inLine(c.camera_id));
    setActiveClipId(prev =>
      lineClips.some(c => c.clip_id === prev) ? prev : (lineClips[0]?.clip_id ?? ''));
  }, [segmentsData, inLine]);

  // 모달 실시간 평가 피드백 (2026-07-17 운영자): 사이클 종료 모달에서 방금 준 점수가
  // 있으면 이 사이클 탑뷰 슬라이더의 시작값으로 반영 — 영상 재시청 없이 확정 가능.
  const [liveEvalScore, setLiveEvalScore] = useState<number | null>(null);
  useEffect(() => {
    setLiveEvalScore(null);
    const cid = segmentsData?.cycle_id;
    const tid = segmentsData?.tank_id;
    if (!cid || !tid || !segmentsData?.clips?.length) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await Vision.observations({ tankId: tid, limit: 30 });
        const items = res.items as unknown as { payload?: Record<string, unknown> }[];
        const obs = items.find(it => {
          const ev = it.payload?.['evidence'] as Record<string, unknown> | undefined;
          const cam = (it.payload?.['camera_id'] as string | undefined) ?? '';
          return ev?.['cycle_id'] === cid && !cam.includes('underwater');
        });
        const obsId = obs?.payload?.['observation_id'] as string | undefined;
        if (!obsId || cancelled) return;
        const d = await Vision.observationDisputes(obsId, 5);
        const score = d.items?.[0]?.payload?.operator_score;
        if (cancelled || typeof score !== 'number') return;
        setLiveEvalScore(score);
        setLabelState(prev => {
          const nextState = { ...prev };
          for (const clip of segmentsData.clips ?? []) {
            if (clip.camera_id.includes('underwater')) continue; // 탑뷰 라인만
            const per = { ...(nextState[clip.clip_id] ?? {}) };
            for (const k of Object.keys(per)) {
              const e = per[Number(k)];
              if (e && e.source === 'prior' && !e.excluded) {
                per[Number(k)] = { ...e, score, source: 'operator' };
              }
            }
            nextState[clip.clip_id] = per;
          }
          return nextState;
        });
      } catch { /* 프리필 실패는 조용히 무시 — 채점은 그대로 가능 */ }
    })();
    return () => { cancelled = true; };
  }, [segmentsData]);

  // 새 phase 풀에서 새 영상 1건 호출 (queue 새로 받음). feeding 은 같은 cycle_id 중복 제거.
  const requestPhase = useCallback(async (p: VisionTrainingPoolPhase) => {
    setLoading(true);
    setError(null);
    try {
      const res = await Vision.trainingPool(p, 10);
      setPoolStats(prev => ({ ...prev, [p]: res.count }));
      let items = res.items;
      if (p === 'feeding') {
        const seen = new Set<string>();
        items = items.filter(it => {
          const cid = it.payload.evidence?.cycle_id;
          if (!cid || seen.has(cid)) return false;
          seen.add(cid);
          return true;
        });
      }
      if (items.length === 0) {
        setQueue([]);
        setQueueIndex(0);
        setCurrent(null);
        setPhase(p);
        return;
      }
      setQueue(items);
      setQueueIndex(0);
      setCurrent(items[0]);
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

  // 저장 + 다음. scoreOverride 지정 시 슬라이더값 대신 그 값으로 제출(제안값 원클릭 수락).
  async function saveAndNext(verdict: 'correct' | 'wrong' | 'unsure', scoreOverride?: number) {
    if (!current || !phase) return;
    const submitScore = scoreOverride ?? score;
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
              ? { during_score: submitScore }
              : { pre_score: submitScore }),
            operator_score: submitScore,
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

  // R8: 판단 불가 격리 / 보류 — 원본 mp4+poster 즉시 삭제 + 풀에서 제외.
  // reasonOverride='unsure' 면 보류(모르겠음) 경로.
  async function excludeClip(reasonOverride?: string) {
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
            reason: reasonOverride ?? excludeReason,
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

  // docs/50 — feeding 세그먼트 채점: 현재 활성 카메라 탭의 클립
  const activeClip = segmentsData?.clips?.find(c => c.clip_id === activeClipId) ?? null;
  const activeClipURL = activeClip ? mediaClipURL(activeClip.clip_id) : '';
  const activePulseNo = activeClip
    ? activeClip.segments.find(sg => playPosMs >= sg.offset_start_ms && playPosMs < sg.offset_end_ms)?.pulse_no ?? null
    : null;

  function setSegScore(clipId: string, pulseNo: number, val: number) {
    setLabelState(prev => ({
      ...prev,
      [clipId]: {
        ...prev[clipId],
        [pulseNo]: { ...prev[clipId]?.[pulseNo], score: val, source: 'operator', excluded: prev[clipId]?.[pulseNo]?.excluded ?? false },
      },
    }));
  }

  function toggleSegIsolate(clipId: string, pulseNo: number) {
    setIsolateTouched(prev => (prev.has(clipId) ? prev : new Set(prev).add(clipId)));
    setLabelState(prev => {
      const cur = prev[clipId]?.[pulseNo];
      const nextExcluded = !(cur?.excluded ?? false);
      return {
        ...prev,
        [clipId]: {
          ...prev[clipId],
          // 격리 시 기본 사유 = 사료 미급이(판단 근거 없음). 해제 시 사유 비움.
          [pulseNo]: { ...cur, excluded: nextExcluded, excludeReason: nextExcluded ? (cur?.excludeReason || 'feed_not_delivered') : '' },
        },
      };
    });
  }

  function setSegExcludeReason(clipId: string, pulseNo: number, reason: string) {
    setLabelState(prev => ({
      ...prev,
      [clipId]: {
        ...prev[clipId],
        [pulseNo]: { ...prev[clipId]?.[pulseNo], excludeReason: reason },
      },
    }));
  }

  function seekActiveClip(offsetStartMs: number) {
    if (feedingVideoRef.current) {
      feedingVideoRef.current.currentTime = Math.max(0, offsetStartMs / 1000);
    }
  }

  // [전체 확정 + 다음 사이클] — 모든 카메라 탭의 세그먼트를 각각 저장(격리 펄스 제외), 성공 후 다음 사이클.
  async function saveAllSegmentsAndNext() {
    if (!segmentsData?.clips || segmentsData.clips.length === 0) return;
    setSubmitting(true);
    setError(null);
    try {
      // 선택된 카메라 라인의 클립만 저장 — 안 본 라인에 prior 복사 라벨이 생기는 것 방지.
      for (const clip of segmentsData.clips.filter(c => inLine(c.camera_id))) {
        const perPulse = labelState[clip.clip_id] ?? {};
        // 격리 세그먼트도 사유와 함께 저장(그냥 버리지 않음) — 급이 전달 실패 추적.
        const segs: SegmentLabelInput[] = clip.segments.map(sg => {
          const lbl = perPulse[sg.pulse_no];
          if (lbl?.excluded) {
            return {
              pulse_no: sg.pulse_no,
              offset_start_ms: sg.offset_start_ms,
              offset_end_ms: sg.offset_end_ms,
              score: 0,
              prior: sg.agitation_prior,
              source: 'prior',
              excluded: true,
              exclude_reason: lbl.excludeReason || 'feed_not_delivered',
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
        });
        if (segs.length === 0) continue;
        await Vision.saveSegmentLabels({
          cycle_id: segmentsData.cycle_id,
          clip_id: clip.clip_id,
          camera_id: clip.camera_id,
          tank_id: segmentsData.tank_id,
          operator_id: 'operator',
          algorithm_id: PHASE_TO_ALGORITHM_ID.feeding,
          discharge_modified: isolateTouched.has(clip.clip_id),
          segments: segs,
        });
      }
      onSubmitted();
      await nextInQueue();
    } catch (e) {
      setError(e instanceof Error ? e.message : tr('disputeTab.errorSave'));
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

      {/* 영상 + 평가 (baseline 전용 — R5 한 영상 패턴, 통클립 슬라이더 1개) */}
      {phase === 'baseline' && current && clipURL && (
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
              ref={setVideoEl}
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

            {/* 요동곡선 요약 + 제안 점수 (참고용 prior) */}
            {agitation && (
              <div className="space-y-2 p-2.5 rounded border border-amber-500/25 bg-amber-500/5">
                <div className="flex items-center justify-between">
                  <b className="text-xs text-amber-200">{tr('disputeTab.agitationTitle')}</b>
                  {agitation.startle_suspect && (
                    <span className="px-1.5 py-0.5 text-[10px] rounded bg-yellow-500/20 text-yellow-300 border border-yellow-500/40">
                      ⚠ {tr('disputeTab.agitationStartle')}
                    </span>
                  )}
                </div>
                <AgitationSparkline
                  series={agitation.series ?? []}
                  baseline={agitation.baseline ?? 0}
                  peakAtS={agitation.peak_at_s ?? -1}
                />
                <div className="grid grid-cols-3 gap-1 text-[11px] text-gray-400 font-mono">
                  <span>{tr('disputeTab.agitationPeakRise')}: <b className="text-amber-300">
                    +{Math.round((agitation.peak_rise_frac ?? 0) * 100)}%</b></span>
                  <span>{tr('disputeTab.agitationCalm')}: {agitation.calm_reached ? '✓' : '—'}</span>
                  <span className="text-right">
                    {tr('disputeTab.agitationSuggested')}: <b className="text-amber-200">
                      {(agitation.suggested_score ?? 0).toFixed(2)}</b>
                  </span>
                </div>
                <p className="text-[10px] text-gray-500 leading-tight">{tr('disputeTab.agitationHint')}</p>
              </div>
            )}

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
                placeholder={tr('disputeTab.memoPlaceholderBaseline')}
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
                onClick={() => void excludeClip('unsure')}
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

      {/* docs/50 — feeding 전용 펄스 세그먼트 채점 (통클립 슬라이더 대체) */}
      {phase === 'feeding' && current && (
        <div className="space-y-3 p-3 bg-gray-900/40 border border-gray-700/40 rounded-lg">
          <div className="flex items-center justify-between">
            <span className="px-2 py-0.5 rounded text-xs bg-amber-500/20 text-amber-300 border border-amber-500/40">
              {tr('disputeTab.currentClip')} {phaseLabel}
            </span>
            <small className="text-xs text-gray-500 font-mono">
              {tr('dispute.queuePosition', { pos: queueIndex + 1, total: queue.length })}
            </small>
          </div>

          {segmentsLoading && (
            <div className="text-sm text-gray-500 italic py-6 text-center">{tr('disputeTab.seg.loading')}</div>
          )}

          {!segmentsLoading && segmentsData && !segmentsData.found && (
            <div className="text-sm text-gray-500 italic py-6 text-center space-y-2">
              <p>{tr('disputeTab.seg.notFound')}</p>
              <button
                disabled={submitting}
                onClick={() => void nextInQueue()}
                className="px-3 py-1.5 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 text-gray-200 transition-colors"
              >
                {tr('disputeTab.seg.btnNextCycle')}
              </button>
            </div>
          )}

          {!segmentsLoading && segmentsData?.found && (
            <>
              {/* 카메라 라인 토글 — 탑뷰=LRCN 학습 / 수중=별도 라인 (①탭과 동일한 분리) */}
              <div className="flex items-center gap-1.5">
                {(['top', 'underwater'] as const).map(line => (
                  <button
                    key={line}
                    onClick={() => setCameraLine(line)}
                    className={`px-2.5 py-1 text-xs rounded border transition-colors ${
                      cameraLine === line
                        ? 'bg-sky-700 border-sky-500 text-white'
                        : 'bg-gray-800 border-gray-600 text-gray-400 hover:bg-gray-700'
                    }`}
                  >
                    {line === 'top' ? tr('disputeTab.lineTop') : tr('disputeTab.lineUnderwater')}
                  </button>
                ))}
                {liveEvalScore != null && cameraLine === 'top' && (
                  <span className="px-2 py-0.5 text-xs rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/40">
                    {tr('disputeTab.liveEvalPrefill', { score: liveEvalScore.toFixed(2) })}
                  </span>
                )}
              </div>

              {/* 카메라 탭 — 선택된 라인의 클립만 */}
              <div className="flex flex-wrap gap-1.5">
                {segmentsData.clips?.filter(c => inLine(c.camera_id)).map(clip => (
                  <button
                    key={clip.clip_id}
                    onClick={() => setActiveClipId(clip.clip_id)}
                    className={`px-2.5 py-1 text-xs rounded border transition-colors ${
                      clip.clip_id === activeClipId
                        ? 'bg-amber-600 border-amber-500 text-white'
                        : 'bg-gray-800 border-gray-600 text-gray-300 hover:bg-gray-700'
                    }`}
                  >
                    {clip.camera_id}
                  </button>
                ))}
              </div>
              {!activeClip && (
                <p className="text-xs text-gray-500">{tr('disputeTab.lineEmpty')}</p>
              )}
            </>
          )}

          {!segmentsLoading && segmentsData?.found && activeClip && (
            <>

              {/* 영상 */}
              <video
                ref={setFeedingVideoEl}
                key={activeClip.clip_id}
                src={activeClipURL}
                controls
                preload="metadata"
                onTimeUpdate={e => setPlayPosMs(e.currentTarget.currentTime * 1000)}
                className="w-full rounded border border-gray-700/50 bg-black h-[840px] object-contain"
              />
              <div className="flex items-center justify-between text-xs text-gray-500 font-mono">
                <span>{segmentsData.tank_id ?? tankID} · {activeClip.camera_id}</span>
                <span>{activeClip.started_at.slice(0, 19)}</span>
              </div>
              <div className="text-xs text-gray-500 font-mono break-all">
                cycle_id: {segmentsData.cycle_id} · clip_id: {activeClip.clip_id}
              </div>

              {/* 펄스 타임라인 — 막대 클릭 시 seek, 현재 재생 펄스 하이라이트 */}
              <PulseTimeline segments={activeClip.segments} activePulseNo={activePulseNo} onSeek={seekActiveClip} />

              {/* 점수 의미 안내 — 0=사료 섭이 안 함 · 1=먹이를 열심히 먹음 */}
              <div className="flex items-center justify-between text-[11px] text-gray-400 font-mono px-0.5">
                <span>{tr('disputeTab.seg.scaleLow')}</span>
                <span className="text-gray-500">{tr('disputeTab.seg.scaleLabel')}</span>
                <span>{tr('disputeTab.seg.scaleHigh')}</span>
              </div>

              {/* 세그먼트 스트립 — 펄스당 카드(미니 슬라이더 직접 끌기 + 상태칩 + 격리) */}
              <div className="flex gap-2 overflow-x-auto pb-1">
                {activeClip.segments.map(seg => {
                  const label = labelState[activeClip.clip_id]?.[seg.pulse_no];
                  if (!label) return null;
                  return (
                    <SegmentCard
                      key={seg.pulse_no}
                      segment={seg}
                      label={label}
                      active={seg.pulse_no === activePulseNo}
                      onScoreChange={(pulseNo, val) => setSegScore(activeClip.clip_id, pulseNo, val)}
                      onToggleIsolate={pulseNo => toggleSegIsolate(activeClip.clip_id, pulseNo)}
                      onSetReason={(pulseNo, reason) => setSegExcludeReason(activeClip.clip_id, pulseNo, reason)}
                      tr={tr}
                    />
                  );
                })}
              </div>

              <button
                disabled={submitting}
                onClick={() => void saveAllSegmentsAndNext()}
                className="px-3 py-1.5 text-xs rounded font-medium bg-green-600 hover:bg-green-500 disabled:bg-gray-700 text-white transition-colors"
              >
                {tr('disputeTab.seg.btnSaveAllNext')}
              </button>
            </>
          )}
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
