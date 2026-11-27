import { useState } from 'react';
import { Controllers } from '../lib/api';
import { useLanguage } from '../lib/language-core';

/**
 * 화면 하단 고정 급이 비상 정지 버튼 (2026-07-07 운영자 요구).
 *
 * 모든 페이지에서 항상 보이며, 확인 1회 후 **모든 feeder 컨트롤러에 즉시 stop** 을
 * 보낸다. 사이클 활성 여부와 무관 — backend 원시 stop 이 활성 사이클도 함께
 * 정지시키고(2026-07-04 일원화), 사이클 없이 도는 방치 run(2026-07-07 실사고)도
 * 이 버튼이 잡는다.
 */
export function EmergencyFeedStop() {
  const { tr } = useLanguage();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | null>(null);

  async function stopAll() {
    setBusy(true);
    setResult(null);
    try {
      const list = await Controllers.list();
      const feeders = (list.items ?? []).filter(c => c.controller_id.startsWith('feeder'));
      const results = await Promise.allSettled(
        feeders.map(c => Controllers.feed(c.controller_id, 'stop', true)),
      );
      const ok = results.filter(r => r.status === 'fulfilled').length;
      setResult(tr('emergencyStop.done', { ok, total: feeders.length }));
    } catch {
      setResult(tr('emergencyStop.failed'));
    } finally {
      setBusy(false);
      setConfirming(false);
      window.setTimeout(() => setResult(null), 6000);
    }
  }

  return (
    <div className="fixed bottom-3 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2">
      {result && (
        <span className="px-3 py-1.5 text-xs rounded-md bg-gray-900/95 border border-gray-600 text-gray-100 shadow-lg">
          {result}
        </span>
      )}
      {confirming ? (
        <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-gray-900/95 border border-red-500/70 shadow-xl">
          <span className="text-xs text-red-200">{tr('emergencyStop.confirmMsg')}</span>
          <button
            onClick={() => void stopAll()}
            disabled={busy}
            className="px-3 py-1 text-xs font-bold rounded bg-red-600 hover:bg-red-500 text-white disabled:opacity-50"
          >
            {busy ? tr('emergencyStop.stopping') : tr('emergencyStop.confirmBtn')}
          </button>
          <button
            onClick={() => setConfirming(false)}
            disabled={busy}
            className="px-2 py-1 text-xs rounded border border-gray-600 text-gray-300 hover:bg-gray-800"
          >
            {tr('emergencyStop.cancel')}
          </button>
        </div>
      ) : (
        <button
          onClick={() => setConfirming(true)}
          title={tr('emergencyStop.hint')}
          className="px-4 py-1.5 text-xs font-semibold rounded-full bg-red-950/80 border border-red-500/60 text-red-200 hover:bg-red-900/90 hover:border-red-400 shadow-lg backdrop-blur-sm"
        >
          🛑 {tr('emergencyStop.button')}
        </button>
      )}
    </div>
  );
}
