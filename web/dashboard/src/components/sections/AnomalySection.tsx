import type { StateVector } from '../../lib/types';
import { relativeTime } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-context';

type Props = { data?: StateVector['anomaly'] };

function severityStyle(severity: string): string {
  switch (severity) {
    case 'critical': return 'bg-red-500/20 text-red-400 border-red-500/30';
    case 'warning':  return 'bg-orange-500/20 text-orange-400 border-orange-500/30';
    case 'info':     return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    default:         return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
  }
}

export function AnomalySection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('anomalySection.noData')}</p>;
  const alerts = Array.isArray(data.open_alerts) ? data.open_alerts : [];
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Badge
          className={`text-xs ${data.has_model ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-gray-500/20 text-gray-400 border-gray-500/30'}`}
        >
          {data.has_model ? tr('anomalySection.modelTrained') : tr('anomalySection.modelUntrained')}
        </Badge>
      </div>

      {alerts.length === 0 ? (
        <p className="text-sm text-muted-foreground">{tr('anomalySection.noOpenAlerts')}</p>
      ) : (
        <div className="space-y-2">
          {alerts.map(alert => (
            <div key={alert.alert_id} className="flex items-start gap-3 bg-muted/40 rounded-lg p-3">
              <Badge className={`text-xs flex-shrink-0 mt-0.5 ${severityStyle(alert.severity)}`}>
                {alert.severity}
              </Badge>
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium">{alert.kind}</div>
                <div className="text-xs text-muted-foreground mt-0.5">{alert.message}</div>
                <div className="text-xs text-muted-foreground font-mono mt-1">{relativeTime(alert.opened_at)}</div>
              </div>
            </div>
          ))}
        </div>
      )}

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
