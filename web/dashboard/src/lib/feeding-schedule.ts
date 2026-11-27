// 급이 스케줄 자동생성 — 1일 급이량을 N회(수처리 부하 상한 기반)로 분할 + 수조간 교차(non-overlapping).
// 원칙: 공유 수처리에 물린 수조들은 동시 급이 금지(pH/DO 순간부하). 활동시간대에 전 수조×전 급이를
//       시간축에서 겹치지 않게 배치한다. GET80(소화간격)은 참고·경고용(하드 제약 아님).
// 생성 결과는 표시/검토용. 실제 자동 급이는 feeding_schedules enabled 로 별도 게이트.

import type { FeedingPolicyComputation } from './species-policy';

export type TankFeedingSchedule = {
  tank_id: string;
  times: string[]; // "HH:MM" 급이 시각들
  daily_cycles: number;
  get80_h: number | null;             // 소화간격(참고)
  scheduled_interval_h: number | null; // 실제 배치된 수조내 급이 간격(h)
  refeed_below_get80: boolean;         // 실제 간격 < GET80 (소화 전 재급이) 경고
  cycle_target_amount_g: number | null;
};

// 급이 활동시간대 = 경연장 점등 시간. 소등되면 카메라 영상이 안 들어와 AI 멈춤판단·관찰 불가 →
// 급이는 반드시 점등시간 안에서만 진행한다.
//  · 오후 소등 ~17:30(관측) → 마지막 급이가 소등과 겹치지 않도록 종료 17:00(버퍼 30분).
//  · 아침 점등 ~09:00 은 추정값(밤사이 캡처 없어 미측정) — 실측 후 보정.
export const FEED_WINDOW_START_HOUR = 9;
export const FEED_WINDOW_END_HOUR = 17;

export type ScheduleGenOptions = {
  startHour?: number; // 활동시간대 시작(시). 기본 FEED_WINDOW_START_HOUR
  endHour?: number;   // 활동시간대 종료(시). 기본 FEED_WINDOW_END_HOUR
};

function fmtHHMM(totalMin: number): string {
  const m = ((Math.round(totalMin) % 1440) + 1440) % 1440;
  const h = Math.floor(m / 60);
  const mm = m % 60;
  return `${String(h).padStart(2, '0')}:${String(mm).padStart(2, '0')}`;
}

// generateCrossTankSchedule — 전 수조×전 급이를 활동시간대에 round-robin 으로 교차 배치.
// 각 급이 이벤트가 고유 슬롯을 가지므로 동시 급이(overlap)가 발생하지 않는다.
export function generateCrossTankSchedule(
  tanks: { tank_id: string; computation: FeedingPolicyComputation }[],
  opts: ScheduleGenOptions = {},
): TankFeedingSchedule[] {
  const startMin = (opts.startHour ?? FEED_WINDOW_START_HOUR) * 60;
  const endMin = (opts.endHour ?? FEED_WINDOW_END_HOUR) * 60;
  const windowLen = Math.max(1, endMin - startMin);

  // 각 수조의 급이 횟수 N.
  const counts = tanks.map(t => Math.max(1, t.computation.daily_cycles ?? 1));

  // round-robin 이벤트 순서 생성: [tank0, tank1, ..., tank0, tank1, ...] — 수조를 번갈아 배치.
  const remaining = counts.slice();
  const order: number[] = []; // 각 이벤트의 수조 index
  let anyLeft = true;
  while (anyLeft) {
    anyLeft = false;
    for (let i = 0; i < tanks.length; i++) {
      if (remaining[i] > 0) {
        order.push(i);
        remaining[i]--;
        anyLeft = true;
      }
    }
  }

  const total = order.length;
  // 급이 시각을 점등·소등 "안쪽"으로만 배치(양 끝 여백 1슬롯) — 점등 직후/소등 직전(소등 시각) 급이 방지.
  // 소등엔 카메라 영상이 없어 급이·관찰 불가하므로 마지막 급이가 소등과 겹치면 안 된다.
  const slot = windowLen / (total + 1);
  const perTankMinutes: number[][] = tanks.map(() => []);
  order.forEach((tankIdx, rank) => {
    perTankMinutes[tankIdx].push(startMin + (rank + 1) * slot);
  });

  return tanks.map((t, i) => {
    const mins = perTankMinutes[i].sort((a, b) => a - b);
    const times = mins.map(fmtHHMM);
    // 실제 수조내 급이 간격 = 연속 이벤트 시간차(균등하므로 첫 간격 대표값). 1회면 null.
    const scheduled_interval_h =
      mins.length >= 2 ? (mins[1] - mins[0]) / 60 : null;
    const get80 = t.computation.get80_h;
    const refeed_below_get80 =
      scheduled_interval_h != null && get80 != null && scheduled_interval_h < get80;
    return {
      tank_id: t.tank_id,
      times,
      daily_cycles: counts[i],
      get80_h: get80,
      scheduled_interval_h,
      refeed_below_get80,
      cycle_target_amount_g: t.computation.cycle_target_amount_g,
    };
  });
}
