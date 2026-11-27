import type { StateVector } from '../../lib/types';
import { useLanguage } from '../../lib/language-core';
import { Badge } from '../ui/badge';

type Props = { data?: StateVector['convergence'] };

// 수렴 점수(1-격차) 색 — 높을수록 정확(초록).
function scoreColor(score: number): string {
  if (score >= 0.85) return 'bg-green-500';
  if (score >= 0.7) return 'bg-yellow-400';
  if (score >= 0.5) return 'bg-orange-400';
  return 'bg-red-400';
}

// 정적 스파크라인 — 격차(gap) 시계열. 낮을수록(아래) 정확. inline SVG(비상주·비애니).
function GapSparkline({ series }: { series: NonNullable<StateVector['convergence']['series']> }) {
  const W = 240, H = 40, pad = 3;
  const gaps = series.map(p => p.gap);
  const max = Math.max(0.5, ...gaps); // 최소 스케일 0.5 (과확대 방지)
  const n = series.length;
  const x = (i: number) => (n <= 1 ? W / 2 : pad + (i * (W - 2 * pad)) / (n - 1));
  const y = (g: number) => pad + (g / max) * (H - 2 * pad); // 격차 클수록 아래(y↑)
  const pts = series.map((p, i) => `${x(i).toFixed(1)},${y(p.gap).toFixed(1)}`).join(' ');
  return (
    <svg width="100%" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="block">
      <polyline points={pts} fill="none" stroke="#22c55e" strokeWidth={1.5} />
      {series.map((p, i) => (
        <circle key={i} cx={x(i)} cy={y(p.gap)} r={1.5} fill="#22c55e" />
      ))}
    </svg>
  );
}

export function ConvergenceSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('convergenceSection.noData')}</p>;

  const hasData = typeof data.convergence_score === 'number';
  const score = data.convergence_score ?? 0;
  const pct = Math.round(score * 100);
  const gap = data.recent_gap ?? 0;
  const sample = data.sample_count.toLocaleString();
  const notes = Array.isArray(data.notes) ? data.notes : [];

  const dir = data.direction;
  const dirBadge =
    dir === 'improving' ? { cls: 'bg-green-500/20 text-green-400 border-green-500/30', txt: `↓ ${tr('convergenceSection.improving')}` }
    : dir === 'worsening' ? { cls: 'bg-red-500/20 text-red-400 border-red-500/30', txt: `↑ ${tr('convergenceSection.worsening')}` }
    : dir === 'stable' ? { cls: 'bg-gray-500/20 text-gray-300 border-gray-500/30', txt: tr('convergenceSection.stable') }
    : { cls: 'bg-gray-500/20 text-gray-400 border-gray-500/30', txt: tr('convergenceSection.insufficient') };

  return (
    <div className="space-y-4">
      {/* 정확도(수렴 점수) 바 */}
      <div>
        <div className="flex justify-between text-sm mb-1.5">
          <span className="text-muted-foreground">{tr('convergenceSection.accuracy')}</span>
          <span className="font-mono font-bold">{hasData ? `${pct}%` : '—'}</span>
        </div>
        <div className="h-3 rounded-full bg-muted overflow-hidden">
          <div className={`h-full transition-all ${scoreColor(score)}`} style={{ width: `${pct}%` }} />
        </div>
      </div>

      {/* 현재 격차 + 방향 */}
      <div className="flex items-center justify-between">
        <div className="text-sm">
          <span className="text-muted-foreground">{tr('convergenceSection.recentGap')} </span>
          <span className="font-mono font-semibold">{hasData ? gap.toFixed(3) : '—'}</span>
        </div>
        <Badge className={`text-xs ${dirBadge.cls}`}>{dirBadge.txt}</Badge>
      </div>

      {/* 스파크라인 (격차 추이 — 내려갈수록 정확) */}
      {data.series && data.series.length >= 2 && (
        <div className="rounded border border-border/40 bg-muted/20 px-2 py-1.5">
          <GapSparkline series={data.series} />
          <div className="flex justify-between text-[10px] text-muted-foreground mt-0.5">
            <span>{tr('convergenceSection.older')}</span>
            <span>{tr('convergenceSection.axisHint')}</span>
            <span>{tr('convergenceSection.newer')}</span>
          </div>
        </div>
      )}

      <p className="text-xs text-muted-foreground">{tr('convergenceSection.explain')}</p>
      <span className="text-xs text-muted-foreground">{tr('convergenceSection.sampleCount', { count: sample })}</span>

      {notes.length > 0 && (
        <ul className="space-y-0.5">
          {notes.map((n, i) => (
            <li key={i} className="text-xs text-muted-foreground italic">{n}</li>
          ))}
        </ul>
      )}
    </div>
  );
}
