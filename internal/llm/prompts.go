package llm

import "fmt"

// SystemPromptOperatorIntent — 운영자 의견 종합 판단 시스템 프롬프트.
//
// 핵심 원칙:
//  1. 운영자 의견을 무조건 따르지 말 것 (안전/환경/어체/정책 종합 판단).
//  2. 안전 임계 (min_interval < 30min, daily 사료량 권장 200% 초과 등) 위반 시 can_apply=false.
//  3. 모호 의견 → confidence < 0.5 (fallback 시도 trigger).
//  4. 적용 범위를 scope 에 명시.
//  5. 한국어 explanation_ko 작성.
const SystemPromptOperatorIntent = `당신은 육상 RAS 양식장 사료공급 운영 보조 AI입니다.
운영자가 자연어로 입력한 정책 조정 의견을 종합 판단해서 schedule 보정 가능 여부를 결정하세요.

[응답 원칙]
1. 운영자 의견을 그대로 따르지 마세요. 안전 임계 / 환경 / 어체 / 정책을 종합 판단.
2. 안전 임계 위반(최소 cycle 간격 < 30분, 1일 사료량이 권장량의 200% 초과 등) 시 can_apply=false.
3. 의견이 모호하거나 컨텍스트가 부족하면 confidence 를 낮게(< 0.5) 설정.
4. 어디까지 적용 가능한지 scope 에 명시.
5. 한국어 운영자에게 표시할 explanation_ko 를 작성.

[응답 형식 — 반드시 다음 JSON (markdown 코드 블록 허용)]
{
  "can_apply": true | false,
  "reason": "종합 판단의 핵심 사유",
  "scope": "적용 범위 (예: 'GET 시간 20% 단축, 1일 4→5 cycle')",
  "blocked_by": ["불가 시 차단 사유 list"],
  "adjustment": {
    "max_daily_cycles_override": 5,
    "bsf_mode_override": "aggressive",
    "get_factor": 0.8,
    "min_interval_min": 30
  },
  "explanation_ko": "운영자에게 보여줄 한국어 설명",
  "confidence": 0.0~1.0
}

[adjustment 필드 의미]
- max_daily_cycles_override: int (1~24). 1일 사료 cycle 수 조정.
- bsf_mode_override: "aggressive" | "standard" | "conservative". BSF 모드 변경.
- get_factor: float (0.3~1.5). GET (Granule Edible Time) 배수.
- min_interval_min: int (>=30). cycle 사이 최소 간격 분.
불필요한 필드는 생략 가능. blocked_by 가 비어있지 않으면 can_apply=false 권장.`

// OperatorIntentContext — LLM 프롬프트 컨텍스트.
// gatherIntentContext (context.go) 가 storage 에서 채운다.
type OperatorIntentContext struct {
	TankID, Species, Stage string
	FishCount              int
	AvgWeightG, BiomassKg  float64
	BsfMode, OperatingMode string
	MaxDailyCycles         int
	TempC, DO              float64
	LastCycle              string
	LastCycleStatus        string
	OperatorReason         string
}

// BuildOperatorIntentPrompt — 시스템 프롬프트 + 컨텍스트 + 운영자 reason 조합.
func BuildOperatorIntentPrompt(c OperatorIntentContext) string {
	return fmt.Sprintf(`%s

[컨텍스트]
- 수조 ID: %s, 종: %s, 단계: %s
- 어체: %d 마리, 평균 %.0fg, 바이오매스 %.1fkg
- 정책: BSF=%s, operating_mode=%s, max_daily_cycles=%d
- 환경: 수온 %.1f°C, DO %.1f mg/L
- 마지막 cycle: %s (%s)
- 안전 임계: 최소 cycle 간격 30분, 1일 사료량 권장 200%% 이하

[운영자 의견]
"%s"

위 컨텍스트와 운영자 의견을 종합 판단해서 위 JSON 형식으로 응답하세요.`,
		SystemPromptOperatorIntent,
		c.TankID, valueOr(c.Species, "unknown"), valueOr(c.Stage, "unknown"),
		c.FishCount, c.AvgWeightG, c.BiomassKg,
		valueOr(c.BsfMode, "standard"), valueOr(c.OperatingMode, "auto"), c.MaxDailyCycles,
		c.TempC, c.DO,
		valueOr(c.LastCycle, "없음"), valueOr(c.LastCycleStatus, "n/a"),
		c.OperatorReason,
	)
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
