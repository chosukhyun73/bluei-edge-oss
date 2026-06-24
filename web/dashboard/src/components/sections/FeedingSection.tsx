import type { StateVector } from '../../lib/types';
import { relativeTime } from '../../lib/format';
import { useLanguage } from '../../lib/language-context';

type Props = { data?: StateVector['feeding'] };

export function FeedingSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('feedingSection.noData')}</p>;
  const todayTotal = typeof data.today_total_g === 'number' ? data.today_total_g.toLocaleString() : '—';
  const lastG = typeof data.last_feeding_g === 'number' ? `${data.last_feeding_g.toLocaleString()} g` : '—';
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-4">
      <div className="flex items-end gap-2">
        <span className="text-3xl font-mono font-bold text-green-400">
          {todayTotal}
        </span>
        <span className="text-sm text-muted-foreground pb-1">{tr('feedingSection.gToday')}</span>
      </div>

      <div className="grid grid-cols-2 gap-4 text-sm">
        <div>
          <div className="text-muted-foreground text-xs mb-0.5">{tr('feedingSection.lastFeedingAmount')}</div>
          <div className="font-mono font-medium">{lastG}</div>
        </div>
        <div>
          <div className="text-muted-foreground text-xs mb-0.5">{tr('feedingSection.lastFeeding')}</div>
          <div className="font-mono">{data.last_feeding_at ? relativeTime(data.last_feeding_at) : '—'}</div>
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
