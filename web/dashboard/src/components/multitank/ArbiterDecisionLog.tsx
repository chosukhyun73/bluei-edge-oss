import { useCallback, useEffect, useMemo, useState } from 'react';
import { ArbiterDecisions } from '../../lib/api';
import { relativeTime } from '../../lib/format';
import type { ArbiterDecision, ArbiterPriority } from '../../lib/types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card';
import { Button } from '../ui/button';
import { Skeleton } from '../ui/skeleton';
import { useLanguage } from '../../lib/language-context';

// C-4: Arbiter 결정 audit 로그.
// 5-G Progressive Autonomy 의 priority(manual_override / ai_advisory / ai_autonomous)
// 와 decision(accept / reject / preempt) 흐름을 한눈에 본다.
// LearnedSafetyPanel 안 "안전학습" sub-tab 의 마지막 섹션으로 배치.

const POLL_INTERVAL_MS = 10_000;
const LIST_LIMIT = 50;
const CONFLICT_WINDOW_MS = 5_000;

const PRIORITY_FILTER_OPTIONS: { value: '' | ArbiterPriority; trKey: string }[] = [
  { value: '', trKey: 'arbiterLog.filterAll' },
  { value: 'manual_override', trKey: 'arbiterLog.priorityManual' },
  { value: 'ai_advisory', trKey: 'arbiterLog.priorityAdvisory' },
  { value: 'ai_autonomous', trKey: 'arbiterLog.priorityAutonomous' },
];

// returns a tr key, or null when value is unknown (caller falls back to raw value).
function priorityLabelKey(p: string): string | null {
  switch (p) {
    case 'manual_override': return 'arbiterLog.priorityManual';
    case 'ai_advisory':     return 'arbiterLog.priorityAdvisory';
    case 'ai_autonomous':   return 'arbiterLog.priorityAutonomous';
    default:                return null;
  }
}

function decisionLabelKey(verb: string): string | null {
  switch (verb) {
    case 'accept':  return 'arbiterLog.decisionAccept';
    case 'reject':  return 'arbiterLog.decisionReject';
    case 'preempt': return 'arbiterLog.decisionPreempt';
    default:        return null;
  }
}

function priorityBadgeCls(p: string): string {
  switch (p) {
    case 'manual_override':
      return 'border-amber-500/40 text-amber-300 bg-amber-500/10';
    case 'ai_advisory':
      return 'border-blue-500/40 text-blue-300 bg-blue-500/10';
    case 'ai_autonomous':
      return 'border-violet-500/40 text-violet-300 bg-violet-500/10';
    default:
      return 'border-gray-600/40 text-gray-300 bg-gray-600/10';
  }
}

function decisionBadgeCls(verb: string): string {
  switch (verb) {
    case 'accept':
      return 'border-green-500/40 text-green-300 bg-green-500/10';
    case 'reject':
      return 'border-red-500/40 text-red-300 bg-red-500/10';
    case 'preempt':
      return 'border-orange-500/40 text-orange-300 bg-orange-500/10';
    default:
      return 'border-gray-600/40 text-gray-300 bg-gray-600/10';
  }
}

// Conflict 식별:
//   1) preempted_cycle_id 가 있으면 즉시 conflict.
//   2) 같은 tank_id 안에서 priority 가 다른 두 decision 의 recorded_at 차가 5초 이내.
// 두 경우의 union 을 conflict set 으로 반환.
function detectConflicts(items: ArbiterDecision[]): Set<string> {
  const conflicts = new Set<string>();

  for (const d of items) {
    if (d.preempted_cycle_id) {
      conflicts.add(d.decision_id);
    }
  }

  const byTank = new Map<string, ArbiterDecision[]>();
  for (const d of items) {
    if (!d.tank_id) continue;
    const arr = byTank.get(d.tank_id) ?? [];
    arr.push(d);
    byTank.set(d.tank_id, arr);
  }
  for (const arr of byTank.values()) {
    arr.sort((a, b) => Date.parse(a.recorded_at) - Date.parse(b.recorded_at));
    for (let i = 0; i < arr.length; i++) {
      const a = arr[i];
      for (let j = i + 1; j < arr.length; j++) {
        const b = arr[j];
        const dt = Date.parse(b.recorded_at) - Date.parse(a.recorded_at);
        if (dt > CONFLICT_WINDOW_MS) break;
        if (a.priority !== b.priority) {
          conflicts.add(a.decision_id);
          conflicts.add(b.decision_id);
        }
      }
    }
  }

  return conflicts;
}

interface ArbiterDecisionLogProps {
  tankIds?: string[]; // optional dropdown — 미지정 시 input 없음
}

export function ArbiterDecisionLog({ tankIds }: ArbiterDecisionLogProps = {}) {
  const { tr } = useLanguage();
  const [items, setItems] = useState<ArbiterDecision[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [priorityFilter, setPriorityFilter] = useState<'' | ArbiterPriority>('');
  const [tankFilter, setTankFilter] = useState<string>('');
  const [lastFetchedAt, setLastFetchedAt] = useState<Date | null>(null);

  const fetchDecisions = useCallback(async () => {
    setError(null);
    try {
      const res = await ArbiterDecisions.list({
        limit: LIST_LIMIT,
        priority: priorityFilter || undefined,
        tankId: tankFilter || undefined,
      });
      setItems(res.items ?? []);
      setLastFetchedAt(new Date());
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('arbiterLog.unknownError');
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [priorityFilter, tankFilter]);

  useEffect(() => {
    void fetchDecisions();
    const t = setInterval(() => { void fetchDecisions(); }, POLL_INTERVAL_MS);
    return () => clearInterval(t);
  }, [fetchDecisions]);

  const conflicts = useMemo(() => detectConflicts(items), [items]);

  return (
    <Card className="bg-gray-900/60 border-gray-700/50">
      <CardHeader className="pb-0 border-b border-gray-800">
        <div className="flex flex-wrap items-center justify-between gap-2 py-1">
          <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide">
            {tr('arbiterLog.title')}
            {!loading && (
              <span className="ml-2 text-gray-500 font-normal">({items.length})</span>
            )}
          </CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <select
              value={priorityFilter}
              onChange={e => setPriorityFilter(e.target.value as '' | ArbiterPriority)}
              className="text-xs px-2 py-1 rounded-md border border-gray-700 bg-gray-900 text-gray-200"
              aria-label={tr('arbiterLog.filterPriorityAriaLabel')}
            >
              {PRIORITY_FILTER_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{tr(o.trKey)}</option>
              ))}
            </select>
            {tankIds && tankIds.length > 0 && (
              <select
                value={tankFilter}
                onChange={e => setTankFilter(e.target.value)}
                className="text-xs px-2 py-1 rounded-md border border-gray-700 bg-gray-900 text-gray-200"
                aria-label={tr('arbiterLog.filterTankAriaLabel')}
              >
                <option value="">{tr('arbiterLog.filterAllTanks')}</option>
                {tankIds.map(t => (
                  <option key={t} value={t}>{t}</option>
                ))}
              </select>
            )}
            <Button
              size="sm"
              variant="outline"
              onClick={() => void fetchDecisions()}
              aria-label={tr('arbiterLog.refresh')}
            >
              {tr('arbiterLog.refresh')}
            </Button>
          </div>
        </div>
        {lastFetchedAt && (
          <p className="pb-2 text-xs text-gray-500">
            {tr('arbiterLog.lastUpdated')}: {relativeTime(lastFetchedAt.toISOString())}
          </p>
        )}
      </CardHeader>
      <CardContent className="p-0">
        {error && (
          <div className="px-4 py-3 bg-destructive/10 border-b border-destructive/30 text-sm text-destructive font-mono">
            {tr('arbiterLog.errorPrefix')}: {error}
          </div>
        )}
        {loading ? (
          <div className="p-4 space-y-2">
            {[1, 2, 3].map(i => <Skeleton key={i} className="h-8 w-full" />)}
          </div>
        ) : items.length === 0 ? (
          <p className="py-10 text-center text-sm text-gray-500">
            {tr('arbiterLog.empty')}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm" aria-label={tr('arbiterLog.title')}>
              <thead>
                <tr className="border-b border-gray-800 text-xs text-gray-500 uppercase tracking-wide">
                  <th className="text-left px-4 py-3 font-medium">{tr('arbiterLog.colTime')}</th>
                  <th className="text-left px-4 py-3 font-medium">{tr('arbiterLog.colCageTank')}</th>
                  <th className="text-left px-4 py-3 font-medium">{tr('arbiterLog.colPriority')}</th>
                  <th className="text-left px-4 py-3 font-medium">{tr('arbiterLog.colDecision')}</th>
                  <th className="text-left px-4 py-3 font-medium">{tr('arbiterLog.colDetail')}</th>
                  <th className="text-left px-4 py-3 font-medium">decision_id</th>
                </tr>
              </thead>
              <tbody>
                {items.map(d => {
                  const isConflict = conflicts.has(d.decision_id);
                  return (
                    <tr
                      key={d.decision_id}
                      className={[
                        'border-b border-gray-800/50 transition-colors',
                        isConflict
                          ? 'bg-orange-500/10 hover:bg-orange-500/15'
                          : 'hover:bg-gray-800/30',
                      ].join(' ')}
                    >
                      <td className="px-4 py-3 text-xs text-gray-400 whitespace-nowrap">
                        {relativeTime(d.recorded_at)}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-300 font-mono">
                        {d.tank_id || '—'}
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={[
                            'inline-flex items-center px-2 py-0.5 rounded-md border text-xs font-medium',
                            priorityBadgeCls(d.priority),
                          ].join(' ')}
                        >
                          {priorityLabelKey(d.priority) ? tr(priorityLabelKey(d.priority)!) : d.priority}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={[
                            'inline-flex items-center px-2 py-0.5 rounded-md border text-xs font-medium',
                            decisionBadgeCls(d.decision),
                          ].join(' ')}
                        >
                          {decisionLabelKey(d.decision) ? tr(decisionLabelKey(d.decision)!) : d.decision}
                        </span>
                        {isConflict && (
                          <span className="ml-2 text-[11px] text-orange-300">
                            ⚠ {tr('arbiterLog.conflict')}
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-400 max-w-xs">
                        {d.preempted_cycle_id && (
                          <span className="font-mono text-orange-300">
                            {tr('arbiterLog.preemptedPrefix')}: {d.preempted_cycle_id.slice(-12)}
                          </span>
                        )}
                        {!d.preempted_cycle_id && d.rejection_reason && (
                          <span className="text-red-300">
                            {d.rejection_reason}
                          </span>
                        )}
                        {!d.preempted_cycle_id && !d.rejection_reason && d.resulting_cycle_id && (
                          <span className="font-mono text-gray-500">
                            cycle: {d.resulting_cycle_id.slice(-12)}
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-600 font-mono">
                        {d.decision_id.slice(-12)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
