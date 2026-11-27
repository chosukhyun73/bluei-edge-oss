package feed_cycle

// 섀도 중단 판정 v1 — 배출 확인과 반응 판정을 분리한다 (2026-08-03 운영자).
//
// 종전 섀도(shadow_judge.go, no_response_streak_v0)는 "요동 반응이 없으면 중단"만
// 봤다. 그런데 반응이 없는 데는 두 가지 원인이 있고 조치가 정반대다.
//
//	배불러서 안 먹음   → 중단이 맞다
//	사료가 안 나와서 못 먹음 → 중단하면 안 된다. 급이기를 고쳐야 한다
//
// 8-03 10:00 tank_02 가 후자였다. 6발 중 배출 1발(운영자 확인 `배출=none`)인
// 급이기 고장 사이클인데 v0 는 priors 0.04~0.10 을 보고 "p2 중단"을 냈다. 실제로
// 배선돼 있었다면 고장을 만복으로 기록하고 급이를 끊었을 것이다. 같은 날 만복
// 게이트도 같은 착각으로 승인된 보상 2발을 취소했다.
//
// 두 원인을 가르는 신호는 **절대 상승폭**이다. 사료가 안 나오면 어체가 자발적으로
// 움직여도 수면 요동의 절대 크기가 바닥을 벗어나지 못한다. 반면 상대 상승률은
// 자발적 움직임을 배출로 읽는다(8-03 11:00 tank_02 p6 = 첫 거짓 yes 실측).
//
// 그래서 펄스마다 두 단계로 본다.
//
//	① 배출 판정  차트값 < shadowDischargeMin  → 무배출
//	② 반응 판정  배출 확인된 펄스만 모아 반응 강도를 본다
//	             무배출 펄스는 제외 — 사료가 없었으니 식욕 정보가 없다
//	③ 중단      배출 확인 펄스가 연속 shadowLowStreak 발 낮으면 만복 중단
//	④ 경보      무배출이 잦으면 중단이 아니라 급이기 이상
//
// 기록만 — 실제 중단은 하지 않는다. v0 결과도 나란히 남겨 비교한다.

import (
	"context"
	"encoding/json"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

const (
	// shadowDischargeMin — ① 배출 확인 하한 (차트 스케일)의 **상한**.
	// 14일 운영자 라벨 718펄스 실측: 무배출 중앙 0.068 / 배출 중앙 0.377.
	shadowDischargeMin = 0.20

	// shadowDischargeRel — ① 하한을 그 사이클 식욕 수준에 연동하는 비율.
	//
	// 고정 0.20 은 저식욕 사이클에서 무너진다. 어체가 잘 안 먹으면 사료가 떨어져도
	// 요동이 작아서 배출 펄스가 통째로 하한 아래로 내려간다 — 8-03 12:00 tank_02 는
	// 운영자가 6발 전부 배출로 봤는데 고정 0.20 은 4발을 무배출로 판정했다.
	//
	// 식욕 수준별 실측 (사이클 최고 차트값 기준):
	//	고식욕 ≥0.45   검출  73%  오탐  6%
	//	중식욕 0.30~   검출  91%  오탐  8%
	//	저식욕 <0.30   검출 100%  오탐 44%   ← 여기만 무너진다
	//
	// 그런데 저식욕 구간에서도 분포는 갈린다(무배출 중앙 0.051·P75 0.068 vs
	// 배출 중앙 0.211·P25 0.137). 임계 위치가 틀렸을 뿐이라 사이클 최고값에 연동한다.
	shadowDischargeRel = 0.40

	// shadowDischargeAbsMin — ① 하한의 **바닥**. 연동만 하면 급이기가 완전히 막힌
	// 사이클이 스스로를 정규화해버린다 — 8-03 10:00 tank_02(값 0.09~−0.05, 운영자
	// 전량 무배출)는 peak 0.09 라 하한이 0.036 까지 내려가 무배출이 배출로 뒤집힌다.
	// 바닥을 두면 그 사이클은 전량 무배출로 남는다.
	//
	// clamp(peak×0.40, 0.12, 0.20) 실측 (14일 718펄스):
	//	           검출  오탐        저식욕오탐  무배출사이클 검출
	//	고정 0.20   86%  62 (10%)    24/55       15/16
	//	이 식        81%  36 ( 6%)     7/55       15/16
	// 오탐 42%·저식욕 오탐 71% 줄이면서 급이기 고장 검출은 그대로 유지한다.
	shadowDischargeAbsMin = 0.12

	// shadowLowReaction — ② 낮은 반응 기준의 **코드 기본값**.
	// 수조별 덮어쓰기는 config feed_cycle.shadow_stop_by_tank (2026-08-03).
	// 배출 확인 612펄스의 P25 = 0.284. 만복이 진행되며 펄스별 중앙값이 이 선을
	// 교차하는 지점이 p6(0.284)이고, 사이클 마지막 배출펄스 중앙값(0.303)과도 맞다.
	shadowLowReaction = 0.30

	// shadowLowStreak — ③ 연속 몇 발이 낮으면 중단으로 볼지의 **코드 기본값**.
	// ⚠ shadowLowReaction 과 한 세트다. 8-03 실측에서 임계만 보면 이르게 보였으나
	// (v1 0.30 ↔ 운영자 점수 0.35~0.45) 연속 2발 조건이 지연을 만들어 정답이었다
	// (13:43 정답 p5 = v1 p5 · 14:40 정답 p6 = v1 중단없음, 2/2).
	shadowLowStreak = 2

	// shadowFaultMinNoDischarge — ④ 급이기 이상 의심 최소 무배출 발수.
	shadowFaultMinNoDischarge = 3

	// ── 배출 판정 대안 지표: AUC (2026-08-05 운영자 관찰) ───────────────────────
	//
	// 운영자: "무배출일 때 움직임의 간격이 매우 좁다. 사료가 절대적으로 부족하거나
	// 배출이 정상적이지 않다는 표시다." (8-05 12:00 tank_02 차트)
	//
	// 최고 상승폭은 **순간 반응**만 본다. 사료가 배출구에 몇 알만 떨어져도 어체가
	// 순간 몰려들어 봉우리는 어느 정도 나오고, 곧 흩어져 기저로 복귀한다. 반면
	// 제대로 배출되면 계속 먹느라 활동이 넓게 유지된다 — 그 차이가 **면적**이다.
	//
	// 라벨 75펄스 실측 (무배출 6 · 배출 69), 사이클 내 최고 대비 비율 하한:
	//	지표          최적비율  검출   오탐
	//	최고상승폭      0.75    6/6   21/69   ← 현행이 보는 것
	//	창내평균        0.65    6/6   18/69
	//	AUC           0.50    6/6    8/69   ← 같은 검출에 오탐 62% 감소
	// 무배출↔배출 중앙값 대비도 최고상승폭 3.1배 vs AUC 8.6배로 벌어진다.
	//
	// ⚠ 표본이 무배출 6건뿐이라 **기록만** 한다. 현행 판정은 최고상승폭 그대로이고,
	// 두 결과를 나란히 남겨 대조한 뒤 교체를 판단한다.

	// aucWindowStartS/EndS — 면적 관찰 창. 표준 반응창(+10~20s)보다 넓게 잡아
	// "얼마나 오래 유지되는가"를 담는다. 펄스 간격이 약 70s 라 다음 펄스와 겹치지 않는다.
	aucWindowStartS = 5
	aucWindowEndS   = 40

	// aucDischargeRel — AUC 하한 = 사이클 최고 AUC × 이 비율. 위 실측의 최적점.
	// 절대 상수를 두지 않는다 — 면적은 사이클 활동 수준에 따라 자릿수가 달라진다.
	aucDischargeRel = 0.50

	// widthRelHeight — 폭(지속 시간) 측정 높이. 그 펄스 최고 상승폭의 이 비율을
	// 넘긴 초 수를 센다. 절대 높이로 재면 활동 수준이 다른 사이클끼리 비교가 안 된다.
	//
	// 왜 폭을 따로 재는가 (2026-08-05 실측). AUC 만으로는 **만복 저반응**과
	// **무배출**이 안 갈린다 — 둘 다 AUC 비 0.36 인 사례가 나왔다:
	//	12:30 t01 p6  만복(배출확인·운영자 0.30)  AUC비 0.36 · 폭 28초  ← 낮고 넓다
	//	12:00 t02 p1  무배출(운영자 라벨)          AUC비 0.36 · 폭 19초  ← 뾰족하고 좁다
	//	12:00 t02 p3·p5 무배출                    AUC비 0.11~0.15 · 폭 8~14초
	//	정상 배출 (다수)                          AUC비 0.68~1.00 · 폭 27~35초
	// 사료가 몇 알만 떨어지면 어체가 순간 몰렸다 흩어져 봉우리가 좁고, 만복이면
	// 사료는 있으니 접근이 오래 이어지되 강도만 약하다. 운영자 관찰과 일치한다:
	// "무배출일 때 움직임의 간격이 매우 좁다."
	widthRelHeight = 0.30

	// ── 섀도 중단 판정 v3: 직전 대비 낙폭 (2026-08-05 운영자) ─────────────────
	//
	// 운영자: "배출에도 불구하고 급이 반응이 낮아지기 시작하면 멈추면 된다.
	// 얼마나 이전보다 떨어지는가가 중요한 지표다. 서서히 감소하는 경우에는
	// 2연속 감소하면 중단하고 다음 턴에 급이하면 된다."
	//
	// v1/v2 가 실패한 이유가 여기 있다. 둘 다 **절대 수준**(임계 미만)을 보는데,
	// 12:30 t01 은 p1~p5 가 평탄(운영자 0.65~0.75)하다 p6 에서 0.30 으로 급락했다.
	// 절대 임계는 평탄 구간을 계속 스쳐 p4 에서 헛중단을 냈고, 정작 급락은 못 봤다.
	//
	// ⚠ 사전 예측은 불가능하다 (운영자: "p6 를 중단할 기준은 없다. p5 에서 반응이
	// 좋았고 점차 낮아지던 상황도 아니라 기대할 수 없다"). 그래서 v3 의 목표는
	// **급락한 그 발을 마지막으로 삼는 것**이지 그 앞에서 미리 끊는 것이 아니다.
	//
	// ⚠ 배출 확인된 펄스만 본다. 무배출로 값이 떨어진 것은 식욕 정보가 아니다.
	//
	// ⚠ 값은 9건(그중 "멈췄어야" 1건)에 맞춘 것이 아니라 운영자 논리를 그대로 옮긴
	// 것이다. 표본이 쌓이면 재산출한다. 격자 탐색은 아직 "안 멈춤" 구석으로 수렴한다.

	// shadowV3DropHard — 직전 대비 이 비율 미만이면 급락으로 보고 그 발에서 중단.
	// 12:30 t01 실측: 운영자 0.75→0.30(0.40) · 요동 0.26→0.15(0.58).
	shadowV3DropHard = 0.60
	// shadowV3DropDecline — "유의한 감소" 기준. 노이즈 수준의 미세 감소를 세지 않는다.
	shadowV3DropDecline = 0.90
	// shadowV3DeclineStreak — 유의한 감소가 몇 발 연속이면 중단할지.
	shadowV3DeclineStreak = 2

	// shadowAnchorDrop — 최초 배출 펄스 대비 이 비율 미만이면 그 발에서 중단.
	// 운영자 제안(2026-08-11): "최초 펄스에서 40% 이상 하락하면 중단."
	//
	// ⚠ 섀도 전용. 소급 검증(오염 없는 12사이클, 시간순 절반 분할)에서 지표에 따라
	// 결과가 갈렸다 — 검증 6사이클 기준 auc 결식 2·절감 3, rise 결식 5·절감 1,
	// mean 은 값이 평평해 임계 0.3 까지 내려도 무발동. 표본이 작아(중단 필요 5사이클)
	// 임계를 확정할 수 없으므로 실제 중단에는 쓰지 않는다.
	shadowAnchorDrop = 0.60

	// shadowSlopeWindow / shadowSlopeDrop — 최근 N 발의 정규화 기울기(발당 감소율)가
	// 이 값 미만이면 중단으로 본다.
	//
	// 한 발 급락은 중단 신호가 못 된다 — 배출 펄스 열로 실측하니 직전 대비 0.5 미만
	// 급락 29건 중 19건(79%)이 다음 발에서 되살아났다. 깊이 떨어질수록 오히려 회복
	// 확률이 높다(0.4 임계에서 83%). 사람이 차트에서 보는 것은 한 발이 아니라 여러
	// 발의 흐름이므로 기울기로 잰다.
	//
	// 값은 **운영자 정답지 기준 out-of-sample** 로 골랐다 (clean 13사이클, 시간순
	// 절반 분할, 학습 6 / 검증 7, 임계 고정).
	//
	//   지표 창  임계 | 검증 결식  낭비  거짓중단  절감
	//   rise  4 -0.20 |    0       5      0       2   ← 채택
	//   auc   4 -0.20 |    1       4      1       3
	//   rise  4 -0.25 |    0       6      0       1
	//   rise  3 -0.10 |   12       4      4       3   ← 결식 폭증. 쓰면 안 된다
	//   대조군(직전 배출펄스 대비 0.5)  0  6  0  1
	//
	// ⚠ -0.10 은 "발동 후 다음 발이 회복했는가"라는 대리 지표로는 최고였으나(거짓 27%)
	// 정답지로 재니 정반대였다. 대리 지표로 임계를 고르지 말 것 — 2026-08-12 하루에만
	// 같은 함정에 세 번 빠졌다(하한 캘리브레이션·앵커 임계·이 값).
	//
	// ⚠ 여전히 섀도다. 검증 표본이 7사이클뿐이다.
	shadowSlopeWindow = 4
	shadowSlopeDrop   = -0.20

	// shadowRiseHeadroom — 차트와 동일한 표시 여유 (InferenceMonitor.tsx RISE_HEADROOM).
	// P90 을 0.71 에 놓아 위쪽 여유를 남긴다. 화면 눈금과 임계를 같은 자로 맞추기
	// 위한 것 — 운영자가 차트에서 본 높이가 곧 이 판정의 입력이어야 한다.
	shadowRiseHeadroom = 1.4

	// ── 섀도 중단 판정: 절대 수준 (2026-08-18 운영자 지시 A안) ────────────────
	//
	// 기존 네 규칙(v3·anchor_rise·anchor_auc·slope)은 전부 **직전 대비 낙폭**만 본다.
	// 8-15~17 24사이클로 검증하니 넷 다 귀무 기준을 넘지 못했다:
	//
	//	교차축(AUC) 채점   규칙 0.68~0.71  ↔  아무 지점에서나 절단 0.68~0.70
	//	고정 5발과 비교     v3 는 이기는 사이클 4 · 지는 사이클 7 (평균차 +0.051)
	//	최적 절단점 적중    1~3/24 (±1발까지 넓혀도 5~8/24)
	//
	// 사이클 안에서 반응은 원래 뒤로 갈수록 떨어진다. "중단 이후가 더 낮더라"는
	// 모든 절단점에서 참이라 정보가 없었다. 비율 규칙의 또 다른 결함은 **반등을
	// 죽인다**는 것이다 — 8-17 15:37 tank_01 은 p4 에서 꺼졌다 p5~p8 에 되살아났는데
	// 비율 규칙은 5발에서 끊었다.
	//
	// 그래서 자를 바꾼다: 직전 대비가 아니라 **그 수조의 평소 반응 대비**.
	// 일시적 하강은 평소선 위에 머물러 통과하고, 진짜 식욕 저하만 아래로 내려간다.
	//
	// 라이브 조건 실측 (낮은 펄스 '다음'부터 차단 — 그 펄스 자신은 이미 나갔다):
	//	              적합 8-15~16        시험 8-17~18       급이감소
	//	절대 0.4×평소  정밀도 50%          정밀도 83%          -16.7%
	//	절대 0.5×             52%                 62%          -22.2%
	//	절대 0.6×             57%                 70%          -27.9%
	//	고정 5발(귀무)         21%                 26%          -37.5%
	// "나쁜 펄스" 정의를 0.4~0.7 로 바꿔도 절대규칙 60~85% vs 귀무 22~37% 로 유지된다.
	//
	// 0.4 를 고른 이유: 시험구간 정밀도가 가장 높고 급이 감소가 가장 작다. 이 규칙은
	// **어체가 안 먹는 것과 곡선이 나빠진 것을 구분하지 못하므로**(8-17 13:43~14:53 은
	// 두 수조에서 동시에·기저가 함께 오른 채 벌어졌고 원인 미확인) 보수적으로 잡는다.
	shadowAbsFactor = 0.40

	// shadowAbsRefMinCycles — 평소 반응 산출에 필요한 최소 사이클 수. 미달이면 미판정.
	shadowAbsRefMinCycles = 6

	// shadowAbsRefWindow — 평소 반응 산출용 사이클 보고 조회 건수(두 수조 합산).
	// 수조당 약 4사이클/일이므로 60건이면 수조당 1주일 안팎이 남는다.
	//
	// ⚠ 이동 중앙값이라 저반응이 며칠 이어지면 기준선도 따라 내려가 규칙이 무뎌진다.
	// 섀도 기간에 ref 드리프트를 함께 기록해 관찰한 뒤 고정 여부를 정한다.
	shadowAbsRefWindow = 60
)

// resolveShadowStop — 반응 임계 해석. 수조별 → 공유 → 코드 기본값 순.
// 항목별로 따로 폴백한다 (수조별로 연속 조건만 바꾸는 것도 가능해야 하므로).
func (rp *Reporter) resolveShadowStop(tankID string) (float64, int, string) {
	low, streak, src := shadowLowReaction, shadowLowStreak, "default"
	if d := rp.shadowStop; d.LowReaction > 0 || d.LowStreak > 0 {
		if d.LowReaction > 0 {
			low, src = d.LowReaction, "shared"
		}
		if d.LowStreak > 0 {
			streak, src = d.LowStreak, "shared"
		}
	}
	if t, ok := rp.shadowStopByTank[tankID]; ok {
		if t.LowReaction > 0 {
			low, src = t.LowReaction, "tank"
		}
		if t.LowStreak > 0 {
			streak, src = t.LowStreak, "tank"
		}
	}
	return low, streak, src
}

// shadowStopBlock — 섀도 중단 판정 v1 결과. 판정 불가면 nil.
func (rp *Reporter) shadowStopBlock(ctx context.Context, tankID string, segs []Segment, curves []agitationCurve) map[string]any {
	// 공용 P90 은 앵커가 없는 카메라의 폴백으로만 남는다. 앵커가 전부 있으면
	// P90 산정 실패도 판정을 막지 않는다.
	p90Scale, hasP90 := rp.surfaceRiseScale(ctx)
	usedAnchor, usedP90 := false, false
	usedScales := map[string]float64{}
	scaleFor := func(cam string) float64 {
		if a := rp.riseAnchorByCamera[cam]; a > 0 {
			usedAnchor = true
			usedScales[cam] = roundTo2(a)
			return a
		}
		if hasP90 {
			usedP90 = true
			usedScales[cam] = roundTo2(p90Scale)
			return p90Scale
		}
		return 0
	}

	values := make([]float64, 0, len(segs))
	for _, sg := range segs {
		v, has := segmentChartValue(sg, curves, scaleFor)
		if !has {
			return nil // 한 펄스라도 창 밖이면 열이 끊겨 연속 판정이 무의미
		}
		values = append(values, roundTo2(v))
	}

	// 자의 출처 — 앵커 대비 값인지 옛 P90 대비 값인지 모르면 사후 비교가 불가능하다.
	scaleSource := "none"
	switch {
	case usedAnchor && usedP90:
		scaleSource = "mixed"
	case usedAnchor:
		scaleSource = "anchor"
	case usedP90:
		scaleSource = "p90"
	}

	minV := dischargeFloor(values)
	lowReaction, lowStreak, src := rp.resolveShadowStop(tankID)
	discharged, noDischarge, stopAt := judgeDischargeGated(values, minV, lowReaction, lowStreak)

	block := map[string]any{
		"rule":             "discharge_gated_streak_v1",
		"scale":            roundTo2(p90Scale), // 폴백 P90 (앵커 사용 시 참고값)
		"scale_source":     scaleSource,        // anchor | p90 | mixed — 값 비교 시 반드시 확인
		"scale_by_camera":  usedScales,         // 실제로 나눈 값
		"values":           values,
		"discharged":       discharged,
		"no_discharge":     noDischarge,
		"stop_at_pulse":    stopAt,
		"feeder_fault":     noDischarge >= shadowFaultMinNoDischarge || noDischarge*2 >= len(values),
		"discharge_min":    roundTo2(minV),
		"low_reaction":     lowReaction,
		"low_streak":       lowStreak,
		"threshold_source": src, // tank | shared | default — 수조별 분화 추적용
		"shadow":           true,
	}
	// AUC 기준 배출 판정 — 비교 기록용. 위 판정에는 영향을 주지 않는다.
	if auc := dischargeAUCBlock(segs, curves); auc != nil {
		block["discharge_auc"] = auc
		// 최초 배출 펄스 대비 낙폭 — AUC 축. 소급 검증에서 상승폭(rise)축보다
		// 나았던 쪽이라 함께 남긴다.
		av, okV := auc["values"].([]float64)
		ad, okD := auc["discharged"].([]bool)
		if okV && okD {
			if a := shadowStopAnchorBlock(av, ad); a != nil {
				block["stop_anchor_auc"] = a
			}
		}
	}
	// 낙폭 기반 중단 판정 v3 — 비교 기록용.
	if v3 := shadowStopV3Block(values, discharged); v3 != nil {
		block["stop_v3"] = v3
	}
	// 최초 배출 펄스 대비 낙폭 — 상승폭(차트값) 축. 두 축을 나란히 기록해야
	// 어느 지표로 임계를 잡을지 사후에 가릴 수 있다.
	if a := shadowStopAnchorBlock(values, discharged); a != nil {
		block["stop_anchor_rise"] = a
	}
	// 추세(기울기) 축 — 한 발 비교가 아니라 여러 발의 흐름. 상승폭 축으로만 채점했으므로
	// 여기서도 상승폭만 본다 (AUC 축은 검증 후에 추가할 것).
	if s := shadowStopSlopeBlock(values, discharged); s != nil {
		block["stop_slope"] = s
	}
	// 절대 수준 축 — 위 규칙들이 전부 "직전 대비"라 사이클의 자연 하강과 구분되지
	// 않았다(상수 주석 참조). 그 수조의 평소 반응을 자로 쓴다.
	if ref, cycles, ok := rp.tankNormalRise(ctx, tankID); ok {
		if a := shadowStopAbsoluteBlock(values, ref, cycles); a != nil {
			block["stop_absolute"] = a
		}
	}
	return block
}

// dischargeFloor — ① 배출 확인 하한 = clamp(사이클최고 × rel, absMin, min).
// 위로는 저식욕 사이클에서 배출 펄스가 통째로 잘리는 것을, 아래로는 급이기가
// 막힌 사이클이 스스로를 정규화하는 것을 막는다 (상수 주석의 실측 참고).
func dischargeFloor(values []float64) float64 {
	peak := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}
	f := peak * shadowDischargeRel
	if f < shadowDischargeAbsMin {
		return shadowDischargeAbsMin
	}
	if f > shadowDischargeMin {
		return shadowDischargeMin
	}
	return f
}

// judgeDischargeGated — ①~③ 판정 본체. 펄스별 차트값 열 → (배출확인, 무배출 발수,
// 중단 펄스). 중단 펄스 0 = 중단 없음. minV = dischargeFloor 가 정한 배출 하한.
//
// 무배출 펄스는 연속 카운트를 **올리지도 리셋하지도 않는다**. 사료가 안 나온 펄스는
// 식욕에 대한 정보가 없으므로, 배출된 펄스들만 이어 붙여 판단한다.
func judgeDischargeGated(values []float64, minV, lowReaction float64, lowStreak int) (discharged []bool, noDischarge, stopAt int) {
	discharged = make([]bool, len(values))
	streak := 0
	for i, v := range values {
		if v < minV {
			noDischarge++
			continue
		}
		discharged[i] = true
		if v < lowReaction {
			streak++
			if streak >= lowStreak && stopAt == 0 {
				stopAt = i + 1
			}
			continue
		}
		streak = 0
	}
	return discharged, noDischarge, stopAt
}

// segmentChartValue — 펄스의 차트값 = (반응창 최고 − 기저) / 자, 표면 곡선 max.
// 창은 배출 판정과 동일한 +10~20s (discharge_check.go) — 같은 자를 써야 임계가
// 옮겨 붙는다. 수중 곡선은 제외한다(어체 근접 통과로 포화).
//
// 자는 카메라마다 다르다 — scaleFor 가 앵커(있으면) 또는 공용 P90(폴백)을 준다.
func segmentChartValue(sg Segment, curves []agitationCurve, scaleFor func(string) float64) (float64, bool) {
	best, found := 0.0, false
	for _, c := range curves {
		if isUnderwaterCamera(c.CameraID) || c.Baseline <= 0 {
			continue
		}
		scale := scaleFor(c.CameraID)
		if scale <= 0 {
			continue
		}
		r := int(sg.StartAbs.Sub(c.StartAt).Seconds())
		post := sliceRange(c.Series, r+dischargePostStartS, r+dischargePostEndS)
		if len(post) == 0 {
			continue
		}
		peak := post[0]
		for _, v := range post {
			if v > peak {
				peak = v
			}
		}
		if v := (peak - c.Baseline) / scale; !found || v > best {
			best, found = v, true
		}
	}
	return best, found
}

// segmentAUC — 펄스의 기저 위 면적과 폭. 창은 [+5s, +40s].
//
//	면적(auc)  Σ max(0, 곡선 − 기저)
//	폭(width)  기저 + 최고상승폭×widthRelHeight 를 넘긴 초 수
//
// 표면 곡선 중 면적이 가장 큰 것을 채택. 창 밖이면 ok=false.
// 자(스케일)를 나누지 않는다 — 판정이 사이클 최고 대비 비율이라 상쇄된다.
func segmentAUC(sg Segment, curves []agitationCurve) (auc float64, width int, ok bool) {
	for _, c := range curves {
		if isUnderwaterCamera(c.CameraID) || c.Baseline <= 0 {
			continue
		}
		r := int(sg.StartAbs.Sub(c.StartAt).Seconds())
		win := sliceRange(c.Series, r+aucWindowStartS, r+aucWindowEndS)
		if len(win) == 0 {
			continue
		}
		sum, peak := 0.0, 0.0
		for _, v := range win {
			d := v - c.Baseline
			if d > 0 {
				sum += d
			}
			if d > peak {
				peak = d
			}
		}
		w := 0
		if peak > 0 {
			cut := c.Baseline + peak*widthRelHeight
			for _, v := range win {
				if v > cut {
					w++
				}
			}
		}
		if !ok || sum > auc {
			auc, width, ok = sum, w, true
		}
	}
	return auc, width, ok
}

// dischargeAUCBlock — AUC 기준 배출 판정(섀도 비교용). 판정 불가면 nil.
// 하한 = 사이클 최고 AUC × aucDischargeRel. 현행 최고상승폭 판정과 나란히 기록한다.
func dischargeAUCBlock(segs []Segment, curves []agitationCurve) map[string]any {
	vals := make([]float64, 0, len(segs))
	widths := make([]int, 0, len(segs))
	for _, sg := range segs {
		v, w, ok := segmentAUC(sg, curves)
		if !ok {
			return nil // 한 펄스라도 비면 사이클 최고값이 왜곡된다
		}
		vals = append(vals, v)
		widths = append(widths, w)
	}
	peak := 0.0
	for _, v := range vals {
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		return nil
	}
	floor := peak * aucDischargeRel
	discharged := make([]bool, len(vals))
	no := 0
	rounded := make([]float64, len(vals))
	for i, v := range vals {
		rounded[i] = roundTo2(v)
		if v < floor {
			no++
			continue
		}
		discharged[i] = true
	}
	return map[string]any{
		"rule":         "auc_relative_v1",
		"values":       rounded,
		"widths_s":     widths, // 폭(지속 초). 만복 저반응 vs 무배출을 가르는 축
		"width_rel":    widthRelHeight,
		"peak":         roundTo2(peak),
		"floor":        roundTo2(floor),
		"rel":          aucDischargeRel,
		"window_s":     []int{aucWindowStartS, aucWindowEndS},
		"discharged":   discharged,
		"no_discharge": no,
		"shadow":       true,
	}
}

// shadowStopV3Block — 직전 대비 낙폭 기반 중단 판정(섀도). 배출 확인 펄스만 잇는다.
// 급락 1발이면 그 발에서, 유의한 감소가 연속이면 그 발에서 중단으로 본다.
// 중단 펄스 = "마지막으로 준 발". 그 앞에서 미리 끊자는 것이 아니다.
func shadowStopV3Block(values []float64, discharged []bool) map[string]any {
	if len(values) != len(discharged) || len(values) < 2 {
		return nil
	}
	ratios := make([]any, len(values))
	prev, streak, stopAt, reason := 0.0, 0, 0, ""
	for i, v := range values {
		if !discharged[i] {
			ratios[i] = nil // 무배출 — 식욕 정보 없음. 비교에서 건너뛴다
			continue
		}
		if prev > 0 {
			r := v / prev
			ratios[i] = roundTo2(r)
			switch {
			case r < shadowV3DropHard:
				if stopAt == 0 {
					stopAt, reason = i+1, "sharp_drop"
				}
			case r < shadowV3DropDecline:
				streak++
				if streak >= shadowV3DeclineStreak && stopAt == 0 {
					stopAt, reason = i+1, "gradual_decline"
				}
			default:
				streak = 0
			}
		} else {
			ratios[i] = nil // 첫 배출 펄스 — 직전이 없다
		}
		prev = v
	}
	return map[string]any{
		"rule":           "drop_ratio_v3",
		"ratios":         ratios,
		"stop_at_pulse":  stopAt,
		"stop_reason":    reason, // sharp_drop | gradual_decline | ""
		"drop_hard":      shadowV3DropHard,
		"drop_decline":   shadowV3DropDecline,
		"decline_streak": shadowV3DeclineStreak,
		"shadow":         true,
	}
}

// slopeNorm — 값 열의 정규화 기울기(발당 감소율). 단순 선형회귀 기울기를 그 구간
// 평균으로 나눠 스케일을 없앤다 — 수조·카메라마다 절대값이 달라 원시 기울기는 비교 불가.
// 평균이 0 이하(전부 무반응)면 판정 불가.
func slopeNorm(ys []float64) (float64, bool) {
	n := len(ys)
	if n < 2 {
		return 0, false
	}
	mean := 0.0
	for _, y := range ys {
		mean += y
	}
	mean /= float64(n)
	if mean <= 0 {
		return 0, false
	}
	mx := float64(n-1) / 2
	num, den := 0.0, 0.0
	for i, y := range ys {
		dx := float64(i) - mx
		num += dx * (y - mean)
		den += dx * dx
	}
	if den == 0 {
		return 0, false
	}
	return (num / den) / mean, true
}

// shadowStopSlopeBlock — 최근 shadowSlopeWindow 발의 추세로 중단 판정(섀도).
//
// v3·앵커가 **직전 한 발**과 비교하는 것과 달리 여러 발의 흐름을 본다. 한 발이 튀어도
// 나머지가 붙잡아주므로 노이즈에 강하다 — 급락 단독의 거짓중단률 79% vs 27%.
//
// 무배출 펄스는 열에서 빼고 배출 확인 펄스만 이어 붙인다 (v3·앵커와 같은 규약).
func shadowStopSlopeBlock(values []float64, discharged []bool) map[string]any {
	if len(values) != len(discharged) || len(values) < 2 {
		return nil
	}
	seq := make([]float64, 0, len(values)) // 배출 확인 펄스만
	slopes := make([]any, len(values))
	stopAt := 0
	for i, v := range values {
		if !discharged[i] {
			continue // 무배출 — slopes[i] 는 nil 로 남는다
		}
		seq = append(seq, v)
		if len(seq) < shadowSlopeWindow {
			continue // 창이 아직 안 찼다
		}
		s, ok := slopeNorm(seq[len(seq)-shadowSlopeWindow:])
		if !ok {
			continue
		}
		slopes[i] = roundTo2(s)
		if s < shadowSlopeDrop && stopAt == 0 {
			stopAt = i + 1
		}
	}
	if len(seq) < shadowSlopeWindow {
		return nil // 배출 펄스가 창보다 적다 — 추세를 볼 수 없다
	}
	return map[string]any{
		"rule":          "slope_v1",
		"window":        shadowSlopeWindow,
		"slopes":        slopes,
		"stop_at_pulse": stopAt,
		"drop":          shadowSlopeDrop,
		"shadow":        true,
	}
}

// shadowStopAnchorBlock — 최초 배출 펄스 대비 낙폭 기반 중단 판정(섀도).
// v3 가 직전 배출펄스와 비교하는 것과 달리 앵커(첫 배출 펄스)를 고정 분모로 쓴다.
// 분모가 하나라 비율이 덜 흔들리고, 후반이 통째로 무배출이라 비교 입력이 사라지는
// v3 의 구조적 결함을 피한다.
//
// 무배출 펄스는 앵커로도 비교 대상으로도 쓰지 않는다 (v3 와 같은 규약 — 사료가 안
// 나온 펄스는 식욕 정보가 아니다).
//
// 임계 보정을 코드에 넣지 않고 ratios 를 그대로 남긴다. 펄스별 정상 감쇠로 나누는
// 보정안이 있으나, 기대값을 지금 상수로 박으면 표본 6~10개의 중앙값을 고정하는
// 것이라 그 자체가 과적합이다. 비율만 남기면 어떤 보정이든 소급 적용할 수 있다.
func shadowStopAnchorBlock(values []float64, discharged []bool) map[string]any {
	if len(values) != len(discharged) || len(values) < 2 {
		return nil
	}
	anchor, anchorPulse := 0.0, 0
	ratios := make([]any, len(values))
	stopAt := 0
	for i, v := range values {
		if !discharged[i] {
			continue // 무배출 — ratios[i] 는 nil 로 남는다
		}
		if anchorPulse == 0 {
			if v > 0 {
				anchor, anchorPulse = v, i+1
			}
			continue // 앵커 자신은 비교하지 않는다
		}
		r := v / anchor
		ratios[i] = roundTo2(r)
		if r < shadowAnchorDrop && stopAt == 0 {
			stopAt = i + 1
		}
	}
	if anchorPulse == 0 {
		return nil // 배출 확인 펄스가 없다 — 앵커를 잡을 수 없다
	}
	return map[string]any{
		"rule":          "anchor_drop_v1",
		"anchor":        roundTo2(anchor),
		"anchor_pulse":  anchorPulse,
		"ratios":        ratios,
		"stop_at_pulse": stopAt,
		"drop":          shadowAnchorDrop,
		"shadow":        true,
	}
}

// surfaceRiseScale — 표면 카메라 절대 상승폭 P90 × 여유. 차트(InferenceMonitor)와
// 같은 산식이라 화면 눈금과 판정 입력이 일치한다.
//
// 카메라별이 아니라 표면 카메라 전체를 모아 하나의 스케일을 쓴다. 카메라 차이는
// 덧셈형(노이즈 바닥, 실측 tank_02 9.4 vs tank_01 6.5)이라 (peak − 기저) 뺄셈에서
// 이미 상쇄된다 — 절대 상승폭 P90 은 5.22 vs 4.44 로 1.18배까지 좁혀지는 반면
// 상승률 P90 은 0.830 vs 0.502 로 1.65배 벌어진다. 카메라별로 나누면 같은 임계가
// 수조마다 다른 뜻이 되고, 활동이 낮아지면 그 카메라 P90 도 낮아져 판정이 오히려
// 관대해지는 되먹임도 생긴다.
func (rp *Reporter) surfaceRiseScale(ctx context.Context) (float64, bool) {
	if rp.store == nil {
		return 0, false
	}
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded,
		Limit:     riseP90Window,
	})
	if err != nil {
		return 0, false
	}
	var rises []float64
	for _, e := range evs {
		p, perr := vision_pipeline.ParseAgitationPayload(e.PayloadJSON)
		if perr != nil || p.Baseline <= 0 || isUnderwaterCamera(p.CameraID) {
			continue
		}
		rises = append(rises, p.Peak-p.Baseline)
	}
	p90, ok := percentile90(rises)
	if !ok {
		return 0, false
	}
	return p90 * shadowRiseHeadroom, true
}

// tankNormalRise — 그 수조의 "평소 반응" = 최근 사이클 보고에 남은 펄스 차트값의
// 중앙값. 자기 수조 안에서만 구한다 (카메라 시야·크롭이 달라 수조 간 비교는 무의미).
func (rp *Reporter) tankNormalRise(ctx context.Context, tankID string) (ref float64, cycles int, ok bool) {
	if rp.store == nil || tankID == "" {
		return 0, 0, false
	}
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeedCycleReport,
		Limit:     shadowAbsRefWindow,
	})
	if err != nil {
		return 0, 0, false
	}
	var vals []float64
	for _, e := range evs {
		var p struct {
			TankID      string `json:"tank_id"`
			ShadowJudge struct {
				V1 struct {
					Values []float64 `json:"values"`
				} `json:"v1"`
			} `json:"shadow_judge"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.TankID != tankID {
			continue
		}
		if len(p.ShadowJudge.V1.Values) == 0 {
			continue
		}
		vals = append(vals, p.ShadowJudge.V1.Values...)
		cycles++
	}
	if cycles < shadowAbsRefMinCycles {
		return 0, cycles, false
	}
	if ref = median(vals); ref <= 0 {
		return 0, cycles, false
	}
	return ref, cycles, true
}

// shadowStopAbsoluteBlock — 절대 수준 중단 판정 (섀도). 평소 반응 × shadowAbsFactor
// 아래로 내려간 **다음** 펄스부터 중단. 그 펄스 자신은 이미 나간 뒤에 판정되므로
// 막을 수 없다 — stop_at_pulse 는 급여를 허용한 발수다.
func shadowStopAbsoluteBlock(values []float64, ref float64, cycles int) map[string]any {
	if len(values) == 0 || ref <= 0 {
		return nil
	}
	cut := ref * shadowAbsFactor
	stopAt := 0
	for i, v := range values {
		if v < cut {
			stopAt = i + 1
			break
		}
	}
	if stopAt >= len(values) {
		stopAt = 0 // 마지막 펄스에서 걸리면 막을 것이 없다
	}
	return map[string]any{
		"rule":          "absolute_level_v1",
		"ref":           roundTo2(ref),
		"ref_cycles":    cycles,
		"factor":        shadowAbsFactor,
		"threshold":     roundTo2(cut),
		"stop_at_pulse": stopAt,
		"shadow":        true,
	}
}

func roundTo2(v float64) float64 {
	return float64(int(v*100+sign(v)*0.5)) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
