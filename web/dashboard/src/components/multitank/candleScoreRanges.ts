// CandleScoreChart 의 시간범위 타입/라벨 — 컴포넌트 파일에서 분리 (Fast Refresh 경계).
// 컴포넌트+상수 혼합 export 는 HMR invalidate 를 임포터로 전파시킨다 (language-core.ts 참조).
export type TimeRange = '1h' | '6h' | '12h' | '1d' | '7d' | '30d' | 'since-stocking';

// 시간 범위 → span/윈도우(캔들 1개) 크기. windowMs=null 이면 raw 선형(집계 없음).
// CandleScoreChart 와 LineScoreChart(평소 활동성, spanMs 만 사용)가 공유한다.
// 2026-07-24: 6·12시간을 raw 선형으로 전환 — 급이는 하루 8번 띄엄띄엄한 이벤트라
// 60/30분 OHLC 캔들이 5분짜리 급이 스파이크를 뭉갠다(Open/Low/Close 가 평소값으로
// 붕괴, High 만 피크 포착 → 몸통이 무반응처럼 보임). 선형은 급이 진폭·모양을 그대로
// 보이고 급이선(:00/:30)과 정렬되며 r3 오버레이도 표시된다. 1일+는 점 과다로 캔들 유지.
export const RANGE_CONFIG: Record<TimeRange, { spanMs: number | null; windowMs: number | null }> = {
  '1h':             { spanMs: 1 * 3600_000,        windowMs: null },       // raw 7초
  '6h':             { spanMs: 6 * 3600_000,        windowMs: null },       // raw 선형 (급이 스파이크 보존)
  '12h':            { spanMs: 12 * 3600_000,       windowMs: null },       // raw 선형 (급이 스파이크 보존)
  '1d':             { spanMs: 24 * 3600_000,       windowMs: 2 * 3600_000 },
  '7d':             { spanMs: 7 * 24 * 3600_000,   windowMs: 12 * 3600_000 },
  '30d':            { spanMs: 30 * 24 * 3600_000,  windowMs: 24 * 3600_000 },
  'since-stocking': { spanMs: null,                windowMs: 24 * 3600_000 },
};

// 값은 i18n 키 — 소비처(InferenceMonitor)에서 tr 로 해석해 표시한다.
export const RANGE_LABELS: Record<TimeRange, string> = {
  '1h': 'candleScoreChart.range1h',
  '6h': 'candleScoreChart.range6h',
  '12h': 'candleScoreChart.range12h',
  '1d': 'candleScoreChart.range1d',
  '7d': 'candleScoreChart.range7d',
  '30d': 'candleScoreChart.range30d',
  'since-stocking': 'candleScoreChart.rangeSinceStocking',
};
