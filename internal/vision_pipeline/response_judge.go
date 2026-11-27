package vision_pipeline

// 멈춤 판정 룰 v0 (축1 1b — docs/wip/autonomous-feeding-roadmap-2026-07-06.md).
//
// 펄스별 요동 prior(SegmentPrior)에 "무반응 연속" 룰을 적용해 남은 펄스 중단
// 시점을 제안한다. v0 는 섀도 전용 — 실제 중단은 운영자 승인(L2) 후.
//
// ⚠ 오프라인 재생 실측(2026-07-06, scripts/replay_response_judge.py):
// 요동 prior 는 뚜렷한 반응 소멸(연속 ~0.0)은 잡지만, 07-06 tank_01 기준 사례
// (운영자 관찰 p5·p6 무반응)는 못 잡는다 — 요동은 수영 활동을 재고 섭이 중단을
// 못 잰다. 90% 일치(L2 승격)는 LRCN v1(사람 라벨 학습) 승격이 전제.
// 파라미터 변경 시 replay 스크립트의 TRACK_PARAMS 와 동기화할 것.

// JudgeParams — 트랙별 판정 파라미터.
type JudgeParams struct {
	NoResponseMax float64 // prior < 이 값 → 무반응 (가중 1.0)
	WeakMax       float64 // prior < 이 값 → 약반응 (가중 WeakWeight)
	WeakWeight    float64 // 약반응 누적 가중
	StopScore     float64 // 연속 누적 ≥ 이 값 → 중단 판정
}

// TrackUParams — 수중캠 보유 수조(트랙 U): 무반응 2연속. 약반응 카운트 안 함.
func TrackUParams() JudgeParams {
	return JudgeParams{NoResponseMax: 0.15, WeakMax: 0.35, WeakWeight: 0, StopScore: 2.0}
}

// TrackTParams — 탑뷰 전용 수조(트랙 T): 더 민감 — 무반응 1 + 약반응 0.5 로 1.5.
func TrackTParams() JudgeParams {
	return JudgeParams{NoResponseMax: 0.15, WeakMax: 0.35, WeakWeight: 0.5, StopScore: 1.5}
}

// JudgeStopPulse — 펄스별 prior 열에 판정 룰 적용. 반환 = 중단 판정이 확정되는
// 펄스 번호(1-based; 그 펄스까지 관찰 후 남은 펄스 중단 제안). 0 = 중단 없음.
// 강반응(≥WeakMax) 이 나오면 누적을 리셋한다.
func JudgeStopPulse(priors []float64, p JudgeParams) int {
	score := 0.0
	for i, pr := range priors {
		switch {
		case pr < p.NoResponseMax:
			score += 1.0
		case pr < p.WeakMax:
			score += p.WeakWeight
		default:
			score = 0
		}
		if score >= p.StopScore {
			return i + 1
		}
	}
	return 0
}
