import { useCallback, useEffect, useRef, useState } from 'react';
import { FeedCycles, Vision } from '../../lib/api';
import type { FeedCycle, VisionObservation } from '../../lib/types';

// G-4 전역 hook: App.tsx 최상위에서 사용. 어느 탭에 있든 cycle 종료를 감지해
// modal 을 띄운다. 10초 polling 으로 active cycle set 을 추적하고, 이전 polling 에
// active 였던 cycle 이 사라지면 completed 로 간주.
//
// race 조건: cycle 이 polling 사이 (10s) 안에 시작+종료되면 못 잡음. 실 cycle 은
// 보통 5s+ 라 안전. 강릉 D-18 환경에서도 4탱크 동시 운영 + 10s 폴링 = OK.
//
// 별도 파일로 분리한 이유: vite React Fast Refresh 가 컴포넌트 + 비컴포넌트 export
// 를 같은 파일에서 함께 export 하면 invalidate 되어 hot reload 시 hook polling 이
// 죽음. 분리하면 정상 Fast Refresh.

const CYCLE_POLLING_INTERVAL_MS = 10_000;

export function useCycleCompletionDispute() {
  const seenRef = useRef<Set<string>>(new Set());
  const lastActiveRef = useRef<Map<string, FeedCycle>>(new Map());
  const initialSeededRef = useRef(false);
  const [queue, setQueue] = useState<FeedCycle[]>([]);
  const [current, setCurrent] = useState<{ cycle: FeedCycle; observation: VisionObservation } | null>(null);

  // 10s polling — active cycle set 추적 + 사라진 cycle 감지
  useEffect(() => {
    let cancelled = false;

    async function tick() {
      try {
        const res = await FeedCycles.listActive();
        if (cancelled) return;
        const newMap = new Map(res.items.map(c => [c.cycle_id, c]));
        if (!initialSeededRef.current) {
          // 첫 polling — active 목록만 저장, modal trigger 안 함 (페이지 로드 시 폭주 방지)
          lastActiveRef.current = newMap;
          initialSeededRef.current = true;
          return;
        }
        const newlyCompleted: FeedCycle[] = [];
        for (const [id, cycle] of lastActiveRef.current) {
          if (!newMap.has(id) && !seenRef.current.has(id)) {
            seenRef.current.add(id);
            newlyCompleted.push(cycle);
          }
        }
        lastActiveRef.current = newMap;
        if (newlyCompleted.length > 0) {
          setQueue(prev => [...prev, ...newlyCompleted]);
        }
      } catch {
        // 네트워크 일시 실패 — silent skip
      }
    }

    void tick();
    const interval = setInterval(() => void tick(), CYCLE_POLLING_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  // 큐 처리 — current 없으면 다음 cycle 의 observation lookup
  // race fix: setQueue 를 fetch 시작 전에 호출하면 useEffect dependency 변경 →
  // cleanup 의 cancelled=true 가 fetch 응답을 막아 modal 안 뜸. setQueue 를 응답
  // 처리 후로 이동.
  useEffect(() => {
    if (current !== null) return;
    if (queue.length === 0) return;
    const next = queue[0];
    let cancelled = false;
    type WrappedItem = { payload?: Record<string, unknown> };
    Vision.observations({ tankId: next.tank_id, limit: 30 })
      .then(res => {
        if (cancelled) return;
        const items = res.items as unknown as WrappedItem[];
        const match = items.find(item => {
          const evidence = item.payload?.['evidence'] as Record<string, unknown> | undefined;
          return evidence !== undefined && evidence['cycle_id'] === next.cycle_id;
        });
        setQueue(prev => prev.slice(1)); // 응답 처리 후 큐에서 제거 (race fix: 시작 전에 호출하면 cleanup 으로 fetch 막힘)
        if (match?.payload) {
          setCurrent({ cycle: next, observation: match.payload as VisionObservation });
        }
        // observation 미발견은 silent skip — LRCN 미가동 / G-3 disabled 환경
      })
      .catch(() => {
        setQueue(prev => prev.slice(1)); // 실패 시도 큐에서 제거 (무한 retry 방지)
      });
    return () => { cancelled = true; };
  }, [queue, current]);

  const dismiss = useCallback(() => setCurrent(null), []);
  return { current, dismiss };
}
