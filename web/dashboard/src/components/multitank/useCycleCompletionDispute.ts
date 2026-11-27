import { useCallback, useEffect, useRef, useState } from 'react';
import { FeedCycles, Vision } from '../../lib/api';
import type { FeedCycleReport, VisionObservation } from '../../lib/types';
import { effectivePulseVerdicts } from '../../lib/pulse-verdict';

// G-4 전역 hook: App.tsx 최상위에서 사용. 어느 탭에 있든 cycle 종료를 감지해
// 평가 modal 을 띄운다.
//
// v2 (2026-07-17): 트리거를 "active 목록 diff" → "feed.cycle.report 폴링"으로 재설계.
//   - 구 방식은 10s 폴링 사이 active 목록 요동(백엔드 재시작·일시 실패)으로 모달
//     횟수가 실제 사이클 수와 어긋날 수 있었다 (운영자 실측 보고).
//   - 보고는 사이클당 정확히 1건이므로 report_id 기준이면 횟수가 항상 일치한다.
//   - 평가 대상 관측은 탑뷰 카메라만 (운영자 확정: 수중캠은 수조 공통이 아니라
//     모달 평가에서 제외 — 수중 클립은 ② 탭 세그먼트 채점 라인에서 별도 학습).
//
// 별도 파일로 분리한 이유: vite React Fast Refresh 가 컴포넌트 + 비컴포넌트 export
// 를 같은 파일에서 함께 export 하면 invalidate 되어 hot reload 시 hook polling 이
// 죽음. 분리하면 정상 Fast Refresh.

const REPORT_POLLING_INTERVAL_MS = 10_000;
const REPORT_POLL_LIMIT = 10;

type CycleRef = Pick<FeedCycleReport, 'cycle_id' | 'tank_id' | 'started_at' | 'pulses_executed'> & {
  suspected_no_discharge: number | null; // 시스템 무배출 의심 발수 (판정 없으면 null)
  pulse_verdicts: string[]; // 펄스별 판정 (라이브 우선 병합) — 몇 번째가 의심인지 표기용
};

// 펄스별 판정 병합 + 게이트 보정 — lib/pulse-verdict.ts 참고 (카드와 동일 규칙).

function isUnderwater(cameraID: string): boolean {
  return cameraID.toLowerCase().includes('underwater');
}

export function useCycleCompletionDispute() {
  const seenRef = useRef<Set<string>>(new Set()); // cycle_id 기준 — 사이클당 모달 1회
  const initialSeededRef = useRef(false);
  const [queue, setQueue] = useState<FeedCycleReport[]>([]);
  const [current, setCurrent] = useState<{
    cycle: CycleRef;
    observation: VisionObservation;
    ordinal: number | null; // 오늘 몇 번째 급이 (조회 실패 시 null)
  } | null>(null);

  // 10s 폴링 — 신규 사이클 보고 감지 (report = 사이클당 정확히 1건)
  useEffect(() => {
    let cancelled = false;

    async function tick() {
      try {
        const res = await FeedCycles.reports(REPORT_POLL_LIMIT);
        if (cancelled) return;
        const items = res.items ?? [];
        if (!initialSeededRef.current) {
          // 첫 폴링 — 기존 보고는 전부 seen 처리 (페이지 로드 시 과거분 폭주 방지)
          for (const it of items) seenRef.current.add(it.cycle_id);
          initialSeededRef.current = true;
          return;
        }
        const fresh = items
          .filter(it => !seenRef.current.has(it.cycle_id))
          .sort((a, b) => a.completed_at.localeCompare(b.completed_at)); // 오래된 것부터
        if (fresh.length === 0) return;
        for (const it of fresh) seenRef.current.add(it.cycle_id);
        setQueue(prev => [...prev, ...fresh]);
      } catch {
        // 네트워크 일시 실패 — silent skip (보고는 다음 폴링에 그대로 남아있음)
      }
    }

    void tick();
    const interval = setInterval(() => void tick(), REPORT_POLLING_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  // 큐 처리 — current 없으면 다음 보고의 탑뷰 관측 lookup
  useEffect(() => {
    if (current !== null) return;
    if (queue.length === 0) return;
    const next = queue[0];
    let cancelled = false;
    type WrappedItem = { payload?: Record<string, unknown> };
    // 오늘 몇 번째 급이 — 같은 수조의 당일 보고 수로 계산 (2026-07-17 운영자:
    // cycle_id 만으로는 몇 번째였는지 알 수 없음). 조회 실패해도 modal 은 뜬다.
    const ordinalPromise: Promise<number | null> = FeedCycles.reports(30, next.tank_id)
      .then(r => {
        const dayKey = new Date(next.started_at).toDateString();
        const startMs = new Date(next.started_at).getTime();
        return (r.items ?? []).filter(c =>
          new Date(c.started_at).toDateString() === dayKey &&
          new Date(c.started_at).getTime() <= startMs,
        ).length;
      })
      .catch(() => null);
    Promise.all([
      Vision.observations({ tankId: next.tank_id, limit: 30 }),
      ordinalPromise,
    ])
      .then(([res, ordinal]) => {
        if (cancelled) return;
        const items = res.items as unknown as WrappedItem[];
        // 탑뷰 관측만 평가 대상 — 수중캠 관측은 제외 (별도 학습 라인)
        const match = items.find(item => {
          const evidence = item.payload?.['evidence'] as Record<string, unknown> | undefined;
          if (evidence === undefined || evidence['cycle_id'] !== next.cycle_id) return false;
          const cam = (item.payload?.['camera_id'] as string | undefined) ?? '';
          return !isUnderwater(cam);
        });
        setQueue(prev => prev.slice(1)); // 응답 처리 후 큐에서 제거 (race fix: 시작 전에 호출하면 cleanup 으로 fetch 막힘)
        if (match?.payload) {
          setCurrent({
            cycle: {
              cycle_id: next.cycle_id,
              tank_id: next.tank_id,
              started_at: next.started_at,
              pulses_executed: next.pulses_executed,
              // 게이트 보정 후 기준 — 보류된 펄스는 의심에서 제외 (lib/pulse-verdict.ts)
              suspected_no_discharge: next.discharge_check
                ? effectivePulseVerdicts(next).filter(v => v === 'no').length
                : null,
              pulse_verdicts: effectivePulseVerdicts(next),
            },
            observation: match.payload as VisionObservation,
            ordinal,
          });
        }
        // 탑뷰 관측 미발견은 silent skip — LRCN 미가동 / 수중캠 단독 수조
      })
      .catch(() => {
        setQueue(prev => prev.slice(1)); // 실패 시도 큐에서 제거 (무한 retry 방지)
      });
    return () => { cancelled = true; };
  }, [queue, current]);

  const dismiss = useCallback(() => setCurrent(null), []);
  return { current, dismiss };
}
