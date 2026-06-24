import type { StateVector } from '../../lib/types';
import { relativeTime, qualityColor } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-context';

type Props = { data?: StateVector['fish'] };

export function FishSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('fishSection.noData')}</p>;
  const score = typeof data.activity_score === 'number' ? data.activity_score : 0;
  const pct = Math.round(score * 100);
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-3">
      {/* Activity score */}
      <div>
        <div className="flex justify-between text-sm mb-1">
          <span className="text-muted-foreground">{tr('fishSection.activityScore')}</span>
          <span className="font-mono font-medium">{pct}%</span>
        </div>
        <div className="h-2 rounded-full bg-muted overflow-hidden">
          <div
            className="h-full bg-green-500 transition-all"
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      <div className="flex items-center gap-3 text-sm">
        <span className="text-muted-foreground">{tr('fishSection.lastObserved')}</span>
        <span className="font-mono">{data.last_observed_at ? relativeTime(data.last_observed_at) : '—'}</span>
        <div className="flex items-center gap-1">
          <span className={`inline-block w-2 h-2 rounded-full ${qualityColor(data.quality)}`} />
          <Badge variant="outline" className="text-xs">{data.quality || '—'}</Badge>
        </div>
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
