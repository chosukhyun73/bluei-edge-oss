import { useMemo } from 'react';
import { dateLocale } from '../../lib/format';
import { CanvasTimeChart, type CanvasPoint } from '../CanvasTimeChart';
import type { VisionObservation, FeedCycle } from '../../lib/types';
import { useLanguage } from '../../lib/language-core';
import { RANGE_CONFIG, type TimeRange } from './candleScoreRanges';

// 급이 관찰 시계열 차트 — 요동/r3 등 0~1 점수.
// ⚠ recharts(SVG) 금지: WebKitGTK 가 화면 SVG 를 repaint 할 때마다 메모리를 잔류시켜
// 몇 시간 열어두면 GB 단위 누수 → 워치독 재기동(관찰 끊김). CanvasTimeChart(비트맵,
// 제자리 갱신)로 렌더해 누수를 제거한다 (2026-07-24, CanvasTimeChart 헤더 참조).
// 데이터 준비 로직(extractPoints/clipByRange/병합/급이마커/통계)은 종전 그대로 보존.

type RawPoint = { t: number; v: number };

const OVERLAY_COLOR = '#22d3ee'; // r3 오버레이 (청록, 'AI' 관례)
const PRIMARY_COLOR = '#22c55e'; // 요동/지표 (초록)
const MARKER_COLOR = '#3b82f6';  // 급이 시작 (파랑)
const BAND_COLOR = 'rgba(34,211,238,0.13)'; // 펄스 반응창 음영 (청록 계열, 판정 구간)

// 펄스 반응창 — 백엔드 배출 판정과 동일 (discharge_check.go dischargePostStartS/EndS).
// ⚠ 이 값을 바꾸면 화면과 판정이 어긋난다. 백엔드와 함께 바꿀 것.
const PULSE_WINDOW_START_MS = 10_000;
const PULSE_WINDOW_END_MS = 20_000;

function extractPoints(
  observations: VisionObservation[],
  cameraId: string,
  scoreKey: string,
): RawPoint[] {
  const out: RawPoint[] = [];
  for (const obs of observations) {
    if (cameraId && obs.camera_id !== cameraId) continue;
    if (!obs.recorded_at) continue;
    const v = obs.scores?.[scoreKey];
    if (typeof v !== 'number') continue;
    out.push({ t: new Date(obs.recorded_at).getTime(), v });
  }
  out.sort((a, b) => a.t - b.t);
  return out;
}

function clipByRange(
  points: RawPoint[],
  range: TimeRange,
  stockingAtMs: number | null,
  now: number,
): { from: number; to: number; clipped: RawPoint[] } {
  const cfg = RANGE_CONFIG[range];
  let from: number;
  if (range === 'since-stocking') {
    from = stockingAtMs ?? (points[0]?.t ?? now - 24 * 3600_000);
  } else {
    from = now - (cfg.spanMs ?? 0);
  }
  const to = now;
  const clipped = points.filter(p => p.t >= from && p.t <= to);
  return { from, to, clipped };
}

// x축 시각 포맷 — 장기 범위는 날짜 병기.
function fmtTick(range: TimeRange): (ms: number) => string {
  return (ms: number) => {
    const d = new Date(ms);
    if (range === '7d' || range === '30d') {
      return `${d.getMonth() + 1}/${d.getDate()}`;
    }
    return d.toLocaleTimeString(dateLocale(), { hour: '2-digit', minute: '2-digit', hour12: false });
  };
}

export type CandleScoreChartProps = {
  observations: VisionObservation[];
  feedCycles: FeedCycle[];
  cameraId: string;
  scoreKey: string;
  range: TimeRange;
  stockingAtMs?: number | null;
  height?: number;
  // 오버레이 시리즈 (선택) — 주 지표(예: 요동) 위에 r3 펄스 점수를 겹쳐 변화를 함께 판단.
  // 급이 로직 무관, 관찰용. 청록(OVERLAY_COLOR). 없으면 미표시.
  overlayObservations?: VisionObservation[];
  overlayScoreKey?: string;
  overlayLabel?: string;
};

export function CandleScoreChart({
  observations, feedCycles, cameraId, scoreKey, range, stockingAtMs = null, height = 320,
  overlayObservations, overlayScoreKey, overlayLabel,
}: CandleScoreChartProps) {
  const { tr } = useLanguage();
  const now = Date.now();

  const { data, markers, bands, stats, from, to, hasOverlay } = useMemo(() => {
    const all = extractPoints(observations, cameraId, scoreKey);
    const { from, to, clipped } = clipByRange(all, range, stockingAtMs, now);

    // 오버레이(r3) — 같은 창으로 클립.
    const overlay = (overlayObservations && overlayScoreKey)
      ? clipByRange(extractPoints(overlayObservations, cameraId, overlayScoreKey), range, stockingAtMs, now).clipped
      : [];

    // 시각(ts) union 으로 병합 — 주 지표는 v, 오버레이는 r3. connectNulls 로 각 시리즈가
    // 상대 시각의 빈칸을 건너뛴다.
    const byT = new Map<number, CanvasPoint>();
    for (const p of clipped) byT.set(p.t, { ts: p.t, v: p.v });
    for (const o of overlay) {
      const ex = byT.get(o.t);
      if (ex) ex.r3 = o.v;
      else byT.set(o.t, { ts: o.t, r3: o.v });
    }
    const data = Array.from(byT.values()).sort((a, b) => a.ts - b.ts);

    // 급이 시작 마커 (창 내 사이클).
    const markers = feedCycles
      .filter(c => {
        const t = new Date(c.started_at).getTime();
        return t >= from && t <= to;
      })
      .map(c => ({ ts: new Date(c.started_at).getTime(), color: MARKER_COLOR }));

    // 펄스 반응창 음영 — 판정이 실제로 보는 구간(펄스 시작 +10~20초)을 그대로 표시한다.
    // 근거: 배출 판정 창이 dischargePostStartS=10 / dischargePostEndS=20 (discharge_check.go).
    // 오버레이(r3) 관측의 시각은 feed.pulse.started 조인값(pulse_at, vision_model_scores.go)
    // 이라 백엔드 판정과 기준점이 같다.
    //
    // 왜 필요한가: 사람이 다가오거나 소음이 나면 창 밖에서도 봉우리가 생기는데, 지금 화면
    // 으로는 그게 사료 반응인지 구분할 방법이 없었다. 실측(8-05 tank_01) 곡선 최고 상승폭
    // 3.83 은 마지막 펄스 창이 끝나고 48초 뒤 지점이었고, 창 안 최고는 2.38 이었다.
    // ⚠ 색까지 여기서 붙인다. JSX 에서 .map 하면 매 렌더마다 새 배열이 되고,
    // CanvasTimeChart 의 draw useEffect 가 그 배열을 의존성으로 잡아 계속 다시 그린다.
    const bands = overlay
      .filter(o => o.t >= from - PULSE_WINDOW_END_MS && o.t <= to)
      .map(o => ({
        from: o.t + PULSE_WINDOW_START_MS,
        to: o.t + PULSE_WINDOW_END_MS,
        color: BAND_COLOR,
      }));

    // 통계 — 주 지표 값 기준.
    const vs = clipped.map(p => p.v);
    const avg = vs.length > 0 ? vs.reduce((a, b) => a + b, 0) / vs.length : null;
    const sd = vs.length > 1
      ? Math.sqrt(vs.reduce((s, v) => s + Math.pow(v - (avg ?? 0), 2), 0) / vs.length)
      : null;

    return {
      data, markers, bands,
      stats: {
        count: clipped.length,
        avg, sd,
        max: vs.length > 0 ? Math.max(...vs) : null,
        min: vs.length > 0 ? Math.min(...vs) : null,
      },
      from, to,
      hasOverlay: overlay.length > 0,
    };
  }, [observations, overlayObservations, overlayScoreKey, cameraId, scoreKey, range, stockingAtMs, now, feedCycles]);

  const series = [{ key: 'v', name: tr('candleScoreChart.score'), color: PRIMARY_COLOR }];
  if (hasOverlay) series.push({ key: 'r3', name: overlayLabel ?? 'r3', color: OVERLAY_COLOR });

  return (
    <div className="space-y-3">
      {/* 통계 */}
      <div className="grid grid-cols-5 gap-3 px-3 py-2 bg-gray-900/40 border border-gray-700/40 rounded">
        <Stat label={tr('candleScoreChart.statObservations')} value={stats.count.toLocaleString()} />
        <Stat label={tr('candleScoreChart.statAvg')} value={stats.avg !== null ? stats.avg.toFixed(3) : '—'} />
        <Stat label={tr('candleScoreChart.statMax')} value={stats.max !== null ? stats.max.toFixed(3) : '—'} color="text-green-300" />
        <Stat label={tr('candleScoreChart.statMin')} value={stats.min !== null ? stats.min.toFixed(3) : '—'} color="text-red-300" />
        <Stat label={tr('candleScoreChart.statStdDev')} value={stats.sd !== null ? stats.sd.toFixed(3) : '—'} />
      </div>

      {/* 차트 (canvas — 누수 없음) */}
      {data.length === 0 ? (
        <div className="flex items-center justify-center bg-gray-900/30 border border-gray-700/40 rounded" style={{ height }}>
          <p className="text-sm text-gray-500">{tr('candleScoreChart.noData')}</p>
        </div>
      ) : (
        <CanvasTimeChart
          data={data}
          series={series}
          xDomain={[from, to]}
          yMin={0}
          yMax={1}
          height={height}
          markers={markers}
          bands={bands}
          fmtTime={fmtTick(range)}
        />
      )}

      {/* 범례 — 급이 시작(마커) + r3(오버레이) */}
      <div className="flex items-center gap-3 text-xs text-gray-500">
        <span className="inline-flex items-center gap-1">
          <span className="w-3 h-0.5" style={{ borderTop: `1px dashed ${MARKER_COLOR}` }} />
          {tr('candleScoreChart.feedStart', { count: markers.length })}
        </span>
        {hasOverlay && (
          <span className="inline-flex items-center gap-1">
            <span className="w-2 h-2 rounded-full" style={{ backgroundColor: OVERLAY_COLOR }} />
            {overlayLabel ?? 'r3'} ({data.filter(d => typeof d.r3 === 'number').length})
          </span>
        )}
        {bands.length > 0 && (
          <span className="inline-flex items-center gap-1">
            <span className="w-3 h-3 rounded-sm" style={{ backgroundColor: BAND_COLOR }} />
            {tr('candleScoreChart.pulseWindow', { count: bands.length })}
          </span>
        )}
      </div>
    </div>
  );
}

function Stat({ label, value, color = 'text-gray-200' }: { label: string; value: string; color?: string }) {
  return (
    <div>
      <div className="text-xs text-gray-500 uppercase tracking-wide">{label}</div>
      <div className={`text-sm font-mono font-semibold mt-0.5 ${color}`}>{value}</div>
    </div>
  );
}
