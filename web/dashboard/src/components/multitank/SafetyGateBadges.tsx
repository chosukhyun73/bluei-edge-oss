import { useCallback, useEffect, useState } from 'react';
import { SafetyGates } from '../../lib/api';
import type { SafetyGatesStatus, SafetyGateStatus } from '../../lib/types';
import { useLanguage } from '../../lib/language-context';

// C-5: 신규 사이클 시작 폼에 띄우는 안전 게이트 3종 배지.
// 운영자가 의도 메모를 입력하는 동안 "지금 어떤 안전 규칙이 살아 있나" 확인.
// FeedCycleMonitor 의 NewCycleForm intent textarea 바로 아래에 배치.

const POLL_INTERVAL_MS = 30_000; // 의도 입력 폼이라 너무 빈번하면 거슬림

interface SafetyGateBadgesProps {
  tankId: string;
}

function statusColor(s: SafetyGateStatus): {
  badge: string;
  dot: string;
  text: string;
} {
  switch (s) {
    case 'ok':
      return {
        badge: 'border-green-500/40 bg-green-500/10',
        dot: 'bg-green-400',
        text: 'text-green-200',
      };
    case 'caution':
      return {
        badge: 'border-amber-500/40 bg-amber-500/10',
        dot: 'bg-amber-400',
        text: 'text-amber-200',
      };
    case 'breach':
      return {
        badge: 'border-red-500/40 bg-red-500/10',
        dot: 'bg-red-400',
        text: 'text-red-200',
      };
    case 'active':
      // learned 게이트: enabled rules 가 있다는 뜻 — 정상 가동.
      return {
        badge: 'border-blue-500/40 bg-blue-500/10',
        dot: 'bg-blue-400',
        text: 'text-blue-200',
      };
    case 'na':
    default:
      return {
        badge: 'border-gray-700/50 bg-gray-800/40',
        dot: 'bg-gray-600',
        text: 'text-gray-400',
      };
  }
}

interface BadgeProps {
  title: string;
  status: SafetyGateStatus;
  detail: string;
}

function Badge({ title, status, detail }: BadgeProps) {
  const c = statusColor(status);
  return (
    <div
      className={[
        'flex flex-col gap-0.5 px-3 py-2 rounded-md border min-w-[140px]',
        c.badge,
      ].join(' ')}
      role="status"
    >
      <div className="flex items-center gap-1.5">
        <span className={['w-1.5 h-1.5 rounded-full', c.dot].join(' ')} aria-hidden />
        <span className="text-[11px] uppercase tracking-wide text-gray-400 font-medium">
          {title}
        </span>
      </div>
      <span className={['text-xs leading-tight', c.text].join(' ')}>
        {detail}
      </span>
    </div>
  );
}

export function SafetyGateBadges({ tankId }: SafetyGateBadgesProps) {
  const { tr } = useLanguage();
  const [data, setData] = useState<SafetyGatesStatus | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchStatus = useCallback(async () => {
    if (!tankId) return;
    setError(null);
    try {
      const res = await SafetyGates.status(tankId);
      setData(res);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : tr('safetyGateBadges.unknownError');
      setError(msg);
    }
  }, [tankId, tr]);

  useEffect(() => {
    void fetchStatus();
    if (!tankId) return;
    const t = setInterval(() => { void fetchStatus(); }, POLL_INTERVAL_MS);
    return () => clearInterval(t);
  }, [fetchStatus, tankId]);

  if (!tankId) return null;

  if (error) {
    return (
      <p className="text-xs text-destructive font-mono">
        {tr('safetyGateBadges.fetchError')}: {error}
      </p>
    );
  }

  if (!data) {
    return (
      <p className="text-xs text-gray-500">{tr('safetyGateBadges.loading')}</p>
    );
  }

  // predictive 상세: NH3 headroom 또는 summary
  const predictiveDetail = (() => {
    if (data.predictive.status === 'na') return data.predictive.summary;
    if (data.predictive.headroom_kg_per_h !== undefined) {
      return tr('safetyGate.headroom', { v: data.predictive.headroom_kg_per_h.toFixed(2) });
    }
    return data.predictive.summary;
  })();

  // learned 상세: 활성 규칙 개수
  const learnedDetail = (() => {
    if (data.learned.status === 'na') return data.learned.summary;
    if (data.learned.rules_enabled === 0) return tr('safetyGateBadges.noLearnedRules');
    return `${tr('safetyGateBadges.rulesActive_prefix')}${data.learned.rules_enabled}${tr('safetyGateBadges.rulesActive_suffix')}`;
  })();

  // environmental 상세: summary (풍속/파고 또는 'na' 메시지)
  const envDetail = data.environmental.summary;

  return (
    <div className="flex flex-wrap gap-2">
      <Badge
        title={tr('safetyGateBadges.titlePredictive')}
        status={data.predictive.status}
        detail={predictiveDetail}
      />
      <Badge
        title={tr('safetyGateBadges.titleLearned')}
        status={data.learned.status}
        detail={learnedDetail}
      />
      <Badge
        title={tr('safetyGateBadges.titleEnvironmental')}
        status={data.environmental.status}
        detail={envDetail}
      />
    </div>
  );
}
