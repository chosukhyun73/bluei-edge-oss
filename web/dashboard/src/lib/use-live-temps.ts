import { useEffect, useState } from 'react';
import { Optimize } from './api';

// 수조별 실시간 수온 — 급이 판단(GET 온도보정)용. 60초 갱신, 미수신 수조는 키 없음
// (호출부가 `?? null` 로 fallback). backend AIScheduler 도 동일하게 실측을 사용 (2026-07-05).
export function useLiveTemps(): Record<string, number> {
  const [temps, setTemps] = useState<Record<string, number>>({});
  useEffect(() => {
    let alive = true;
    const load = () =>
      Optimize.temp()
        .then(res => {
          if (!alive) return;
          const map: Record<string, number> = {};
          for (const r of res.items) map[r.tank_id] = r.current_temp_c;
          setTemps(map);
        })
        .catch(() => { /* 실패 시 기존값 유지 */ });
    void load();
    const t = setInterval(load, 60_000);
    return () => { alive = false; clearInterval(t); };
  }, []);
  return temps;
}
