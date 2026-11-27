import { useEffect, useState } from 'react';
import { dateLocale, tempRationale } from '../lib/format';
import { Snowflake } from 'lucide-react';
import { Optimize, type TempControlDecision } from '../lib/api';
import { useLanguage } from '../lib/language-core';

// 자동 수온제어 워커의 실제 마지막 판단을 칠러 카드 옆에 표시.
// "지금 왜 켜졌나/꺼졌나"(코스팅 예측 정지 포함)를 로그 없이 대시보드에서 확인.
const POLL_MS = 15_000;

function hhmm(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString(dateLocale(), { hour: '2-digit', minute: '2-digit' });
  } catch {
    return '—';
  }
}

export function TempControlStatusPanel() {
  const { tr } = useLanguage();
  const [d, setD] = useState<TempControlDecision | null>(null);

  useEffect(() => {
    const load = () =>
      Optimize.temp()
        .then(r => setD(r.control ?? null))
        .catch(() => undefined);
    void load();
    const id = window.setInterval(load, POLL_MS);
    return () => window.clearInterval(id);
  }, []);

  const rateStr = d && d.have_rate ? `${d.rate_c_per_h >= 0 ? '+' : ''}${d.rate_c_per_h.toFixed(1)}/h` : '';

  // 판단 색상: 코스팅 정지=강조(호박), 냉각 중=파랑, OFF/자연상승=청록, 유지=회색.
  const judgeColor = !d
    ? 'text-gray-500'
    : d.coasting
      ? 'text-amber-300'
      : d.action === 'cool'
        ? 'text-blue-300'
        : d.action === 'allow_rise'
          ? 'text-cyan-300'
          : 'text-gray-300';

  return (
    <div className="h-full bg-gray-900/40 border border-gray-700/30 rounded-lg p-3 flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5 text-xs font-medium text-gray-200">
        <Snowflake className="w-3.5 h-3.5 text-cyan-300" />
        <span>{tr('tempControlStatus.title')}</span>
        {d && (
          <span
            className={`ml-auto text-[9px] px-1.5 py-0.5 rounded font-mono border ${d.active ? 'bg-green-500/20 text-green-300 border-green-500/40' : 'bg-gray-600/30 text-gray-400 border-gray-500/40'}`}>
            {d.active ? tr('tempControlStatus.badgeActive') : tr('tempControlStatus.badgeObserve')}
          </span>
        )}
      </div>

      {!d ? (
        <div className="text-[11px] text-gray-500 font-mono py-2">{tr('tempControlStatus.awaiting')}</div>
      ) : (
        <>
          <div className="text-xs font-mono text-gray-400">
            {tr('tempControlStatus.target')} <span className="text-green-400">{d.target_c.toFixed(1)}℃</span> · {tr('tempControlStatus.current')}{' '}
            <span className="text-gray-200">{d.temp_c.toFixed(2)}℃</span>
            {rateStr && <span className="text-gray-500"> ({rateStr})</span>}
          </div>

          <div className={`text-xs font-medium ${judgeColor}`}>
            {d.coasting && '★ '}
            {tr('tempControlStatus.decisionLabel')}{' '}
            {d.coasting
              ? tr('format.tempRatCoasting')
              : tempRationale(d.action, d.temp_c, d.target_c, d.reason)}
          </div>

          <div className="text-[11px] font-mono text-gray-500 mt-auto">
            {tr('tempControlStatus.chiller')}{' '}
            <span className={d.actual_on ? 'text-cyan-300' : 'text-gray-400'}>
              {d.actual_on ? 'ON' : 'OFF'}
            </span>
            {d.desired_on !== d.actual_on && (
              <span className="text-amber-400"> {tr('tempControlStatus.desiredTarget', { state: d.desired_on ? 'ON' : 'OFF' })}</span>
            )}
            {' · '}
            {hhmm(d.at)}
            {!d.active && <span className="text-gray-600"> · {tr('tempControlStatus.observeInactive')}</span>}
          </div>
        </>
      )}
    </div>
  );
}
