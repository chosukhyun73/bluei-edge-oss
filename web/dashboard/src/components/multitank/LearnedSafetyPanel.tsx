import { useCallback, useEffect, useState } from 'react';
import { ArbiterDecisionLog } from './ArbiterDecisionLog';
import { Disputes, LearnedRules } from '../../lib/api';
import {
  disputeTypeLabel,
  formatLearnedCondition,
  relativeTime,
  severityLabel,
} from '../../lib/format';
import type { Dispute, LearnedRule } from '../../lib/types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card';
import { Button } from '../ui/button';
import { Skeleton } from '../ui/skeleton';
import { useLanguage } from '../../lib/language-context';

// C-3l: 학습 규칙 관리 + 운영자 dispute 누적 + mining 트리거 패널.
// MultiTankOperations 의 5번째 sub-tab "안전학습" 안에 배치.

const POLL_INTERVAL_MS = 10_000;

// 최근 7일 dispute 만 카운트 (mining 윈도우와 일치).
function recentDisputeCount(disputes: Dispute[]): number {
  const cutoff = Date.now() - 7 * 24 * 3600 * 1000;
  return disputes.filter(d => new Date(d.disputed_at).getTime() >= cutoff).length;
}

// Mining 임계 — backend internal/learned_safety/mining.go 의 mineMinHits 와 동치.
const MINE_MIN_HITS = 3;
const MINE_WINDOW_MS = 7 * 24 * 3600 * 1000;

// Backend mining.go 의 metricPattern 과 동일. (metric, op, threshold) 패턴 추출.
const PATTERN_REGEX = /([a-z_]+)\s*([><]=?)\s*(\d+(?:\.\d+)?)/gi;

interface MiningPreview {
  maxPatternCount: number;
  readyPatternCount: number;
  topPattern: string | null;
  patternsFound: number;
}

// frontend mining preview — backend regex 와 같은 알고리즘을 dry-run.
// rules_inserted 가 나오기 전까지 운영자에게 진척도를 보여준다.
function computeMiningPreview(disputes: Dispute[]): MiningPreview {
  const cutoff = Date.now() - MINE_WINDOW_MS;
  const counts = new Map<string, number>();

  for (const d of disputes) {
    if (new Date(d.disputed_at).getTime() < cutoff) continue;
    const text = `${d.comment ?? ''} ${d.dispute_type ?? ''}`;
    PATTERN_REGEX.lastIndex = 0;
    for (const m of text.matchAll(PATTERN_REGEX)) {
      const key = `${m[1].toLowerCase()}_${m[2]}_${m[3]}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
  }

  let maxCount = 0;
  let topKey: string | null = null;
  let readyCount = 0;
  for (const [k, v] of counts) {
    if (v > maxCount) { maxCount = v; topKey = k; }
    if (v >= MINE_MIN_HITS) readyCount += 1;
  }

  return {
    maxPatternCount: maxCount,
    readyPatternCount: readyCount,
    topPattern: topKey,
    patternsFound: counts.size,
  };
}

// 패턴 키 (metric_op_threshold) → 사람이 읽는 형식.
function formatPatternKey(key: string): string {
  const parts = key.split('_');
  if (parts.length < 3) return key;
  const threshold = parts[parts.length - 1];
  const op = parts[parts.length - 2];
  const metric = parts.slice(0, parts.length - 2).join('_');
  return `${metric} ${op} ${threshold}`;
}

export function LearnedSafetyPanel() {
  const { tr } = useLanguage();
  const [rules, setRules] = useState<LearnedRule[]>([]);
  const [disputes, setDisputes] = useState<Dispute[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [mining, setMining] = useState(false);
  const [mineResult, setMineResult] = useState<string | null>(null);
  const [togglingId, setTogglingId] = useState<string | null>(null);

  const fetchAll = useCallback(async () => {
    setError(null);
    try {
      const [rulesRes, disputesRes] = await Promise.all([
        LearnedRules.list(),
        Disputes.list(200),
      ]);
      setRules(rulesRes.items ?? []);
      setDisputes(disputesRes.items ?? []);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('learnedSafetyPanel.unknownError');
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchAll();
    const t = setInterval(() => { void fetchAll(); }, POLL_INTERVAL_MS);
    return () => clearInterval(t);
  }, [fetchAll]);

  async function handleMine() {
    setMining(true);
    setMineResult(null);
    setError(null);
    try {
      const res = await LearnedRules.mine();
      const skipped = res.rules_skipped ?? 0;
      const skippedPart = skipped > 0 ? tr('learnedSafety.mineSkipped', { n: skipped }) : '';
      setMineResult(
        tr('learnedSafety.mineResult', {
          checked: res.disputes_checked,
          inserted: res.rules_inserted,
          skipped: skippedPart,
          mined: res.rules_mined,
        }),
      );
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('learnedSafetyPanel.unknownError');
      setError(msg);
    } finally {
      setMining(false);
    }
  }

  async function handleToggle(rule: LearnedRule) {
    setTogglingId(rule.rule_id);
    try {
      if (rule.enabled) {
        await LearnedRules.disable(rule.rule_id);
      } else {
        await LearnedRules.enable(rule.rule_id);
      }
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('learnedSafetyPanel.unknownError');
      setError(msg);
    } finally {
      setTogglingId(null);
    }
  }

  const enabledCount = rules.filter(r => r.enabled).length;
  const recentDisputes = recentDisputeCount(disputes);
  const preview = computeMiningPreview(disputes);
  const miningReady = preview.readyPatternCount > 0;

  return (
    <div className="space-y-4">
      {/* ── Progress banner ────────────────────────────────────────────────── */}
      <Card className="bg-gray-900/60 border-blue-500/20">
        <CardContent className="py-4 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-6">
            <div>
              <p className="text-xs text-gray-500 uppercase tracking-wide">{tr('learnedSafetyPanel.recentDisputesLabel')}</p>
              <p className="text-2xl font-semibold text-white mt-0.5">
                {recentDisputes}
                <span className="text-sm text-gray-500 font-normal ml-1">{tr('learnedSafetyPanel.unitCases')}</span>
              </p>
            </div>
            <div>
              <p className="text-xs text-gray-500 uppercase tracking-wide">{tr('learnedSafetyPanel.totalLearnedRules')}</p>
              <p className="text-2xl font-semibold text-white mt-0.5">
                {rules.length}
                <span className="text-sm text-gray-500 font-normal ml-1">
                  ({enabledCount} {tr('learnedSafetyPanel.activeCount')})
                </span>
              </p>
            </div>
          </div>
          <div className="flex flex-col gap-1 items-end">
            <Button
              size="sm"
              onClick={() => void handleMine()}
              disabled={mining}
              aria-label={tr('learnedSafetyPanel.miningTriggerAriaLabel')}
            >
              {mining ? tr('learnedSafetyPanel.miningInProgress') : tr('learnedSafetyPanel.miningStart')}
            </Button>
            {!loading && !mineResult && preview.patternsFound > 0 && (
              <p
                className={[
                  'text-xs',
                  miningReady ? 'text-green-300' : 'text-amber-300',
                ].join(' ')}
                aria-label={tr('learnedSafetyPanel.miningProgressAriaLabel')}
              >
                {miningReady
                  ? `${tr('learnedSafetyPanel.miningReady')} ${preview.readyPatternCount}${tr('learnedSafetyPanel.miningReadyPatterns')}`
                  : `${tr('learnedSafetyPanel.miningTopPattern')} ${preview.maxPatternCount}/${MINE_MIN_HITS}${tr('learnedSafetyPanel.unitCases')}`}
                {preview.topPattern && (
                  <span className="ml-1 text-gray-500 font-mono">
                    ({formatPatternKey(preview.topPattern)})
                  </span>
                )}
              </p>
            )}
            {!loading && !mineResult && preview.patternsFound === 0 && recentDisputes > 0 && (
              <p className="text-xs text-gray-500" aria-label={tr('learnedSafetyPanel.miningNoPatternsAriaLabel')}>
                {tr('learnedSafetyPanel.miningNoPatterns')}
              </p>
            )}
            {mineResult && (
              <p className="text-xs text-green-300">{mineResult}</p>
            )}
          </div>
        </CardContent>
      </Card>

      {error && (
        <div className="px-4 py-3 bg-destructive/10 border border-destructive/30 rounded-lg text-sm text-destructive font-mono">
          {tr('learnedSafetyPanel.errorPrefix')} {error}
        </div>
      )}

      {/* ── Learned rules list ────────────────────────────────────────────── */}
      <Card className="bg-gray-900/60 border-gray-700/50">
        <CardHeader className="pb-0 border-b border-gray-800">
          <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide py-1">
            {tr('learnedSafetyPanel.learnedRulesTitle')}
            {!loading && (
              <span className="ml-2 text-gray-500 font-normal">({rules.length})</span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="p-5 space-y-2">
              {[1, 2].map(i => <Skeleton key={i} className="h-12 w-full" />)}
            </div>
          ) : rules.length === 0 ? (
            <div className="py-10 px-6 text-center">
              <p className="text-sm text-gray-400">{tr('learnedSafetyPanel.noRules')}</p>
              <p className="text-xs text-gray-600 mt-2 leading-relaxed">
                {tr('learnedSafetyPanel.noRulesHint1')}<br />
                <span className="text-gray-400">{tr('learnedSafetyPanel.noRulesDisputeAction')}</span> {tr('learnedSafetyPanel.noRulesHint2')} <span className="text-gray-400">{tr('learnedSafetyPanel.noRulesMineButton')}</span> {tr('learnedSafetyPanel.noRulesHint3')}
              </p>
            </div>
          ) : (
            <ul aria-label={tr('learnedSafetyPanel.ruleListAriaLabel')}>
              {rules.map(rule => {
                const sev = severityLabel(rule.severity);
                return (
                  <li
                    key={rule.rule_id}
                    className="flex items-start gap-3 px-4 py-3 border-b border-gray-800/50 last:border-b-0"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span
                          className={[
                            'inline-flex items-center px-2 py-0.5 rounded-md border text-xs font-medium',
                            sev.cls,
                          ].join(' ')}
                        >
                          {sev.text}
                        </span>
                        <span className="text-sm text-white font-medium">
                          {formatLearnedCondition(rule.condition_json)}
                        </span>
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-gray-500">
                        <span>{tr('learnedSafetyPanel.confidence')} {(rule.confidence * 100).toFixed(0)}%</span>
                        <span>{tr('learnedSafetyPanel.hitCount')} {rule.hit_count}{tr('learnedSafetyPanel.unitTimes')}</span>
                        <span>{tr('learnedSafetyPanel.source')} {rule.source}</span>
                        <span>{relativeTime(rule.created_at)}</span>
                        <span className="font-mono text-gray-600">
                          {rule.rule_id.slice(-12)}
                        </span>
                      </div>
                    </div>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={togglingId === rule.rule_id}
                      onClick={() => void handleToggle(rule)}
                      className={
                        rule.enabled
                          ? 'border-amber-500/40 text-amber-300 hover:bg-amber-500/10'
                          : 'border-green-500/40 text-green-300 hover:bg-green-500/10'
                      }
                      aria-label={
                        rule.enabled
                          ? `${tr('learnedSafetyPanel.ruleAriaLabelPrefix')} ${rule.rule_id} ${tr('learnedSafetyPanel.disableAction')}`
                          : `${tr('learnedSafetyPanel.ruleAriaLabelPrefix')} ${rule.rule_id} ${tr('learnedSafetyPanel.enableAction')}`
                      }
                    >
                      {togglingId === rule.rule_id
                        ? '...'
                        : rule.enabled
                        ? tr('learnedSafetyPanel.disableAction')
                        : tr('learnedSafetyPanel.enableAction')}
                    </Button>
                  </li>
                );
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      {/* ── Recent disputes timeline ──────────────────────────────────────── */}
      <Card className="bg-gray-900/60 border-gray-700/50">
        <CardHeader className="pb-0 border-b border-gray-800">
          <CardTitle className="text-sm font-semibold text-gray-300 uppercase tracking-wide py-1">
            {tr('learnedSafetyPanel.recentDisputesTitle')}
            {!loading && (
              <span className="ml-2 text-gray-500 font-normal">({disputes.length})</span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {loading ? (
            <div className="p-4 space-y-2">
              {[1, 2].map(i => <Skeleton key={i} className="h-8 w-full" />)}
            </div>
          ) : disputes.length === 0 ? (
            <p className="py-8 text-center text-sm text-gray-500">
              {tr('learnedSafetyPanel.noDisputes')}
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" aria-label={tr('learnedSafetyPanel.disputeTimelineAriaLabel')}>
                <thead>
                  <tr className="border-b border-gray-800 text-xs text-gray-500 uppercase tracking-wide">
                    <th className="text-left px-4 py-3 font-medium">{tr('learnedSafetyPanel.colTime')}</th>
                    <th className="text-left px-4 py-3 font-medium">Cage/Tank</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('learnedSafetyPanel.colType')}</th>
                    <th className="text-left px-4 py-3 font-medium">{tr('learnedSafetyPanel.colReason')}</th>
                    <th className="text-left px-4 py-3 font-medium">decision_id</th>
                  </tr>
                </thead>
                <tbody>
                  {disputes.slice(0, 30).map(d => (
                    <tr
                      key={d.dispute_id}
                      className="border-b border-gray-800/50 hover:bg-gray-800/30 transition-colors"
                    >
                      <td className="px-4 py-3 text-xs text-gray-400 whitespace-nowrap">
                        {relativeTime(d.disputed_at)}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-300 font-mono">
                        {d.tank_id}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-300">
                        {disputeTypeLabel(d.dispute_type)}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-400 max-w-xs truncate">
                        {d.comment || '—'}
                      </td>
                      <td className="px-4 py-3 text-xs text-gray-600 font-mono">
                        {d.decision_id.slice(-12)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── Arbiter decision log (C-4) ────────────────────────────────────── */}
      <ArbiterDecisionLog />
    </div>
  );
}
