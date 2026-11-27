package feed_cycle

// 어종 급이/GET 프로필 (옵시디언 유래, 게이팅 배포). 온도구동 feed_rate% + DT80(GET).
//
// 확장 2a: 로더/모델만 추가하고 스케줄러는 아직 사용하지 않는다(동작 불변 — 안전).
// 2b 에서 computeNextCyclePlan 이 이 프로필 + 실시간 수온으로 전환된다.
//
// 인덱스/설정은 게이팅 경로(예: /var/lib/bluei-edge/knowledge/species-feeding.json)에서
// 로드된다. 파일이 없거나 경로가 비어있으면 빈 맵(자동급이는 기존처럼 안전 폴백).

import (
	"encoding/json"
	"math"
	"os"
)

// DT80Params — 위 배출 시간(GET) 지수모델 파라미터. dt80_h = k·W^(1-b)·exp(-c·T).
type DT80Params struct {
	Model string  `json:"model"`
	K     float64 `json:"k"`
	B     float64 `json:"b"`
	C     float64 `json:"c"`
}

// SpeciesFeedRule — 온도구간별 일일 급이율(%BW) + 펠릿.
type SpeciesFeedRule struct {
	TempMin     *float64 `json:"temp_min"`
	TempMax     *float64 `json:"temp_max"`
	FeedRatePct *float64 `json:"feed_rate_pct"`
	PelletMm    *float64 `json:"pellet_mm"`
	Note        string   `json:"note"`
}

// SpeciesFeeding — 한 어종(슬러그)의 급이/GET 프로필.
type SpeciesFeeding struct {
	Code         string            `json:"code"`
	Name         string            `json:"name"`
	Scientific   string            `json:"scientific"`
	DT80         *DT80Params       `json:"dt80"`
	FeedingRules []SpeciesFeedRule `json:"feeding_rules"`
}

type speciesFeedingConfig struct {
	Species map[string]SpeciesFeeding `json:"species"`
}

// LoadSpeciesFeeding — 슬러그키 급이 프로필 로드. 경로 빈 값/파일 없음 → 빈 맵(에러 아님).
func LoadSpeciesFeeding(path string) (map[string]SpeciesFeeding, error) {
	if path == "" {
		return map[string]SpeciesFeeding{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]SpeciesFeeding{}, nil
		}
		return nil, err
	}
	var c speciesFeedingConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Species == nil {
		c.Species = map[string]SpeciesFeeding{}
	}
	return c.Species, nil
}

// FeedRateAt — 현재 수온에 해당하는 온도구간의 일일 급이율(%BW)을 반환.
// ok=false 면 절식(매칭 구간 없음 또는 feed_rate 공백) → 급이하지 않음.
func (s SpeciesFeeding) FeedRateAt(tempC float64) (pct float64, ok bool) {
	for _, r := range s.FeedingRules {
		if r.TempMin != nil && tempC < *r.TempMin {
			continue
		}
		if r.TempMax != nil && tempC >= *r.TempMax {
			continue
		}
		if r.FeedRatePct == nil {
			return 0, false // 급이중단 구간
		}
		return *r.FeedRatePct, true
	}
	return 0, false
}

// DT80Hours — 체중(kg)·수온에서 위 배출 시간(h). DT80 미보유면 ok=false.
func (s SpeciesFeeding) DT80Hours(weightKg, tempC float64) (h float64, ok bool) {
	if s.DT80 == nil || s.DT80.K <= 0 {
		return 0, false
	}
	w := math.Max(0.001, weightKg)
	return s.DT80.K * math.Pow(w, math.Max(0.0, 1.0-s.DT80.B)) * math.Exp(-s.DT80.C*tempC), true
}
