import { useMemo } from 'react';
import {
  ComposedChart, Line, Bar, XAxis, YAxis, CartesianGrid, Tooltip,
  ReferenceLine, ResponsiveContainer,
} from 'recharts';
import type { VisionObservation, FeedCycle } from '../../lib/types';
import { useLanguage } from '../../lib/language-context';

export type TimeRange = '1h' | '6h' | '12h' | '1d' | '7d' | '30d' | 'since-stocking';

// 값은 i18n 키 — 소비처(InferenceMonitor)에서 tr 로 해석해 표시한다.
export const RANGE_LABELS: Record<TimeRange, string> = {
  '1h': 'candleScoreChart.range1h',
  '6h': 'candleScoreChart.range6h',
  '12h': 'candleScoreChart.range12h',
  '1d': 'candleScoreChart.range1d',
  '7d': 'candleScoreChart.range7d',
  '30d': 'candleScoreChart.range30d',
  'since-stocking': 'candleScoreChart.rangeSinceStocking',
};

// 시간 범위 → 윈도우(캔들 1개) 크기. 1시간만 raw (7초 단위 그대로).
const RANGE_CONFIG: Record<TimeRange, { spanMs: number | null; windowMs: number | null }> = {
  '1h':             { spanMs: 1 * 3600_000,        windowMs: null },       // raw 7초
  '6h':             { spanMs: 6 * 3600_000,        windowMs: 30 * 60_000 },
  '12h':            { spanMs: 12 * 3600_000,       windowMs: 60 * 60_000 },
  '1d':             { spanMs: 24 * 3600_000,       windowMs: 2 * 3600_000 },
  '7d':             { spanMs: 7 * 24 * 3600_000,   windowMs: 12 * 3600_000 },
  '30d':            { spanMs: 30 * 24 * 3600_000,  windowMs: 24 * 3600_000 },
  'since-stocking': { spanMs: null,                windowMs: 24 * 3600_000 },
};

type RawPoint = { t: number; v: number };
type CandlePoint = {
  t: number;          // 윈도우 시작
  open: number;
  high: number;
  low: number;
  close: number;
  count: number;
  range: [number, number]; // [low, high]
  body: [number, number];  // [open, close]
};

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

function aggregateCandles(points: RawPoint[], windowMs: number): CandlePoint[] {
  if (points.length === 0) return [];
  const bins = new Map<number, RawPoint[]>();
  for (const p of points) {
    const bucket = Math.floor(p.t / windowMs) * windowMs;
    if (!bins.has(bucket)) bins.set(bucket, []);
    bins.get(bucket)!.push(p);
  }
  const result: CandlePoint[] = [];
  for (const [bucket, items] of [...bins.entries()].sort((a, b) => a[0] - b[0])) {
    items.sort((a, b) => a.t - b.t);
    const vs = items.map(i => i.v);
    const open = vs[0];
    const close = vs[vs.length - 1];
    const high = Math.max(...vs);
    const low = Math.min(...vs);
    result.push({
      t: bucket,
      open, high, low, close,
      count: items.length,
      range: [low, high],
      body: [Math.min(open, close), Math.max(open, close)],
    });
  }
  return result;
}

function fmtTick(
  ms: number,
  range: TimeRange,
  tr: (key: string, vars?: Record<string, string | number>) => string,
): string {
  const d = new Date(ms);
  if (range === '1h' || range === '6h' || range === '12h') {
    return d.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  if (range === '1d') {
    return d.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  if (range === '7d') {
    return `${d.getMonth() + 1}/${d.getDate()} ${tr('candleScoreChart.hourTick', { hour: d.getHours() })}`;
  }
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

// 캔들 몸통 (open ~ close). dataKey="body" + shape={Body}. recharts 가 [body[0], body[1]] 의
// y/height 를 계산해 props 로 전달 → 직접 직사각형 그림.
function CandleBody(props: unknown) {
  // recharts custom shape signature
  type ShapeProps = {
    x: number; y: number; width: number; height: number;
    payload: CandlePoint;
  };
  const { x, y, width, height, payload } = props as ShapeProps;
  if (!payload) return null;
  const isUp = payload.close >= payload.open;
  const color = isUp ? '#22c55e' : '#ef4444';
  const w = Math.max(2, width * 0.7);
  const xCentered = x + (width - w) / 2;
  return (
    <rect
      x={xCentered}
      y={y}
      width={w}
      height={Math.max(1, height)}
      fill={color}
      stroke={color}
      strokeWidth={0.5}
    />
  );
}

// 캔들 심지 (low ~ high). dataKey="range" — y/height 가 low~high 범위.
function CandleWick(props: unknown) {
  type ShapeProps = {
    x: number; y: number; width: number; height: number;
    payload: CandlePoint;
  };
  const { x, y, width, height, payload } = props as ShapeProps;
  if (!payload) return null;
  const isUp = payload.close >= payload.open;
  const color = isUp ? '#22c55e' : '#ef4444';
  const cx = x + width / 2;
  return (
    <line x1={cx} x2={cx} y1={y} y2={y + height} stroke={color} strokeWidth={1} />
  );
}

function CandleTooltip({ active, payload }: {
  active?: boolean;
  payload?: Array<{ payload: CandlePoint }>;
}) {
  const { tr } = useLanguage();
  if (!active || !payload || payload.length === 0) return null;
  const p = payload[0].payload;
  return (
    <div className="px-3 py-2 bg-gray-900/95 border border-gray-700 rounded text-xs space-y-0.5">
      <div className="text-gray-200 font-mono">{new Date(p.t).toLocaleString('ko-KR')}</div>
      <div className="text-gray-400">Open <span className="text-gray-200 font-mono">{p.open.toFixed(3)}</span></div>
      <div className="text-gray-400">High <span className="text-green-300 font-mono">{p.high.toFixed(3)}</span></div>
      <div className="text-gray-400">Low <span className="text-red-300 font-mono">{p.low.toFixed(3)}</span></div>
      <div className="text-gray-400">Close <span className="text-gray-200 font-mono">{p.close.toFixed(3)}</span></div>
      <div className="text-gray-500">{tr('candleScoreChart.observationCount', { count: p.count })}</div>
    </div>
  );
}

function RawTooltip({ active, payload }: {
  active?: boolean;
  payload?: Array<{ payload: RawPoint }>;
}) {
  const { tr } = useLanguage();
  if (!active || !payload || payload.length === 0) return null;
  const p = payload[0].payload;
  return (
    <div className="px-3 py-2 bg-gray-900/95 border border-gray-700 rounded text-xs space-y-0.5">
      <div className="text-gray-200 font-mono">{new Date(p.t).toLocaleString('ko-KR')}</div>
      <div className="text-gray-400">{tr('candleScoreChart.score')} <span className="text-gray-200 font-mono">{p.v.toFixed(3)}</span></div>
    </div>
  );
}

export type CandleScoreChartProps = {
  observations: VisionObservation[];
  feedCycles: FeedCycle[];
  cameraId: string;
  scoreKey: string;
  range: TimeRange;
  stockingAtMs?: number | null;
  height?: number;
};

export function CandleScoreChart({
  observations, feedCycles, cameraId, scoreKey, range, stockingAtMs = null, height = 320,
}: CandleScoreChartProps) {
  const { tr } = useLanguage();
  const now = Date.now();
  const cfg = RANGE_CONFIG[range];
  const isRaw = cfg.windowMs === null;

  const { rawPoints, candles, stats, feedMarkers, from, to } = useMemo(() => {
    const all = extractPoints(observations, cameraId, scoreKey);
    const { from, to, clipped } = clipByRange(all, range, stockingAtMs, now);
    const cs = isRaw ? [] : aggregateCandles(clipped, cfg.windowMs!);
    const vs = clipped.map(p => p.v);
    const avg = vs.length > 0 ? vs.reduce((a, b) => a + b, 0) / vs.length : null;
    const sd = vs.length > 1
      ? Math.sqrt(vs.reduce((s, v) => s + Math.pow(v - (avg ?? 0), 2), 0) / vs.length)
      : null;
    const fm = feedCycles
      .filter(c => {
        const t = new Date(c.started_at).getTime();
        return t >= from && t <= to;
      })
      .map(c => ({
        t: new Date(c.started_at).getTime(),
        cycle: c,
      }));
    return {
      rawPoints: clipped,
      candles: cs,
      stats: {
        count: clipped.length,
        avg, sd,
        max: vs.length > 0 ? Math.max(...vs) : null,
        min: vs.length > 0 ? Math.min(...vs) : null,
      },
      feedMarkers: fm,
      from, to,
    };
  }, [observations, feedCycles, cameraId, scoreKey, range, stockingAtMs, now, isRaw, cfg.windowMs]);

  const hasData = isRaw ? rawPoints.length > 0 : candles.length > 0;

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

      {/* 차트 */}
      {!hasData ? (
        <div className="flex items-center justify-center bg-gray-900/30 border border-gray-700/40 rounded" style={{ height }}>
          <p className="text-sm text-gray-500">{tr('candleScoreChart.noData')}</p>
        </div>
      ) : (
        <ResponsiveContainer width="100%" height={height}>
          {isRaw ? (
            <ComposedChart data={rawPoints} margin={{ top: 10, right: 20, bottom: 10, left: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" opacity={0.3} />
              <XAxis
                dataKey="t"
                type="number"
                domain={[from, to]}
                tickFormatter={t => fmtTick(t, range, tr)}
                stroke="#6b7280"
                fontSize={11}
              />
              <YAxis domain={[0, 1]} stroke="#6b7280" fontSize={11} />
              <Tooltip content={<RawTooltip />} />
              <Line
                type="linear"
                dataKey="v"
                stroke="#22c55e"
                strokeWidth={1.2}
                dot={{ r: 1.5, fill: '#22c55e' }}
                activeDot={{ r: 3 }}
                isAnimationActive={false}
              />
              {feedMarkers.map(m => (
                <ReferenceLine
                  key={m.cycle.cycle_id}
                  x={m.t}
                  stroke="#3b82f6"
                  strokeDasharray="3 3"
                  strokeWidth={1}
                />
              ))}
            </ComposedChart>
          ) : (
            <ComposedChart data={candles} margin={{ top: 10, right: 20, bottom: 10, left: 5 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="#374151" opacity={0.3} />
              <XAxis
                dataKey="t"
                type="number"
                domain={[from, to]}
                tickFormatter={t => fmtTick(t, range, tr)}
                stroke="#6b7280"
                fontSize={11}
              />
              <YAxis domain={[0, 1]} stroke="#6b7280" fontSize={11} />
              <Tooltip content={<CandleTooltip />} />
              <Bar dataKey="range" shape={CandleWick} isAnimationActive={false} />
              <Bar dataKey="body" shape={CandleBody} isAnimationActive={false} />
              {feedMarkers.map(m => (
                <ReferenceLine
                  key={m.cycle.cycle_id}
                  x={m.t}
                  stroke="#3b82f6"
                  strokeDasharray="3 3"
                  strokeWidth={1}
                />
              ))}
            </ComposedChart>
          )}
        </ResponsiveContainer>
      )}

      <div className="flex items-center gap-3 text-xs text-gray-500">
        <span className="inline-flex items-center gap-1">
          <span className="w-3 h-0.5 bg-blue-500" style={{ borderTop: '1px dashed #3b82f6' }} />
          {tr('candleScoreChart.feedStart', { count: feedMarkers.length })}
        </span>
        {!isRaw && (
          <>
            <span className="inline-flex items-center gap-1">
              <span className="w-2 h-2 bg-green-500" /> {tr('candleScoreChart.legendUp')}
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="w-2 h-2 bg-red-500" /> {tr('candleScoreChart.legendDown')}
            </span>
          </>
        )}
        <span className="ml-auto font-mono">
          {isRaw ? tr('candleScoreChart.rawSeconds') : tr('candleScoreChart.candleWindow', { minutes: Math.round((cfg.windowMs ?? 0) / 60_000) })}
        </span>
      </div>
    </div>
  );
}

function Stat({ label, value, color = 'text-gray-200' }: {
  label: string;
  value: string;
  color?: string;
}) {
  return (
    <div>
      <div className="text-xs text-gray-500 uppercase tracking-wide">{label}</div>
      <div className={`text-sm font-mono font-semibold mt-0.5 ${color}`}>{value}</div>
    </div>
  );
}
