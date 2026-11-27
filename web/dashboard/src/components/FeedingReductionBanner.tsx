import { useEffect, useState } from 'react';
import { Schedules } from '../lib/api';
import type { FeedingSchedule } from '../lib/types';
import { useLanguage } from '../lib/language-core';

// 급이량 감산 배너 (2026-10-03, 운영자 요청).
//
// 왜 필요한가
//   화면의 급이량은 프론트가 computeFeedingPolicy 로 자체 계산한 값이라, backend
//   AIScheduler 가 적용한 수온 급변 감산을 모른다. 2026-10-03 에 실제로는 회당 98g 이
//   나가는데 화면은 163g 을 보여줬고, 운영자는 로그를 봐야만 반감 사실을 알 수 있었다.
//
// 설계 원칙 (FeedingGuidanceBanner 와 동일)
//   - 감산이 없으면 아무것도 안 보인다. 늘 떠 있는 배너는 배경이 된다.
//   - 계수를 프론트에서 계산하지 않는다. backend 가 실어 보낸 값만 표시한다 —
//     같은 판정을 두 군데서 하면 반드시 어긋난다.
//   - 결론(×0.5)만이 아니라 근거(24시간 수온 경로, 감산 전 목표)를 같이 보여준다.
//   - 수온 판단 배너와 역할이 다르다. 저쪽은 30분 창 조기 경보, 이쪽은 "지금 급이가
//     이만큼 줄어 있다"는 현재 상태다. 창이 달라 서로 조용할 수 있다.
type Shown = {
  factor: number;
  reason?: string;
  tempNow?: number;
  temp24h?: number;
  beforeG?: number;
  afterG?: number;
  tanks: string[];
};

function pick(items: FeedingSchedule[]): Shown | null {
  let out: Shown | null = null;
  for (const s of items) {
    const p = s.pattern;
    const f = p?.reduction_factor;
    if (!f || f >= 1) continue;
    if (!out) {
      out = {
        factor: f,
        reason: p.reduction_reason,
        tempNow: p.temp_now_c,
        temp24h: p.temp_24h_ago_c,
        beforeG: p.target_before_cut_g,
        afterG: p.target_amount_g ?? undefined,
        tanks: [],
      };
    }
    // 같은 순환수라 보통 수조별 계수가 같다. 다르면 더 센 감산을 대표로 보여준다.
    if (f < out.factor) {
      out.factor = f;
      out.reason = p.reduction_reason;
      out.beforeG = p.target_before_cut_g;
      out.afterG = p.target_amount_g ?? undefined;
    }
    out.tanks.push(...(s.tank_ids ?? []));
  }
  return out;
}

export function FeedingReductionBanner() {
  const { tr } = useLanguage();
  const [shown, setShown] = useState<Shown | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () => {
      Schedules.list()
        .then(r => { if (!cancelled) setShown(pick(r.items ?? [])); })
        .catch(() => { /* 조회 실패 시 직전 표시 유지 — 깜빡임 방지 */ });
    };
    load();
    // 스케줄러가 5분 주기로 재계산하므로 그보다 자주 볼 이유가 없다.
    const t = window.setInterval(load, 5 * 60_000);
    const onVisible = () => { if (!document.hidden) load(); };
    document.addEventListener('visibilitychange', onVisible);
    return () => { cancelled = true; window.clearInterval(t); document.removeEventListener('visibilitychange', onVisible); };
  }, []);

  if (!shown) return null;

  const pct = Math.round(shown.factor * 100);
  const delta = shown.tempNow != null && shown.temp24h != null
    ? shown.tempNow - shown.temp24h
    : null;
  const reasonLabel = shown.reason === 'adaptation_48h'
    ? tr('feedingReduction.reasonAdaptation')
    : tr('feedingReduction.reasonRapidChange');

  return (
    <div
      className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3"
      role="status"
      aria-label={tr('feedingReduction.ariaLabel')}
    >
      <div className="flex items-start gap-3">
        <span className="mt-1 h-2 w-2 shrink-0 rounded-full bg-amber-400" aria-hidden="true" />
        <div className="min-w-0 space-y-1">
          <p className="text-sm font-semibold text-amber-200">
            {tr('feedingReduction.headline', { pct: String(pct) })}
            <span className="ml-2 rounded bg-amber-500/20 px-1.5 py-0.5 font-mono text-xs text-amber-100">
              ×{shown.factor.toFixed(2)}
            </span>
          </p>
          <p className="text-xs text-amber-100/80">
            {reasonLabel}
            {delta != null && shown.tempNow != null && shown.temp24h != null && (
              <span className="ml-1 font-mono">
                24h {delta >= 0 ? '+' : ''}{delta.toFixed(2)}℃
                {' '}({shown.temp24h.toFixed(2)} → {shown.tempNow.toFixed(2)}℃)
              </span>
            )}
          </p>
          {shown.beforeG != null && shown.afterG != null && (
            <p className="text-xs text-amber-100/80">
              {tr('feedingReduction.amounts')}{' '}
              <span className="font-mono line-through opacity-60">{Math.round(shown.beforeG)}g</span>
              {' → '}
              <span className="font-mono font-semibold text-amber-100">{Math.round(shown.afterG)}g</span>
              <span className="ml-1 opacity-70">{tr('feedingReduction.perCycle')}</span>
            </p>
          )}
          <p className="text-xs text-amber-100/60">{tr('feedingReduction.note')}</p>
        </div>
      </div>
    </div>
  );
}
