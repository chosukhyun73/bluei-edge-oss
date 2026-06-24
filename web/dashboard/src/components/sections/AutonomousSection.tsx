import type { StateVector } from '../../lib/types';
import { relativeTime } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-context';

type Props = { data?: StateVector['autonomous'] };

function modeStyle(mode: string): string {
  switch (mode) {
    case 'full':        return 'bg-green-500/20 text-green-400 border-green-500/30';
    case 'partial':     return 'bg-orange-500/20 text-orange-400 border-orange-500/30';
    case 'observation': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    default:            return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
  }
}

// tr 키 반환 — 렌더 시점에 tr 로 해소.
function modeLabelKey(mode: string): string {
  switch (mode) {
    case 'full':        return 'autonomous.modeFull';
    case 'partial':     return 'autonomous.modePartial';
    case 'observation': return 'autonomous.modeObservation';
    default:            return 'autonomous.modeInactive';
  }
}

export function AutonomousSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('autonomousSection.noData')}</p>;
  const notes = Array.isArray(data.notes) ? data.notes : [];

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <Badge className={`text-sm px-3 py-1 ${modeStyle(data.mode)}`}>
          {tr(modeLabelKey(data.mode))}
        </Badge>
        <span className="text-xs text-muted-foreground font-mono">{data.mode}</span>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
        <div>
          <div className="text-xs text-muted-foreground mb-0.5">{tr('autonomousSection.reason')}</div>
          <div>{data.reason || '—'}</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground mb-0.5">{tr('autonomousSection.changedAt')}</div>
          <div className="font-mono">{data.changed_at ? relativeTime(data.changed_at) : '—'}</div>
        </div>
        <div>
          <div className="text-xs text-muted-foreground mb-0.5">{tr('autonomousSection.changedBy')}</div>
          <div className="font-mono">{data.changed_by || '—'}</div>
        </div>
      </div>

      {notes.length > 0 && (
        <ul className="space-y-0.5">
          {notes.map((n, i) => (
            <li key={i} className="text-xs text-muted-foreground italic">{n}</li>
          ))}
        </ul>
      )}

      <p className="text-xs text-muted-foreground italic">
        {tr('autonomousSection.phase3bNote')}
      </p>
    </div>
  );
}
