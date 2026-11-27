package feed_cycle

// 펄스별 실배출 판정 — 요동 국소상승 채널 (2026-07-08 운영자 지시: "급이 안됨을
// 명확하게 구분"). scripts/pulse_discharge_check.py 의 검증된 로직을 보고 경로에
// 이식한 것 (07-05 tank_01 수동 분석 + 07-08 13:00 tank_02 무배출 실측으로 검증).
//
// 원리: 배출이 있으면 [펄스 ON + 리드 + 착수] 창(현재 run+23~30s, 아래 상수 참조)에서
// 요동이 펄스 이전 기준선 대비 국소 상승한다. 무배출이면 상승 없음.
// 임계는 시리즈 전체 노이즈(σ) 비례 — 스크립트와 동일 (O >1.5σ / ~ >0.7σ / X).
//
// 기록 전용 — 사이클 실행·판정에 영향 없음. 실투입량 보정은 운영자 판단.

import (
	"math"
)

// 착수창 파라미터 — scripts/pulse_discharge_check.py 와 동기 유지.
// 2026-07-09 운영자 판정 기준으로 재정의: "신호 송출 후 10~20초 내 급이 반응
// 없으면 무배출". 기준선 창은 run 이전(-8~-1초) — 종전의 스핀업창(run+2~9)은
// 어체가 모터 소리를 학습한 조건반사(온셋 실측 +4~6초)로 오염되어 거짓 no 를
// 양산했다 (14:30 p5 운영자 육안 배출 vs 판정 no).
const (
	dischargePreStartS = -8 // 기준선 창 시작 (run 기준, 신호 이전)
	dischargePreEndS   = -1 // 기준선 창 끝
	// 2026-08-14 운영자 승인으로 10~20 → 23~30. 종전 창은 급이기 리드(10~25초)보다
	// 일러 사료가 물에 닿기 전을 재고 있었다. 8-12 까지는 라이브 곡선의 5.4% 시간
	// 압축이 이 어긋남을 우연히 상쇄했으나(인덱스 r+10~r+20 이 실제 r+27~r+38 을
	// 가리킴), 8-13 의 드리프트 수정(5a1c695)이 상쇄를 제거해 거짓 no 가 쏟아졌다
	// (8-13 tank_01 두 사이클에서 보상 상한 5발 소진 · 장부 차감 320.2g).
	// 검증: 8-13 12:30 tank_01 사이클을 운영자 라벨(discharge_modified=true)과 대조 —
	// 창 10~20 은 3/8, 23~30·23~31·23~33 은 모두 8/8 적중(진짜 무배출 p7 도 정확히 no).
	//
	// ⚠ 끝값이 33 인 이유 — sliceRange 는 [start,end) 이고 discharge_live.go 에
	// `len(post) < 8` 가드가 있다. 운영자 승인값 23~30 은 표본이 7개뿐이라 항상
	// 가드에 걸려 판정이 통째로 unknown 이 된다. 시작점 23 은 그대로 두고 폭만
	// 구 창과 같은 10초로 맞춰 가드를 건드리지 않았다.
	dischargePostStartS = 23 // 반응 창 시작 (운영자 기준)
	dischargePostEndS   = 33 // 반응 창 끝 (폭 10s — 가드 len>=8 충족)
	// yes 임계 1.5σ→1.2σ (2026-07-10 운영자 승인): 7-09 육안 대조에서 배출인데
	// unclear 로 처진 펄스 다수 (16:00 p1·p5 등). 거짓 no(반응 자체가 없는 펄스)
	// 에는 무효과 — 그건 대리신호의 구조 한계.
	dischargeRiseYes    = 1.2
	dischargeRiseUnsure = 0.7
	// 사이클 첫 펄스처럼 run 이전 표본이 부족하면 조건반사 시작(+4s) 전의
	// 초기 스핀업(+0~+3s)으로 폴백.
	dischargePreFallbackStartS = 0
	dischargePreFallbackEndS   = 3
	dischargePreMinSamples     = 3
)

// dischargeVerdicts — 펄스별 배출 판정. 곡선이 여럿이면 펄스별 최선 판정
// (한 카메라라도 착수를 봤으면 배출로 인정). 어느 펄스도 판정 불가면 nil.
func dischargeVerdicts(segs []Segment, curves []agitationCurve) []string {
	verdicts := make([]string, len(segs))
	any := false
	for i := range verdicts {
		verdicts[i] = "unknown"
	}
	for _, c := range curves {
		noise := pstdev(c.Series)
		if noise <= 0 {
			continue
		}
		for i, sg := range segs {
			r := int(sg.StartAbs.Sub(c.StartAt).Seconds())
			pre := sliceRange(c.Series, r+dischargePreStartS, r+dischargePreEndS)
			if len(pre) < dischargePreMinSamples {
				pre = sliceRange(c.Series, r+dischargePreFallbackStartS, r+dischargePreFallbackEndS)
			}
			post := sliceRange(c.Series, r+dischargePostStartS, r+dischargePostEndS)
			if len(pre) == 0 || len(post) == 0 {
				continue
			}
			rise := maxOf(post) - meanOf(pre)
			mark := "no"
			switch {
			case rise > dischargeRiseYes*noise:
				mark = "yes"
			case rise > dischargeRiseUnsure*noise:
				mark = "unclear"
			}
			if dischargeRank(mark) > dischargeRank(verdicts[i]) {
				verdicts[i] = mark
			}
			any = true
		}
	}
	if !any {
		return nil
	}
	return verdicts
}

// dischargeBlock — 보고 payload 블록. 판정 불가면 nil.
//
// 라이브 판정(maybeCompensatePulse)과 같은 맥락 게이트를 여기에도 건다 (2026-08-01).
// 종전에는 라이브만 게이트를 타서 같은 펄스가 라이브 unclear / 사후 no 로 갈렸고,
// 화면은 병합으로 가렸지만 이벤트·어시스턴트·분석 스크립트는 옛 값을 봤다(운영자 지적).
func (rp *Reporter) dischargeBlock(segs []Segment, curves []agitationCurve) map[string]any {
	verdicts := dischargeVerdicts(segs, curves)
	if verdicts == nil {
		return nil
	}
	raw := append([]string(nil), verdicts...)
	softened := 0
	skipped := 0
	if rp.gateEnabled {
		// 만복 전제조건 (2026-08-02) — 라이브와 같은 규칙. 앞선 펄스에서 배출(yes)이
		// 충분히 확인된 뒤에만 게이트를 건다. 두 채널이 갈리지 않도록 여기서도 적용한다.
		confirmed := 0
		for i, v := range verdicts {
			if v == "yes" {
				confirmed++
				continue
			}
			if v != "no" || i >= len(segs) {
				continue
			}
			if confirmed < rp.gateMinConfirmed {
				skipped++
				continue
			}
			if contextSaysDischarged(segs[i], curves, rp.gateRelMax, rp.gateVarMax) {
				verdicts[i] = "unclear"
				softened++
			}
		}
	}
	suspected := 0
	for _, v := range verdicts {
		if v == "no" {
			suspected++
		}
	}
	block := map[string]any{
		"channel":                "agitation_local_rise",
		"per_pulse":              verdicts, // yes | unclear | no | unknown(데이터 밖)
		"suspected_no_discharge": suspected,
	}
	if softened > 0 {
		// 상승폭 원본 보존 — 사후 분석 연속성 (라이브의 raw_verdict 와 같은 역할).
		block["raw_per_pulse"] = raw
		block["context_softened"] = softened
	}
	if skipped > 0 {
		block["gate_skipped_no_satiation"] = skipped
	}
	// 사이클 통째 판정 — 펄스별 판정이 포화로 무너지는 구간에서도 남는 신호.
	// 기록·경보 전용 (pattern_match.go 참고).
	if m, ok := pulsePatternMatch(segs, curves); ok {
		block["pattern_match"] = m
	}
	return block
}

func dischargeRank(mark string) int {
	switch mark {
	case "yes":
		return 3
	case "unclear":
		return 2
	case "no":
		return 1
	default: // unknown
		return 0
	}
}

func sliceRange(s []float64, start, end int) []float64 {
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	if end <= start {
		return nil
	}
	return s[start:end]
}

func meanOf(s []float64) float64 {
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	return sum / float64(len(s))
}

func maxOf(s []float64) float64 {
	m := s[0]
	for _, v := range s[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func pstdev(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	m := meanOf(s)
	sum := 0.0
	for _, v := range s {
		d := v - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(s)))
}
