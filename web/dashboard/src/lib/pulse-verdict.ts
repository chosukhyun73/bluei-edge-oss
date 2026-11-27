import type { FeedCycleReport } from './types';

export type PulseVerdict = 'yes' | 'unclear' | 'no' | 'unknown';

// 펄스별 배출 판정의 화면 표시값.
//
// 두 채널을 병합한다: 라이브(gap끝, compensation) verdict 우선, 없으면 오프라인
// (discharge_check). 보고 채널은 보상 피크가 σ 를 오염시켜 과비관이다
// (2026-07-09 육안 대조 실측).
//
// ⚠ 게이트 보정 (2026-07-30): 보상 정밀 게이트가 보류한 펄스(gate_pass=false)는
// "요동 상승폭은 안 나왔지만 사이클 전체 맥락상 배출된 것으로 보인다"는 판단이다.
// 그런 펄스를 화면에 '미급이'로 띄우면 두 가지 문제가 생긴다:
//   ① 게이트는 배출로 보고 보상을 막았는데 화면은 반대로 말한다 (운영자 혼란 —
//      2026-07-30 실제 지적)
//   ② 모달 ✓/✗ 프리필이 ✗ 로 시작해, 확인 없이 제출하면 시스템 판정이 그대로
//      '정답 라벨'로 되돌아 기록된다 (순환논리 오염)
// 그래서 표시상으로만 'unclear'(판정 보류)로 낮춘다. verdict 원본은 이벤트에
// 그대로 남으므로 기록·분석은 영향을 받지 않는다.
export function effectivePulseVerdicts(rep: FeedCycleReport): PulseVerdict[] {
  const n = rep.pulses_executed ?? 0;
  const offline = rep.discharge_check?.per_pulse ?? [];
  const live = new Map<number, { verdict: PulseVerdict; gatePass?: boolean }>();
  for (const p of rep.compensation?.per_pulse ?? []) {
    live.set(p.pulse_no, { verdict: p.verdict, gatePass: p.gate_pass });
  }
  return Array.from({ length: n }, (_, i) => {
    const l = live.get(i + 1);
    const v: PulseVerdict = l?.verdict ?? offline[i] ?? 'unknown';
    if (v === 'no' && l?.gatePass === false) return 'unclear';
    return v;
  });
}
