import { useLanguage } from '../../lib/language-context';
import type { StateVector } from '../../lib/types';

type Props = { data?: StateVector['adaptation'] };

export function AdaptationSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('adaptationSection.noData')}</p>;
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-3">
      {data.transition_detected ? (
        <div className="bg-orange-500/10 border border-orange-500/30 rounded-lg px-4 py-3">
          <div className="text-sm font-medium text-orange-400 mb-1">{tr('adaptationSection.transitionDetected')}</div>
          {data.transition_reason && (
            <div className="text-sm text-orange-300">{data.transition_reason}</div>
          )}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{tr('adaptationSection.baselineStable')}</p>
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
