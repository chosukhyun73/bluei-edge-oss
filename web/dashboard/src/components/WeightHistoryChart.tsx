import type { WeightHistorySnapshot, SamplingItem } from '../lib/types';
import { formatNum } from '../lib/format';
import { useLanguage } from '../lib/language-core';

interface Props {
  snapshots: WeightHistorySnapshot[];
  samplings?: SamplingItem[];
  width?: number;
  height?: number;
}

const PAD = { top: 16, right: 12, bottom: 32, left: 48 };

// 날짜 문자열 → epoch ms. snapshot_date("YYYY-MM-DD")는 UTC 자정 기준.
function toT(d: string): number {
  const s = d.length === 10 ? d + 'T00:00:00Z' : d;
  return new Date(s).getTime();
}

export function WeightHistoryChart({ snapshots, samplings = [], width = 540, height = 180 }: Props) {
  const { tr } = useLanguage();
  if (snapshots.length === 0) {
    return (
      <p className="text-xs text-muted-foreground italic mt-2">
        {tr('weightHistoryChart.noDataYet')}
      </p>
    );
  }

  const sorted = [...snapshots].sort((a, b) => a.snapshot_date.localeCompare(b.snapshot_date));

  // 유효 실측만 (중량 > 0), 시각 오름차순 — 마지막이 최신
  const validSamples = samplings
    .filter(s => s.avg_weight_g > 0)
    .sort((a, b) => a.sampled_at.localeCompare(b.sampled_at));

  // Y 범위 — 추정 + 실측 모두 포함
  const allW = [...sorted.map(s => s.estimated_avg_weight_g), ...validSamples.map(s => s.avg_weight_g)];
  const minW = Math.min(...allW);
  const maxW = Math.max(...allW);
  const rangeW = maxW - minW || 1;

  // X 범위 — 추정 snapshot_date + 실측 sampled_at 모두 포함, 실제 날짜 간격 반영
  const allT = [...sorted.map(s => toT(s.snapshot_date)), ...validSamples.map(s => toT(s.sampled_at))];
  const minT = Math.min(...allT);
  const maxT = Math.max(...allT);
  const rangeT = maxT - minT || 1;

  const innerW = width - PAD.left - PAD.right;
  const innerH = height - PAD.top - PAD.bottom;

  const toX = (t: number) => PAD.left + ((t - minT) / rangeT) * innerW;
  const toY = (w: number) => PAD.top + innerH - ((w - minW) / rangeW) * innerH;

  const points = sorted.map(s => `${toX(toT(s.snapshot_date))},${toY(s.estimated_avg_weight_g)}`).join(' ');
  const hasStale = sorted.some(s => s.quality !== 'ok');

  // X축 라벨: 처음/중간/끝 날짜
  const labelIdxs = [0, Math.floor((sorted.length - 1) / 2), sorted.length - 1].filter(
    (v, i, a) => a.indexOf(v) === i,
  );

  const weights = sorted.map(s => s.estimated_avg_weight_g);
  const dailyGain =
    sorted.length >= 2 ? (weights[weights.length - 1] - weights[0]) / Math.max(sorted.length - 1, 1) : null;

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

        {/* 추정중량 곡선 */}
        <polyline
          points={points}
          fill="none"
          stroke={hasStale ? '#6b7280' : '#22c55e'}
          strokeWidth={2}
          strokeDasharray={hasStale ? '4 3' : undefined}
        />

        {/* 추정 데이터 포인트 */}
        {sorted.map(s => (
          <circle
            key={s.snapshot_date}
            cx={toX(toT(s.snapshot_date))}
            cy={toY(s.estimated_avg_weight_g)}
            r={3}
            fill={s.fcr_source === 'calibrated' ? '#22c55e' : '#3b82f6'}
          />
        ))}

        {/* 실측중량 오버레이 (주황 다이아몬드 + 날짜) — 추정과 명확히 구분 */}
        {validSamples.map(s => {
          const x = toX(toT(s.sampled_at));
          const y = toY(s.avg_weight_g);
          const r = 5;
          return (
            <g key={s.sampling_id}>
              <polygon
                points={`${x},${y - r} ${x + r},${y} ${x},${y + r} ${x - r},${y}`}
                fill="#f59e0b"
                stroke="#1a1a1a"
                strokeWidth={1}
              />
              <text x={x} y={y - r - 3} textAnchor="middle" fontSize={9} fill="#f59e0b" fontFamily="monospace">
                {formatNum(s.avg_weight_g, { digits: 0, suffix: 'g' })}
              </text>
            </g>
          );
        })}

        {/* X axis labels */}
        {labelIdxs.map(i => (
          <text
            key={i}
            x={toX(toT(sorted[i].snapshot_date))}
            y={PAD.top + innerH + 16}
            textAnchor="middle"
            fontSize={9}
            fill="#9ca3af"
            fontFamily="monospace"
          >
            {sorted[i].snapshot_date.slice(5)}
          </text>
        ))}
      </svg>

      {/* Summary line */}
      <p className="text-xs text-muted-foreground font-mono mt-1">
        {tr('weightHistoryChart.minLabel')} {formatNum(minW, { digits: 1, suffix: 'g' })} · {tr('weightHistoryChart.maxLabel')} {formatNum(maxW, { digits: 1, suffix: 'g' })}
        {dailyGain !== null && ` · ${tr('weightHistoryChart.dailyGainLabel')} +${formatNum(dailyGain, { digits: 1, suffix: 'g' })}`}
      </p>

      {/* 실측 범례 / 없음 표기 */}
      <p className="text-xs mt-1 flex items-center gap-1.5">
        <span className="inline-block w-2 h-2 rotate-45 bg-amber-500" />
        {validSamples.length > 0 ? (
          <span className="text-muted-foreground">
            {tr('weightHistoryChart.measuredLabel')}: {validSamples.length}{tr('weightHistoryChart.measuredCountSuffix')}
            {' · '}
            {tr('weightHistoryChart.measuredLatest')} {validSamples[validSamples.length - 1].sampled_at.slice(0, 10)}
          </span>
        ) : (
          <span className="text-muted-foreground italic">{tr('weightHistoryChart.noMeasured')}</span>
        )}
      </p>
    </div>
  );
}
