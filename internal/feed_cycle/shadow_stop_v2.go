package feed_cycle

// 섀도 중단 판정 v2 — r3(LRCN 탑뷰 feeding head) 사이클 내 감쇠 (2026-08-05).
//
// 왜 v1 과 별도인가. v1 은 요동(수면 상승폭)으로 중단을 재는데, 8-05 에 그 축의
// 단조성이 깨진 것이 확인됐다. 운영자 정답이 붙은 펄스를 요동 값으로 세우면
//
//	"안 줘도 됐던" 펄스  08-03 13:43 p6  0.59
//	"줘야 했던"  펄스   08-05 t01 p7    0.43   ← 더 낮은데 줘야 했다
//	                    08-05 t02 p6    0.28   ← 훨씬 낮은데 줘야 했다
//
// 로 뒤집힌다. 임계를 어디에 놓아도 이 셋을 못 가른다 — 정규화 방식의 문제가
// 아니라 요동에 그 정보가 없다. 같은 5건을 r3 로 세우면 벽에 막히지 않는다.
//
// ⚠ **절대 수준이 아니라 사이클 내 상대값이어야 한다.** 고정 앵커(절대 기준)로
// 재보면 8-03 14:50(r3 0.55~0.58, 정답 "다 줘야")이 8-05(0.56~0.73, 정답 "다 줘야")
// 보다 통째로 낮아 임계 하나로 못 가른다. 운영자의 "그만 줘도 된다"는 판단은
// 절대 식욕이 아니라 **그 사이클 안에서 얼마나 떨어졌는가**에 붙어 있다.
//
// ⚠ 그래서 v2 는 사이클 최고 r3 대비 비율을 쓴다. 이 자는 "급이 정도 표시"에는
// 못 쓴다(모든 사이클에 1.00 이 하나씩 생기므로 사이클 간 비교 불가). 표시·수조
// 비교·신규 현장 이식은 config RiseAnchorByCamera 쪽이 담당한다. 역할이 다르다.
//
// ⚠ **섀도 전용.** 급이 경로에 배선하지 않는다. 임계 0.90 은 (C) 정답 5건에
// 맞춘 값이라 과적합 위험이 크다 — 표본을 더 쌓아 검증한 뒤에 승격을 판단한다.

const (
	// shadowV2LowRatio — 사이클 최고 r3 대비 이 비율 미만이면 저반응으로 센다.
	// 2026-08-05 (C) 5건에서 0.90·연속 2발이 5/5 적중. ⚠ n=5 과적합 주의.
	shadowV2LowRatio = 0.90
	// shadowV2LowStreak — 저반응이 몇 발 연속이면 중단으로 볼지. v1 과 같은 2발.
	shadowV2LowStreak = 2
	// shadowV2MinPulses — 이 발수 미만이면 감쇠 형태를 볼 수 없어 판정하지 않는다.
	shadowV2MinPulses = 3
)

// shadowStopV2Block — r3 펄스별 점수 → v2 판정. 점수가 없거나 너무 짧으면 nil.
//
// scores 는 펄스 순서대로이며 nil 원소는 채점 실패(그 펄스는 판정에서 제외하되
// 연속 카운트를 올리지도 리셋하지도 않는다 — v1 의 무배출 처리와 같은 이유로,
// 정보가 없는 펄스가 판단을 좌우해선 안 된다).
func shadowStopV2Block(scores []*float64) map[string]any {
	if len(scores) < shadowV2MinPulses {
		return nil
	}
	peak := 0.0
	scored := 0
	for _, s := range scores {
		if s == nil {
			continue
		}
		scored++
		if *s > peak {
			peak = *s
		}
	}
	if scored < shadowV2MinPulses || peak <= 0 {
		return nil
	}

	ratios := make([]any, len(scores))
	streak, stopAt := 0, 0
	for i, s := range scores {
		if s == nil {
			ratios[i] = nil
			continue
		}
		r := *s / peak
		ratios[i] = roundTo2(r)
		if r < shadowV2LowRatio {
			streak++
			if streak >= shadowV2LowStreak && stopAt == 0 {
				stopAt = i + 1
			}
			continue
		}
		streak = 0
	}

	return map[string]any{
		"rule":          "r3_cycle_decay_v2",
		"peak":          roundTo2(peak),
		"ratios":        ratios,
		"stop_at_pulse": stopAt,
		"low_ratio":     shadowV2LowRatio,
		"low_streak":    shadowV2LowStreak,
		"scored":        scored,
		"shadow":        true,
	}
}
