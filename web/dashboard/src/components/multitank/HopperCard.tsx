import { useCallback, useEffect, useState } from 'react';
import { Tanks } from '../../lib/api';
import type { HopperEstimate, Tank } from '../../lib/types';
import { useLanguage } from '../../lib/language-core';

// 호퍼 잔량 카드 (2026-07-17 사료 소진 사태 재발 방지)
// 사료공급 탭 상단 — 수조별 추정 잔량 + [보충 기록]. 추정은 보충 기록(운영자 입력)
// − 사이클 기록 급이량. 보충 기록이 없으면 추정 불가 안내.

const POLL_MS = 60_000;

export function HopperCard() {
  const { tr } = useLanguage();
  const [tanks, setTanks] = useState<Tank[]>([]);
  const [estimates, setEstimates] = useState<Record<string, HopperEstimate>>({});
  const [refillKg, setRefillKg] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const r = await Tanks.list();
      const items = r.items ?? [];
      setTanks(items);
      const next: Record<string, HopperEstimate> = {};
      await Promise.all(items.map(async t => {
        try {
          next[t.tank_id] = await Tanks.hopper(t.tank_id);
        } catch { /* 수조 단위 실패는 무시 */ }
      }));
      setEstimates(next);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, POLL_MS);
    return () => clearInterval(id);
  }, [refresh]);

  async function handleRefill(tankID: string) {
    const kg = parseFloat(refillKg[tankID] ?? '');
    if (!Number.isFinite(kg) || kg <= 0 || kg > 50) return;
    setBusy(tankID);
    try {
      await Tanks.hopperRefill(tankID, Math.round(kg * 1000));
      setRefillKg(prev => ({ ...prev, [tankID]: '' }));
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  if (tanks.length === 0) return null;

  return (
    <div className="rounded-lg border border-gray-700/50 bg-gray-900/40 p-4 space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-200">🪣 {tr('hopper.title')}</h3>
        <small className="text-xs text-gray-500">{tr('hopper.hint')}</small>
      </div>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="grid gap-2 sm:grid-cols-2">
        {tanks.map(t => {
          const est = estimates[t.tank_id];
          const remaining = est?.remaining_g_est;
          const unknown = !est || remaining == null;
          const low = !!est?.low && !unknown;
          return (
            <div key={t.tank_id}
              className={`flex items-center gap-2 rounded border px-3 py-2 text-sm ${
                low ? 'border-red-500/60 bg-red-500/10'
                    : 'border-gray-700/50 bg-gray-800/40'}`}>
              <span className="font-mono text-gray-300 w-20 shrink-0">{t.tank_id}</span>
              <span className={`flex-1 ${low ? 'text-red-300' : 'text-gray-300'}`}>
                {unknown
                  ? tr('hopper.noRecord')
                  : tr('hopper.remaining', { kg: (remaining! / 1000).toFixed(2) })}
                {low && <strong className="ml-1">{tr('hopper.lowBadge')}</strong>}
              </span>
              <input
                type="number" min="0.1" max="50" step="0.5"
                placeholder={tr('hopper.kgPlaceholder')}
                value={refillKg[t.tank_id] ?? ''}
                onChange={e => setRefillKg(prev => ({ ...prev, [t.tank_id]: e.target.value }))}
                className="w-20 rounded border border-gray-600 bg-gray-800 px-2 py-1 text-xs text-gray-200"
              />
              <button
                onClick={() => handleRefill(t.tank_id)}
                disabled={busy === t.tank_id}
                className="rounded bg-emerald-600 px-2 py-1 text-xs text-white hover:bg-emerald-500 disabled:opacity-50">
                {tr('hopper.refillBtn')}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
