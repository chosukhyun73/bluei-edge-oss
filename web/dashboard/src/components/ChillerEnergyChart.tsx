import { memo, useEffect, useState, useCallback } from 'react';
import { dateLocale } from '../lib/format';
import { Chiller, type ChillerEnergyPoint } from '../lib/api';
import { CanvasTimeChart } from './CanvasTimeChart';
import { useLanguage } from '../lib/language-core';

// 냉각기 24시간 전력 평가 — kW 추이 + 누적 kWh(전력사용량, 경진대회 점수지표).
// 전류(실측) → kW(PF 0.85 가정 계산) → kWh(적산).
const POLL_MS = 60_000;

// memo — 부모(GroupOverview) 5s state-vector 폴링 재렌더에서 격리. props 없음 → 자체
// 60s 데이터 폴링 때만 재렌더. WebKitGTK 에서 recharts 재렌더마다 메모리가 잔류해
// (전력차트 단독 +33MB/분 실측, 2026-07-06 bisect) 렌더러가 수 시간 내 사망하던 원인.
export const ChillerEnergyChart = memo(function ChillerEnergyChart() {
  const { tr } = useLanguage();
  const [data, setData] = useState<ChillerEnergyPoint[]>([]);
  const [kwh, setKwh] = useState({ total: 0, month: 0, day: 0 });
  const [latestKw, setLatestKw] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [nowMs, setNowMs] = useState(() => Date.now());

  const load = useCallback(async () => {
    const now = Date.now();
    const since = new Date(now - 24 * 3600 * 1000).toISOString();
    try {
      const res = await Chiller.energy(since);
      setData(res.items ?? []);
      setKwh({ total: res.total_kwh ?? 0, month: res.month_kwh ?? 0, day: res.day_kwh ?? 0 });
      setLatestKw(res.latest_kw ?? null);
    } catch {
      /* keep last */
    }
    setNowMs(now);
    setLoading(false);
  }, []);

  useEffect(() => {
    void load();
    const id = window.setInterval(() => void load(), POLL_MS);
    return () => window.clearInterval(id);
  }, [load]);

  const chartData = data
    .filter(p => typeof p.power_kw === 'number')
    .map(p => ({ ts: new Date(p.ts).getTime(), kw: p.power_kw as number }));
  const fmt = (ts: number) => new Date(ts).toLocaleTimeString(dateLocale(), { hour: '2-digit', minute: '2-digit' });
  const sinceMs = nowMs - 24 * 3600 * 1000;

  return (
    <div className="bg-gray-900/40 border border-gray-700/30 rounded-lg p-3">
      <div className="flex items-center justify-between mb-2">
        <div className="text-sm text-gray-300 font-medium">{tr('chillerEnergy.title')}</div>
        <div className="flex items-center gap-3 text-xs font-mono">
          <span className="text-gray-400">{tr('chillerEnergy.total')} <span className="text-cyan-300">{kwh.total.toFixed(1)}</span></span>
          <span className="text-gray-400">{tr('chillerEnergy.month')} <span className="text-cyan-300">{kwh.month.toFixed(1)}</span></span>
          <span className="text-gray-400">{tr('chillerEnergy.day')} <span className="text-cyan-300">{kwh.day.toFixed(2)}</span></span>
          <span className="text-gray-500">kWh</span>
          {latestKw != null && <span className="text-gray-400 border-l border-gray-700 pl-3">{tr('chillerEnergy.now')} <span className="text-cyan-200">{latestKw.toFixed(1)}</span> kW</span>}
        </div>
      </div>
      {loading ? (
        <div className="py-10 text-center text-xs text-gray-500 font-mono">{tr('chillerEnergy.loading')}</div>
      ) : chartData.length === 0 ? (
        <div className="py-10 text-center text-xs text-gray-500 font-mono">{tr('chillerEnergy.noData')}</div>
      ) : (
        /* SVG(recharts) 금지 — WebKitGTK repaint 누수 (CanvasTimeChart 헤더 참고) */
        <CanvasTimeChart
          data={chartData}
          series={[{ key: 'kw', name: 'kW', color: '#06b6d4', area: true }]}
          xDomain={[sinceMs, nowMs]}
          yMin={0}
          unit="kW"
          height={190}
          fmtTime={fmt}
        />
      )}
      <div className="text-[10px] text-gray-600 mt-1">
        {tr('chillerEnergy.footnote')}
      </div>
    </div>
  );
});
