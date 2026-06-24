import { useState, useEffect } from 'react';
import { Alerts } from '../lib/api';
import type { AlertOpen } from '../lib/types';
import { useLanguage } from '../lib/language-context';

// GlobalAlerts — 헤더 아래 전역 알림 banner.
// open_alerts 5초 polling. 알림 0건이면 hidden. severity 별 색.
// × 버튼: backend close API 호출 → open_alerts 에서 삭제 + alert.updated 적재.
// 자동 close 없음 (사용자 정책 2026-05-22). 같은 조건 재발화 시 새 alert 등장.
export function GlobalAlerts() {
  const { tr } = useLanguage();
  const [alerts, setAlerts] = useState<AlertOpen[]>([]);
  const [closing, setClosing] = useState<Set<string>>(new Set());

  useEffect(() => {
    let cancelled = false;
    const fetch = async () => {
      try {
        const res = await Alerts.listOpen();
        if (!cancelled) setAlerts(res.alerts);
      } catch {
        // non-fatal: keep prev state
      }
    };
    void fetch();
    const interval = setInterval(() => void fetch(), 5000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  async function handleClose(alertID: string) {
    setClosing((prev) => new Set(prev).add(alertID));
    try {
      await Alerts.close(alertID);
      // 즉시 local list 에서 제거 — 다음 polling 까지 기다리지 않음.
      setAlerts((prev) => prev.filter((a) => a.alert_id !== alertID));
    } catch {
      // 실패 시 closing 표시만 해제 — 사용자가 재시도 가능.
    } finally {
      setClosing((prev) => {
        const next = new Set(prev);
        next.delete(alertID);
        return next;
      });
    }
  }

  const visible = alerts;
  if (visible.length === 0) return null;

  return (
    <div className="border-t border-gray-800">
      {visible.map((a) => {
        const sev = a.severity?.toLowerCase();
        const isCritical = sev === 'critical';
        const bgClasses = isCritical
          ? 'bg-red-500/15 border-red-500/40 text-red-200'
          : 'bg-orange-500/15 border-orange-500/40 text-orange-200';
        const label = isCritical ? '🔴 CRITICAL' : '⚠️ WARNING';

        return (
          <div
            key={a.alert_id}
            className={'flex items-center justify-between px-6 py-2 border-b ' + bgClasses}
            role="alert"
          >
            <div className="flex-1 text-sm">
              <span className="font-semibold uppercase tracking-wide mr-2">{label}</span>
              <span>{a.message}</span>
              <span className="ml-3 text-xs opacity-70 font-mono">
                {a.subject?.kind}:{a.subject?.id} · {new Date(a.raised_at).toLocaleTimeString('ko-KR')}
              </span>
            </div>
            <button
              type="button"
              disabled={closing.has(a.alert_id)}
              onClick={() => void handleClose(a.alert_id)}
              className="text-gray-400 hover:text-gray-100 disabled:opacity-30 text-xl leading-none px-2 -my-1"
              aria-label={tr('globalAlerts.closeAriaLabel')}
              title={tr('globalAlerts.closeTitle')}
            >
              {closing.has(a.alert_id) ? '…' : '×'}
            </button>
          </div>
        );
      })}
    </div>
  );
}
