import type { WeightHistorySnapshot } from '../lib/types';
import { formatNum } from '../lib/format';
import { useLanguage } from '../lib/language-context';

interface Props {
  snapshots: WeightHistorySnapshot[];
  width?: number;
  height?: number;
}

const PAD = { top: 16, right: 12, bottom: 32, left: 48 };

export function WeightHistoryChart({ snapshots, width = 540, height = 180 }: Props) {
  const { tr } = useLanguage();
  if (snapshots.length === 0) {
    return (
      <p className="text-xs text-muted-foreground italic mt-2">
        {tr('weightHistoryChart.noDataYet')}
      </p>
    );
  }

  const sorted = [...snapshots].sort((a, b) =>
    a.snapshot_date.localeCompare(b.snapshot_date),
  );

  const weights = sorted.map(s => s.estimated_avg_weight_g);
  const minW = Math.min(...weights);
  const maxW = Math.max(...weights);
  const rangeW = maxW - minW || 1;

  const innerW = width - PAD.left - PAD.right;
  const innerH = height - PAD.top - PAD.bottom;

  const toX = (i: number) => PAD.left + (i / Math.max(sorted.length - 1, 1)) * innerW;
  const toY = (w: number) => PAD.top + innerH - ((w - minW) / rangeW) * innerH;

  const points = sorted.map((s, i) => `${toX(i)},${toY(s.estimated_avg_weight_g)}`).join(' ');

  const hasStale = sorted.some(s => s.quality !== 'ok');

  // X axis label positions: first, middle, last
  const labelIdxs = [0, Math.floor((sorted.length - 1) / 2), sorted.length - 1].filter(
    (v, i, a) => a.indexOf(v) === i,
  );

  // Growth rate: (last - first) / days
  const dailyGain =
    sorted.length >= 2
      ? (weights[weights.length - 1] - weights[0]) / Math.max(sorted.length - 1, 1)
      : null;

  return (
    <div className="w-full overflow-x-auto">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        width="100%"
        style={{ maxWidth: width }}
        aria-label={tr('weightHistoryChart.ariaLabel')}
      >
        {/* Y axis labels */}
        <text x={PAD.left - 4} y={PAD.top + 4} textAnchor="end" fontSize={10} fill="#9ca3af" fontFamily="monospace">
          {formatNum(maxW, { digits: 0, suffix: 'g' })}
        </text>
        <text x={PAD.left - 4} y={PAD.top + innerH + 4} textAnchor="end" fontSize={10} fill="#9ca3af" fontFamily="monospace">
          {formatNum(minW, { digits: 0, suffix: 'g' })}
        </text>

        {/* Grid lines */}
        <line x1={PAD.left} y1={PAD.top} x2={PAD.left + innerW} y2={PAD.top} stroke="#1a1a1a" strokeWidth={1} />
        <line x1={PAD.left} y1={PAD.top + innerH} x2={PAD.left + innerW} y2={PAD.top + innerH} stroke="#374151" strokeWidth={1} />

        {/* Polyline */}
        <polyline
          points={points}
          fill="none"
          stroke={hasStale ? '#6b7280' : '#22c55e'}
          strokeWidth={2}
          strokeDasharray={hasStale ? '4 3' : undefined}
        />

        {/* Data point dots */}
        {sorted.map((s, i) => (
          <circle
            key={s.snapshot_date}
            cx={toX(i)}
            cy={toY(s.estimated_avg_weight_g)}
            r={3}
            fill={s.fcr_source === 'calibrated' ? '#22c55e' : '#3b82f6'}
          />
        ))}

        {/* X axis labels */}
        {labelIdxs.map(i => (
          <text
            key={i}
            x={toX(i)}
            y={PAD.top + innerH + 16}
            textAnchor="middle"
            fontSize={9}
            fill="#9ca3af"
            fontFamily="monospace"
          >
            {sorted[i].snapshot_date.slice(5)} {/* MM-DD */}
          </text>
        ))}
      </svg>

      {/* Summary line */}
      <p className="text-xs text-muted-foreground font-mono mt-1">
        {tr('weightHistoryChart.minLabel')} {formatNum(minW, { digits: 1, suffix: 'g' })} · {tr('weightHistoryChart.maxLabel')} {formatNum(maxW, { digits: 1, suffix: 'g' })}
        {dailyGain !== null && ` · ${tr('weightHistoryChart.dailyGainLabel')} +${formatNum(dailyGain, { digits: 1, suffix: 'g' })}`}
      </p>
    </div>
  );
}
