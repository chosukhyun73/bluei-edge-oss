import { useCallback, useEffect, useState } from 'react';
import { api } from '../lib/api';
import { useLanguage } from '../lib/language-core';

// 급이 판단 가이드 배너 (2026-10-01, 운영자 설계).
//
// 설계 원칙
//   - 평상시에는 아무것도 안 보인다 (level=ok 면 렌더 자체를 안 한다). 늘 떠 있는
//     배너는 배경이 되어 정작 중요할 때 눈에 안 들어온다.
//   - 결론만이 아니라 "언제 얼마였는지" 경로를 보여준다.
//   - 확인을 누르면 사라지고 그 시점이 새 기준이 된다. 이미 인지한 변화를 반복해서
//     띄우는 것은 소음이다 — 중요한 건 "내가 본 뒤로 또 뭐가 달라졌나".
//   - 30분 주기 갱신. 판단 자체가 30분 창을 보므로 더 자주 볼 이유가 없다.
//   - 수조가 아니라 그룹 단위다 (운영자 지적): RAS 단일 순환수라 그룹 안 수조가 모두
//     같은 물을 쓴다. 같은 경고를 수조 수만큼 띄우는 것은 소음이다.
type Point = { at: string; temp_c: number; note?: string };
type Guidance = {
  level: 'ok' | 'caution' | 'hold';
  headline: string;
  path: Point[];
  reasons: string[];
  temp_now_c: number | null;
  baseline_by: 'ack' | '24h';
  elapsed_hours: number;
  rate_30m_c: number | null;
  factor_pct: number;
  acked: boolean;
  sticky: boolean;
};

const STYLE = {
  caution: { box: 'bg-amber-500/10 border-amber-500/40', dot: 'bg-amber-400', label: '감량 권고' },
  hold:    { box: 'bg-red-500/10 border-red-500/40',     dot: 'bg-red-400',   label: '급이 보류' },
} as const;

const hhmm = (iso: string) => {
  const d = new Date(iso);
  return `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ` +
         `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
};

export function FeedingGuidanceBanner({ groupId, groupName }: { groupId: string; groupName?: string }) {
  const { tr } = useLanguage();
  const [g, setG] = useState<Guidance | null>(null);

  const load = useCallback(async () => {
    if (!groupId) { setG(null); return; }
    try {
      const res = await api<Guidance>(`/v1/groups/${encodeURIComponent(groupId)}/feeding-guidance`);
      setG(res.level === 'ok' ? null : res);
    } catch { setG(null); }
  }, [groupId]);

  useEffect(() => {
    void load();
    const id = window.setInterval(() => void load(), 30 * 60_000); // 30분
    return () => window.clearInterval(id);
  }, [load]);

  const ack = async () => {
    try {
      await api(`/v1/groups/${encodeURIComponent(groupId)}/feeding-guidance`, { method: 'POST' });
    } catch { /* 실패해도 아래 재조회가 현재 상태를 다시 그린다 */ }
    void load();
  };

  if (!g) return null; // 평상시 = 아무것도 표시하지 않음
  const st = STYLE[g.level as 'caution' | 'hold'] ?? STYLE.caution;

  return (
    <div className="space-y-2">
      <div className={`px-3 py-2 rounded-lg border ${st.box}`}>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className={`w-2 h-2 rounded-full ${st.dot}`} />
              {groupName && <span className="text-xs font-mono text-gray-400">{groupName}</span>}
              <span className="text-sm font-semibold text-white">{st.label}</span>
              <span className="text-sm text-gray-200">— {g.headline}</span>
              <span className="text-xs font-mono text-gray-400">· 권고량 {g.factor_pct}%</span>
              {g.acked && <span className="text-[10px] text-gray-500">(확인 기준)</span>}
            </div>

            {/* 변화 경로 — 언제 얼마였는지 */}
            {g.path.length > 0 && (
              <div className="mt-1.5 ml-4 font-mono text-[11px] text-gray-300 space-y-0.5">
                {g.path.map((p, i) => (
                  <div key={i}>
                    <span className="text-gray-500">{hhmm(p.at)}</span>
                    <span className="ml-2">{p.temp_c.toFixed(2)}℃</span>
                    {p.note && <span className="ml-2 text-gray-500">← {p.note}</span>}
                  </div>
                ))}
              </div>
            )}

            {g.reasons.length > 0 && (
              <ul className="mt-1 ml-4 text-[11px] text-gray-400 list-disc space-y-0.5">
                {g.reasons.map((r, i) => <li key={i}>{r}</li>)}
              </ul>
            )}
          </div>

          {/* 절대 한계(6/18℃ 밖)는 확인으로 숨길 수 없다 — 변화가 아니라 상태다 */}
          {!g.sticky && (
            <button
              type="button"
              onClick={() => void ack()}
              className="shrink-0 px-2.5 py-1 text-xs rounded border border-gray-500/50
                         text-gray-200 hover:bg-white/10 transition-colors"
            >
              확인
            </button>
          )}
        </div>
      </div>
      <p className="text-[10px] text-gray-500 pl-1">{tr('feedingGuidance.disclaimer')}</p>
    </div>
  );
}
