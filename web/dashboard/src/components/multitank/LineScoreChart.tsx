import { useMemo } from 'react';
import { CanvasTimeChart, type CanvasPoint } from '../CanvasTimeChart';
import { useLanguage } from '../../lib/language-core';
import { RANGE_CONFIG, type TimeRange } from './candleScoreRanges';

// 평소 활동성 선형 차트 — baseline 클립 요동 대표값(카메라별 중앙값 정규화 비율).
// ⚠ 신설 차트는 recharts(SVG) 금지 — WebKitGTK repaint 누수 (CanvasTimeChart 헤더 참조).

export type LinePoint = { t: number; v: number };

type Props = {
  points: LinePoint[]; // 이 카메라의 점들 (정규화 비율, 1.0 = 평소)
  range: TimeRange;
  stockingAtMs?: number | null;
  height?: number;
};

export function LineScoreChart({ points, range, stockingAtMs = null, height = 240 }: Props) {
  const { tr } = useLanguage();
  const now = Date.now();

  const { data, stats, from, to } = useMemo(() => {
    const cfg = RANGE_CONFIG[range];
    const sorted = [...points].sort((a, b) => a.t - b.t);
    const from = range === 'since-stocking'
      ? (stockingAtMs ?? (sorted[0]?.t ?? now - 24 * 3600_000))
      : now - (cfg.spanMs ?? 0);
    const clipped = sorted.filter(p => p.t >= from && p.t <= now);
    const vs = clipped.map(p => p.v);
    const avg = vs.length > 0 ? vs.reduce((a, b) => a + b, 0) / vs.length : null;
    const sd = vs.length > 1
      ? Math.sqrt(vs.reduce((s, v) => s + Math.pow(v - (avg ?? 0), 2), 0) / vs.length)
      : null;
    return {
      data: clipped.map(p => ({ ts: p.t, v: p.v } as CanvasPoint)),
      stats: {
        count: clipped.length,
        avg, sd,
        max: vs.length > 0 ? Math.max(...vs) : null,
        min: vs.length > 0 ? Math.min(...vs) : null,
      },
      from, to: now,
    };
  }, [points, range, stockingAtMs, now]);

  return (
    <div className="space-y-3">
      {/* 통계 — CandleScoreChart 와 동일 레이아웃 */}
      <div className="grid grid-cols-5 gap-3 px-3 py-2 bg-gray-900/40 border border-gray-700/40 rounded">
        <Stat label={tr('candleScoreChart.statObservations')} value={stats.count.toLocaleString()} />
        <Stat label={tr('candleScoreChart.statAvg')} value={stats.avg !== null ? stats.avg.toFixed(3) : '—'} />
        <Stat label={tr('candleScoreChart.statMax')} value={stats.max !== null ? stats.max.toFixed(3) : '—'} color="text-green-300" />
        <Stat label={tr('candleScoreChart.statMin')} value={stats.min !== null ? stats.min.toFixed(3) : '—'} color="text-red-300" />
        <Stat label={tr('candleScoreChart.statStdDev')} value={stats.sd !== null ? stats.sd.toFixed(3) : '—'} />
      </div>

      {data.length === 0 ? (
        <div className="flex items-center justify-center bg-gray-900/30 border border-gray-700/40 rounded" style={{ height }}>
          <p className="text-sm text-gray-500">{tr('candleScoreChart.noData')}</p>
        </div>
      ) : (
        <CanvasTimeChart
          data={data}
          series={[{ key: 'v', name: tr('lineScoreChart.seriesActivity'), color: '#22c55e' }]}
          xDomain={[from, to]}
          yMin={0}
          yMax="auto"
          minSpan={1.5}
          height={height}
        />
      )}

      <p className="text-xs text-gray-500">{tr('lineScoreChart.note')}</p>
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
