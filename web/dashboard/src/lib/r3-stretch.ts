// r3 표시 스트레치 — r3(feeding_score_pulse) 점수가 0.33~0.80 좁은 대역에 압축
// (MSE 회귀 + 적은 학습데이터의 평균회귀)돼 사이클 간 갭이 그래프에서 안 보이는 문제
// (2026-07-23 운영자: "값이 0.4~0.6에 갇혀 갭 인식이 안 된다")를 표시 계층에서만 해소한다.
//
// 방식: r3 분위수 → 운영자 분위수로 3점 piecewise 선형 매핑. 중앙(P50)을 운영자 P50(0.5)에
// 정합시켜 왜곡 없이 꼬리만 편다("AI 점수가 운영자 점수와 같은 의미가 됨").
//
// ⚠ 전역 단일 스케일 — 수조별 정규화 금지. tank_02 저값은 실제 저반응일 수 있어, 수조별로
//    펴면 실제 차이가 지워진다(요동 P90 자기정규화가 만든 "수조간 편향" 혼란 재현).
// ⚠ 표시 전용 — 저장(vision.segment.model_scores.recorded)·dispute ai_score·모달 프리필은
//    원값을 유지한다(재보정 가능·수렴지표 무오염).
//
// 앵커 실측 2026-07-28 (r3 best-r3-20260718, n=743 / 운영자 n=631):
//   r3      P05=0.329 P50=0.627 P95=0.804
//   운영자  P05=0.30  P50=0.50  P95=1.00
const ANCHORS = [
  { raw: 0.0, mapped: 0.0 },
  { raw: 0.329, mapped: 0.3 },
  { raw: 0.627, mapped: 0.5 },
  { raw: 0.804, mapped: 1.0 },
] as const;

/** r3 원값(0~1)을 운영자 스케일로 매핑한 표시값(0~1)으로 변환한다. 표시 전용. */
export function stretchR3(v: number): number {
  if (!Number.isFinite(v)) return v;
  if (v <= ANCHORS[0].raw) return ANCHORS[0].mapped;
  for (let i = 1; i < ANCHORS.length; i++) {
    const lo = ANCHORS[i - 1];
    const hi = ANCHORS[i];
    if (v <= hi.raw) {
      return lo.mapped + ((v - lo.raw) / (hi.raw - lo.raw)) * (hi.mapped - lo.mapped);
    }
  }
  return ANCHORS[ANCHORS.length - 1].mapped; // 상단 clamp
}
