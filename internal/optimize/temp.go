// Package optimize — 최적환경조성 알고리즘. 안전제약 내에서 성장/에너지 최적을 권장한다.
// SHADOW: 권장값을 산출·기록만 하고 실제 장비 제어는 하지 않는다(안전 검증 우선).
// 안전 판정의 최종 책임은 rules engine 이며, 이 패키지는 권고(advisory)만 제공한다.
package optimize

import "fmt"

// TempParams — 수온 최적화 파라미터. 입식 후 어종/크기로 OptimalGrowthC 를 세팅.
type TempParams struct {
	OptimalGrowthC float64 `json:"optimal_growth_c"` // 어종·크기별 최적 성장 수온 (대서양연어 grow-out ~14)
	TempMinC       float64 `json:"temp_min_c"`       // 안전 하한
	TempMaxC       float64 `json:"temp_max_c"`       // 안전 상한(한계 수온)
	DOFloor        float64 `json:"do_floor"`         // DO 안전 하한 (mg/L)
	DOMargin       float64 `json:"do_margin"`        // 온도 상승 허용에 필요한 DO 여유분
	HysteresisC    float64 `json:"hysteresis_c"`     // 냉각 on/off 히스테리시스
	// MaxRampCPerHour — 수온 변화율 상한(℃/h). 열충격 방지 안전장치.
	// 자동제어 워커가 이 값 초과로 승온/냉각 시 냉각기를 반대로 잠깐 작동시켜 완화. 0 → 상한 없음.
	MaxRampCPerHour float64 `json:"max_ramp_c_per_hour"`
	// CoolingLeadMin — 냉각 정지 후 열관성으로 수온이 더 하강하는 시간(분). 냉각 중 하강률로
	// lead분 뒤 착지 온도를 예측해 목표에 닿도록 미리 정지(언더슈트 방지). 0 → 예측 정지 없음.
	CoolingLeadMin float64 `json:"cooling_lead_min"`
}

// DefaultTempParams — 대서양연어 RAS 일반값(입식 후 어종가이드로 보정).
func DefaultTempParams() TempParams {
	return TempParams{
		OptimalGrowthC:  14.0,
		TempMinC:        8.0,
		TempMaxC:        15.0,
		DOFloor:         6.5,
		DOMargin:        1.5,
		HysteresisC:     0.3,
		MaxRampCPerHour: 1.0, // 대서양연어 안전 변화율(℃/h) — 열충격 방지 기본값
		CoolingLeadMin:  7.0, // 냉각 정지 후 열관성 하강 시간(분) — 실측 코스팅(~7분) 기반
	}
}

type TempInputs struct {
	TankID       string
	CurrentTempC float64
	CurrentDO    float64
	HasDO        bool
}

// Action 권고.
const (
	ActionCool      = "cool"       // 온도 높음 → 냉각기 ON 권장
	ActionHold      = "hold"       // 목표 근처 → 유지
	ActionAllowRise = "allow_rise" // 목표보다 낮음 → 냉각기 OFF(자연상승) → 성장·에너지 이득
)

type TempRecommendation struct {
	TankID               string  `json:"tank_id"`
	CurrentTempC         float64 `json:"current_temp_c"`
	CurrentDO            float64 `json:"current_do,omitempty"`
	RecommendedSetpointC float64 `json:"recommended_setpoint_c"`
	Action               string  `json:"action"`
	ConstraintBinding    string  `json:"constraint_binding"` // do | temp_max | none
	Rationale            string  `json:"rationale"`
	EnergyNote           string  `json:"energy_note,omitempty"`
	Mode                 string  `json:"mode"` // 항상 "shadow"
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// RecommendTemp — 안전제약 내에서 성장/에너지 최적 수온 setpoint + 냉각기 동작 권고.
//
// 핵심(이중이득): 냉각기는 냉각만 하므로, 수온을 안전한 최고점까지 자연상승 허용하면
// 성장↑(성과) + 냉각기 에너지↓ 를 동시에 얻는다. 냉각기는 상한 초과 시에만 ON.
// 단 DO 여유가 부족하면 온도 상승을 제한한다(생존율 보호).
func RecommendTemp(in TempInputs, p TempParams) TempRecommendation {
	safeMax := p.TempMaxC
	binding := "temp_max"

	// DO 제약: DO 여유가 부족하면 온도를 더 올리지 않음(온도↑ → DO↓ → 위험).
	if in.HasDO && in.CurrentDO < p.DOFloor+p.DOMargin {
		if in.CurrentTempC < safeMax {
			safeMax = in.CurrentTempC
		}
		binding = "do"
	}

	setpoint := p.OptimalGrowthC
	if safeMax < setpoint {
		setpoint = safeMax
	}
	setpoint = clamp(setpoint, p.TempMinC, p.TempMaxC)

	rec := TempRecommendation{
		TankID:               in.TankID,
		CurrentTempC:         in.CurrentTempC,
		RecommendedSetpointC: setpoint,
		ConstraintBinding:    "none",
		Mode:                 "shadow",
	}
	if in.HasDO {
		rec.CurrentDO = in.CurrentDO
	}

	switch {
	case in.CurrentTempC > setpoint+p.HysteresisC:
		rec.Action = ActionCool
		rec.Rationale = fmt.Sprintf("현재 %.1f℃ > 목표 %.1f℃ → 냉각 권장", in.CurrentTempC, setpoint)
	case in.CurrentTempC < setpoint-p.HysteresisC:
		rec.Action = ActionAllowRise
		rec.Rationale = fmt.Sprintf("현재 %.1f℃ < 목표 %.1f℃ → 냉각기 OFF로 자연상승 허용", in.CurrentTempC, setpoint)
		rec.EnergyNote = "냉각기 OFF 권장 — 성장 촉진 + 에너지 절감(이중이득)"
	default:
		rec.Action = ActionHold
		rec.Rationale = fmt.Sprintf("목표 %.1f℃ 근처 유지", setpoint)
	}

	if binding == "do" {
		rec.ConstraintBinding = "do"
		rec.Rationale += fmt.Sprintf(" · DO %.1f 여유 부족(하한 %.1f) → 온도 상승 제한", in.CurrentDO, p.DOFloor)
	} else if setpoint >= p.TempMaxC {
		rec.ConstraintBinding = "temp_max"
	}
	return rec
}
