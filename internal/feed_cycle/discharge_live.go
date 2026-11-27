package feed_cycle

// gap 끝 라이브 무배출 판정 + 보상 펄스 (2026-07-08 운영자 지시: "2초 진행으로 하고
// 미급이시 추가 펄스 진행").
//
// discharge_check.go 의 보고 시점 판정과 동일 휴리스틱(요동 국소상승, 동일 상수)을
// 라이브 요동 세션(vision_pipeline.LiveAgitation) series 에 적용해, gap 이 끝나는
// 시점에 직전 펄스의 배출 여부를 판정한다. "no" 확정 시에만 보상 — 오탐 가드:
// 스핀업창이 세션 기저 대비 미진정(>1.2×, gap_end_recovery 임계와 동일)이면
// unclear 로 보류한다 (과공급 연속 상황의 거짓 no 로 추가 급이하는 것 방지 —
// 2026-07-08 램프 실험에서 실측된 오판 모드).
//
// 마지막 펄스도 판정·보상 대상이다 (2026-07-20 운영자 결정, 종전 한계 해소):
// 판정 가능 사이클(lastPulseJudgeEligible)은 마지막 펄스 뒤에도 trailing gap 을
// 한 번 돌려 gap 끝 판정을 수행하고, 무배출이면 즉시 보상 펄스가 이어진다.
// 보상 상한 소진 등 판정 불가 조건에서만 종전대로 즉시 마무리한다.

import (
	"time"

	"bluei.kr/edge/internal/vision_pipeline"
)

// compensationSatiationFactor — 보상 펄스 만복 게이트: 발사 직전 라이브 활동도
// (last5 평균)가 세션 기저 ×이 값 이내로 복귀했으면 만복으로 보고 남은 보상을
// 폐기한다 (gap_end_recovery 임계 1.2 와 동일 계열). 2026-07-09 운영자 확정
// "배부르면 그만" — 실측: 15:00 tank_02 보상 2발이 만복 상태에 발화, 전량 침전.
const compensationSatiationFactor = 1.2

// 게이트 평가창 지연 (2026-07-16, 7-09 17:00 경계 오발사 근본 대책) — 직전 펄스
// 요동의 감쇠 꼬리 위에서 last5 를 재면 값이 부풀어 만복을 배고픔으로 오판한다
// (실측: 오발사 사이클에서 감쇠 구간 |last5-prev5|=6~14 vs 정착 후 0.2~0.7).
// 보상 발사 직전, 최근 5s 평균과 그 직전 5s 평균의 차가 기저×tolerance 이내로
// 정착할 때까지 폴링한 뒤 게이트를 평가한다. 상한 도달 시 미정착이어도 진행 —
// 지속 고활동은 실제 섭이 경쟁일 수 있고, 과급 위험은 사이클당 보상 상한으로 유계.
const (
	compensationSettleToleranceFactor = 0.10 // 정착: |last5-prev5| ≤ 기저×이 값
	compensationSettlePollMs          = 5000
	compensationSettleMaxWaitMs       = 30000
)

// dischargeGuardCalmFactor — 스핀업창 극단 포화 가드 (기저 ×2.0 초과면 판정 보류).
// 실측 보정 (2026-07-08 17:00 첫 라이브 사이클): ×1.2 는 배고픔 요동(1.28~1.34×)까지
// 전부 차단해 보상 0발. 과공급 포화(램프 p4·p5)도 1.30~1.33× 로 같은 대역이라 이
// 특징으로는 두 상황이 분리 불가 — 가드는 어체 렌즈 가림급 극단(×2+)만 막고,
// 통상 판정은 보고 경로(가드 없음, 16:00 육안 3/3 일치)와 동일하게 둔다.
// 잔여 과공급 위험은 사이클당 보상 상한(config)으로 유계.
const dischargeGuardCalmFactor = 2.0

// dischargeEvidence — 판정이 실제로 본 수. 종전에는 verdict 만 남아 판정이 왜 그렇게
// 났는지 사후에 알 수 없었고(라이브 세션은 사이클이 끝나면 사라진다), 8-13 에 원인을
// 세 번 지목해 세 번 물렸다. 이벤트에 그대로 실어 조회로 판별하게 한다 (2026-08-14).
// Valid=false 는 지표를 못 구한 경우(unknown 반환 경로).
type dischargeEvidence struct {
	Valid    bool
	Rise     float64 // max(반응창) − mean(기준창)
	Noise    float64 // 세션 series 표준편차 (임계의 자)
	PreMean  float64
	Baseline float64
}

// judgeLivePulseDischarge — 라이브 요동 series 로 run 시각 펄스의 배출 판정.
// 반환: "yes" | "unclear" | "no" | "unknown"(데이터 부족/판정 불가) + 판정 근거.
func judgeLivePulseDischarge(series []float64, sessionStart, pulseRunAt time.Time) (string, dischargeEvidence) {
	m := vision_pipeline.ComputeAgitationMetrics(series, 8)
	if m == nil {
		return "unknown", dischargeEvidence{}
	}
	sm := m.Series
	noise := pstdev(sm)
	if noise <= 0 {
		return "unknown", dischargeEvidence{}
	}
	r := int(pulseRunAt.Sub(sessionStart).Seconds())
	pre := sliceRange(sm, r+dischargePreStartS, r+dischargePreEndS)
	if len(pre) < dischargePreMinSamples {
		pre = sliceRange(sm, r+dischargePreFallbackStartS, r+dischargePreFallbackEndS)
	}
	post := sliceRange(sm, r+dischargePostStartS, r+dischargePostEndS)
	// 반응 창이 대부분 차기 전엔 판정하지 않는다 (gap 끝 호출이라 통상 충족).
	if len(pre) == 0 || len(post) < 8 {
		return "unknown", dischargeEvidence{}
	}
	preMean := meanOf(pre)
	ev := dischargeEvidence{
		Valid:    true,
		Rise:     maxOf(post) - preMean,
		Noise:    noise,
		PreMean:  preMean,
		Baseline: m.Baseline,
	}
	if m.Baseline > 0 && preMean > m.Baseline*dischargeGuardCalmFactor {
		return "unclear", ev // 미진정 — 직전 펄스 여파 위에서는 국소상승을 신뢰할 수 없음
	}
	switch {
	case ev.Rise > dischargeRiseYes*noise:
		return "yes", ev
	case ev.Rise > dischargeRiseUnsure*noise:
		return "unclear", ev
	default:
		return "no", ev
	}
}
