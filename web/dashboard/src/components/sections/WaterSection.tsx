import type { StateVector } from '../../lib/types';
import { relativeTime, qualityColor } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-context';

type Props = { data?: StateVector['water'] };

export function WaterSection({ data }: Props) {
  const { tr } = useLanguage();

  const labels: Record<string, string> = {
    water_temperature: tr('waterSection.labelWaterTemperature'),
    dissolved_oxygen: tr('waterSection.labelDissolvedOxygen'),
    ph: 'pH',
    salinity: tr('waterSection.labelSalinity'),
    nitrate: tr('waterSection.labelNitrate'),
    nitrite: tr('waterSection.labelNitrite'),
    unionized_ammonia: tr('waterSection.labelUnionizedAmmonia'),
    carbon_dioxide: tr('waterSection.labelCarbonDioxide'),
    total_suspended_solids: 'TSS',
    flow_rate: tr('waterSection.labelFlowRate'),
    pump_pressure: tr('waterSection.labelPumpPressure'),
    light_intensity: tr('waterSection.labelLightIntensity'),
    feed_weight: tr('waterSection.labelFeedWeight'),
  };

  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('waterSection.noData')}</p>;
  const metrics = data.metrics && typeof data.metrics === 'object' ? data.metrics : {};
  const entries = Object.entries(metrics);
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-4">
      {entries.length === 0 ? (
        <p className="text-sm text-muted-foreground italic">{tr('waterSection.noWaterData')}</p>
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
          {entries.map(([key, metric]) => (
            <div key={key} className="bg-muted/40 rounded-lg p-3 space-y-1">
              <div className="text-xs text-muted-foreground">{labels[key] ?? key}</div>
              <div className="font-mono text-sm font-medium">
                {metric.value} <span className="text-muted-foreground text-xs">{metric.unit}</span>
              </div>
              <div className="flex items-center gap-1">
                <span className={`inline-block w-1.5 h-1.5 rounded-full ${qualityColor(metric.quality)}`} />
                <span className="text-xs text-muted-foreground">{metric.quality}</span>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center gap-3 text-sm">
        <span className="text-muted-foreground">{tr('waterSection.lastObserved')}</span>
        <span className="font-mono">{data.last_observed_at ? relativeTime(data.last_observed_at) : '—'}</span>
        {data.predictions?.available ? (
          <Badge className="text-xs bg-green-500/20 text-green-400 border-green-500/30">{tr('waterSection.predictionActive')}</Badge>
        ) : (
          <span className="text-xs text-muted-foreground">{tr('waterSection.noPredictionModel')}</span>
        )}
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
