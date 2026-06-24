import type { StateVector, WeightHistorySnapshot } from '../../lib/types';
import { formatNum } from '../../lib/format';
import { Badge } from '../ui/badge';
import { WeightHistoryChart } from '../WeightHistoryChart';
import { useLanguage } from '../../lib/language-context';

type Props = {
  data?: StateVector['biological_context'];
  weightSnapshots: WeightHistorySnapshot[];
};

export function BiologicalContextSection({ data, weightSnapshots }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('biologicalContext.noData')}</p>;
  const fishCount = typeof data.fish_count === 'number' ? data.fish_count.toLocaleString() : '—';
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.species')}</div>
          <div className="text-sm font-medium">{data.species || '—'}</div>
        </div>
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.fishCount')}</div>
          <div className="text-sm font-mono font-medium">{fishCount}</div>
        </div>
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.avgWeight')}</div>
          <div className="text-sm font-mono font-medium">{formatNum(data.avg_weight_g, { digits: 1, suffix: ' g' })}</div>
        </div>
        <div className="bg-muted/40 rounded-lg p-3">
          <div className="text-xs text-muted-foreground mb-1">{tr('biologicalContext.biomass')}</div>
          <div className="text-sm font-mono font-medium">{formatNum(data.biomass_kg, { digits: 1, suffix: ' kg' })}</div>
        </div>
      </div>

      {data.estimated_avg_weight_g !== undefined && (
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">{tr('biologicalContext.estimatedAvgWeight')}</span>
          <span className="font-mono font-medium">{formatNum(data.estimated_avg_weight_g, { digits: 1, suffix: 'g' })}</span>
          {data.fcr_source && (
            <Badge
              className={`text-xs ${data.fcr_source === 'calibrated' ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-blue-500/20 text-blue-400 border-blue-500/30'}`}
            >
              {data.fcr_source === 'calibrated' ? tr('biologicalContext.fcrCalibrated') : tr('biologicalContext.fcrDefault')}
            </Badge>
          )}
          {data.expected_fcr !== undefined && (
            <span className="text-xs text-muted-foreground">FCR {formatNum(data.expected_fcr, { digits: 2 })}</span>
          )}
        </div>
      )}

      {/* Weight history chart */}
      <div>
        <div className="text-xs text-muted-foreground mb-2">{tr('biologicalContext.weightTrend30d')}</div>
        <WeightHistoryChart snapshots={weightSnapshots} />
      </div>

      <div className="text-xs text-muted-foreground">
        {tr('biologicalContext.dataSource')}: <span className="font-mono">{data.source}</span> · {data.system_type}
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
