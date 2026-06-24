import type { StateVector } from '../../lib/types';
import { relativeTime } from '../../lib/format';
import { Badge } from '../ui/badge';
import { useLanguage } from '../../lib/language-context';

type Props = { data?: StateVector['decisions'] };

function routeStyle(route: string): string {
  switch (route) {
    case 'auto_executed':      return 'bg-green-500/20 text-green-400 border-green-500/30';
    case 'pending_approval':   return 'bg-orange-500/20 text-orange-400 border-orange-500/30';
    case 'pending_notify':     return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    case 'advisory_only':      return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
    case 'rejected':           return 'bg-red-500/20 text-red-400 border-red-500/30';
    default:                   return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
  }
}

// i18n 키 반환 — 렌더 시 tr 로 해석. default 는 원본 route 문자열(키 미존재 시 그대로 폴백).
function routeLabel(route: string): string {
  switch (route) {
    case 'auto_executed':    return 'decisions.routeAutoExecuted';
    case 'pending_approval': return 'decisions.routePendingApproval';
    case 'pending_notify':   return 'decisions.routePendingNotify';
    case 'advisory_only':    return 'decisions.routeAdvisoryOnly';
    case 'rejected':         return 'decisions.routeRejected';
    default:                 return route;
  }
}

export function DecisionsSection({ data }: Props) {
  const { tr } = useLanguage();
  if (!data) return <p className="text-sm text-muted-foreground italic">{tr('decisionsSection.noData')}</p>;
  const pending = Array.isArray(data.pending) ? data.pending : [];
  const pendingCount = typeof data.pending_count === 'number' ? data.pending_count : pending.length;
  const graceMinutes = typeof data.grace_minutes === 'number' ? data.grace_minutes : 0;

  return (
    <div className="space-y-4">
      {/* Top stats */}
      <div className="flex flex-wrap items-center gap-4 text-sm">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">{tr('decisionsSection.autoExecute')}</span>
          <Badge
            className={`text-xs ${data.auto_execute_enabled ? 'bg-green-500/20 text-green-400 border-green-500/30' : 'bg-gray-500/20 text-gray-400 border-gray-500/30'}`}
          >
            {data.auto_execute_enabled ? 'ON' : 'OFF'}
          </Badge>
          <span className="text-muted-foreground text-xs">{tr('decisions.graceMinutes', { count: graceMinutes })}</span>
          <Badge variant="outline" className="text-xs">{data.policy_source || '—'}</Badge>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">{tr('decisionsSection.pending')}</span>
          <span className="font-mono font-bold">{pendingCount}</span>
        </div>
      </div>

      {/* Last decision summary */}
      {data.last_route && (
        <div className="bg-muted/40 rounded-lg p-3 space-y-1.5">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">{tr('decisionsSection.lastDecision')}</div>
          <div className="flex items-center gap-2 flex-wrap">
            <Badge className={`text-xs ${routeStyle(data.last_route)}`}>
              {tr(routeLabel(data.last_route))}
            </Badge>
            {data.last_decision_kind && (
              <span className="text-sm font-medium">{data.last_decision_kind}</span>
            )}
            {data.last_routed_at && (
              <span className="text-xs text-muted-foreground font-mono">{relativeTime(data.last_routed_at)}</span>
            )}
          </div>
          {data.last_reasoning && (
            <p className="text-xs text-muted-foreground">{data.last_reasoning}</p>
          )}
        </div>
      )}

      {/* Pending list */}
      {pending.length > 0 && (
        <div className="space-y-2">
          <div className="text-xs text-muted-foreground">{tr('decisionsSection.pendingItems')}</div>
          {pending.map(d => (
            <div key={d.decision_id} className="bg-muted/40 rounded-lg p-3 space-y-1.5">
              <div className="flex items-center gap-2 flex-wrap">
                <Badge className={`text-xs ${routeStyle(d.route)}`}>{tr(routeLabel(d.route))}</Badge>
                <span className="text-sm font-medium">{d.decision_kind}</span>
                <span className="text-xs text-muted-foreground font-mono">{relativeTime(d.proposed_at)}</span>
              </div>
              <p className="text-xs text-muted-foreground">{d.reasoning}</p>
              {d.auto_execute_at && (
                <div className="text-xs text-orange-400 font-mono">
                  {tr('decisionsSection.autoExecuteScheduled')} {relativeTime(d.auto_execute_at)}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <p className="text-xs text-muted-foreground italic">
        {tr('decisionsSection.phase3bNote')}
      </p>
    </div>
  );
}
