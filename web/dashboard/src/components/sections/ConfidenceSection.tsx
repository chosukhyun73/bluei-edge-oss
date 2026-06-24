import type { StateVector } from '../../lib/types';
import { useLanguage } from '../../lib/language-context';
import { Badge } from '../ui/badge';

type Props = { data?: StateVector['confidence'] };

function scoreColor(score: number): string {
  if (score >= 0.8) return 'bg-green-500';
  if (score >= 0.6) return 'bg-yellow-400';
  if (score >= 0.3) return 'bg-orange-400';
  return 'bg-red-400';
}

function MiniBar({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <div className="flex justify-between text-xs mb-0.5">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono">{Math.round(value * 100)}%</span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div
          className={`h-full transition-all ${scoreColor(value)}`}
          style={{ width: `${value * 100}%` }}
        />
      </div>
    </div>
  );
}

export function ConfidenceSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('confidenceSection.noData')}</p>;
  const score = typeof data.tank_confidence_score === 'number' ? data.tank_confidence_score : 0;
  const pct = Math.round(score * 100);
  const barColor = scoreColor(score);
  const c = data.components;
  const notes = Array.isArray(data.notes) ? data.notes : [];
  const sampleCount = typeof c?.sample_count === 'number' ? c.sample_count.toLocaleString() : '—';

  const adaptationColor =
    data.adaptation_level === 'hot'
      ? 'bg-green-500/20 text-green-400 border-green-500/30'
      : data.adaptation_level === 'warm'
      ? 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30'
      : data.adaptation_level === 'warming'
      ? 'bg-orange-500/20 text-orange-400 border-orange-500/30'
      : 'bg-red-500/20 text-red-400 border-red-500/30';

  return (
    <div className="space-y-4">
      {/* Composite score bar */}
      <div>
        <div className="flex justify-between text-sm mb-1.5">
          <span className="text-muted-foreground">{tr('confidenceSection.overallScore')}</span>
          <span className="font-mono font-bold">{pct}%</span>
        </div>
        <div className="h-3 rounded-full bg-muted overflow-hidden">
          <div
            className={`h-full transition-all ${barColor}`}
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      {/* Sub scores */}
      <div className="space-y-2">
        <MiniBar label={tr('confidenceSection.forecastAccuracy')} value={c?.forecast_accuracy ?? 0} />
        <MiniBar label={tr('confidenceSection.baselineStability')} value={c?.baseline_stability ?? 0} />
        <MiniBar label={tr('confidenceSection.trainingMaturity')} value={c?.training_maturity ?? 0} />
      </div>

      {/* Chips */}
      <div className="flex flex-wrap gap-2">
        <Badge className={`text-xs ${adaptationColor}`}>
          {data.adaptation_level === 'hot' ? tr('confidenceSection.adaptHot')
            : data.adaptation_level === 'warm' ? tr('confidenceSection.adaptWarm')
            : data.adaptation_level === 'warming' ? tr('confidenceSection.adaptWarming')
            : tr('confidenceSection.adaptCold')}
        </Badge>
        <Badge className={`text-xs ${c?.has_baseline ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-gray-500/20 text-gray-400 border-gray-500/30'}`}>
          {c?.has_baseline ? `✓ ${tr('confidence.baseline')}` : tr('confidenceSection.noBaseline')}
        </Badge>
        <Badge className={`text-xs ${c?.has_forecast ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-gray-500/20 text-gray-400 border-gray-500/30'}`}>
          {c?.has_forecast ? tr('confidenceSection.hasForecast') : tr('confidenceSection.noForecast')}
        </Badge>
        <span className="text-xs text-muted-foreground self-center">{tr('confidence.sampleCount', { count: sampleCount })}</span>
      </div>

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
