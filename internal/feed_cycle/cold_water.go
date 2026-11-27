package feed_cycle

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 저수온 급이 보정 (2026-10-01, 운영자 결정).
//
// 배경: 9-17 13.5℃ → 10-01 8.7℃ 로 2주간 4.8℃ 하강(9-30 하루에만 1.8℃). 취수 수온
// 자체가 내려간 것이라 냉각기와 무관하고 되돌릴 수 없다. 종전 곡선은 8℃ 미만에서
// 급이율을 0 으로 떨어뜨려 절벽이 생겼고, 펄스 간격·횟수는 수온과 무관하게 고정이었다.
//
// 대사 속도가 기준이다 — 수온 10℃ 하락 = 대사 절반(Q10=2). 이 프로젝트가 소화시간
// (GET) 계산에 이미 쓰는 계수를 펄스 간격에도 그대로 적용한다.
const (
	coldMinTempC     = 6.0  // 이 아래로는 급이 중단 (운영자 결정: 6℃까지는 소량 유지)
	coldResumeTempC  = 6.3  // 재개 임계 — 경계에서 켜졌다 꺼졌다 하는 것 방지(히스테리시스)
	coldRateAtMinPct = 0.20 // 6.0℃ 일일 급이율(%BW)
	coldRateAt8Pct   = 0.50 // 8.0℃ — 기존 곡선과 맞물리는 지점

	gapRefTempC = 13.5 // 현행 간격(77초)이 조정된 기준 수온
	// 간격 확대 계수 — 대사 Q10(=2)보다 크게 잡는다(운영자 결정 2026-10-01).
	// 저수온에서는 대사 저하분만큼만 기다려서는 부족하고, 먹는 행동 자체가 느려져
	// 앞 펄스를 다 먹기 전에 다음이 나가는 것이 문제였다. 3.0 이면 6℃에서 176초로
	// 상한(180초)에 자연스럽게 닿아 상한이 구간을 잘라내지 않는다.
	//   13.5℃ 77초 · 8.7℃ 130초 · 8℃ 141초 · 6℃ 176초
	gapQ10   = 3.0
	gapMaxMs = 180000 // 간격 상한 3분 (운영자 결정)

	fewCyclesBelowTempC = 9.0 // 이 미만에서는 하루 급이 횟수를 줄인다
	fewCyclesCount      = 2

	rapidChangeDeltaC = 1.0 // 24시간 변화폭(절대값)이 이 이상이면 급변으로 본다
	rapidChangeFactor = 0.5 // 급변 당일 — 절반
	adaptationFactor  = 0.7 // 급변 다음 날 — 70% (소화 능력이 수온을 바로 못 따라온다)
)

// coldWaterRatePct — 6.0~8.0℃ 구간의 일일 급이율(%BW). 8℃ 이상은 0 을 반환해
// 기존 곡선(salmonGrowoutFeedRatePct)이 그대로 담당하게 한다.
//
// 8.0℃ 에서 0.50% 로 정확히 맞물리므로 절벽이 사라진다.
func coldWaterRatePct(tempC float64) float64 {
	if tempC < coldMinTempC || tempC >= 8.0 {
		return 0
	}
	f := (tempC - coldMinTempC) / (8.0 - coldMinTempC)
	return coldRateAtMinPct + f*(coldRateAt8Pct-coldRateAtMinPct)
}

// coldGapMs — 수온 보정 펄스 간격. 대사가 느려진 배수만큼 늘려 앞 펄스를 먹을
// 시간을 준다. 상한 3분.
//
//	13.5℃  77초 (기준)    8.7℃ 130초    8.0℃ 141초    6.0℃ 176초
func coldGapMs(baseGapMs int, tempC float64) int {
	if baseGapMs <= 0 || tempC >= gapRefTempC {
		return baseGapMs
	}
	scaled := float64(baseGapMs) * math.Pow(gapQ10, (gapRefTempC-tempC)/10.0)
	if scaled > gapMaxMs {
		scaled = gapMaxMs
	}
	return int(math.Round(scaled))
}

// coldDailyCycles — 저수온에서 횟수를 줄인다. 소화에 30시간 넘게 걸리는데 4회로
// 쪼개면 회당 양이 물고기가 인지하지 못할 만큼 작아진다(6℃·4회 = 회당 49g).
//
// ⚠ 단, 1일량은 불변이므로 횟수를 줄이면 회당 양이 그만큼 커진다. 수온만 보고
// 무조건 2회로 떨어뜨리면 "수온이 내려갔는데 회당 급이량이 늘어나는" 역전이 난다.
//
// 2026-10-10 실사고: 아침 8.85℃ 에서 tank_01 이 8회 → 2회로 꺾이며 회당 85g →
// 330g (1회최대량 163g 의 2배)이 됐다. 10:42 에 그 사이클이 13펄스·401.8g·30.4분
// 으로 나갔고, 11분 뒤 수온이 9.00℃ 로 복귀해 8회 스케줄이 되살아나면서 11:30·
// 12:30·13:30 이 추가로 나갔다 — 그날 727g, 하루 목표 680g 을 이미 초과.
//
// 그래서 축소 하한을 1회최대량(mealMaxG, 수처리 순간부하 기준)으로 잡는다.
// 회당 양이 상한을 넘지 않는 선까지만 줄인다. 결과적으로 횟수 축소는 1일량이
// 충분히 작아지는 "더 낮은 수온"에서만 실제로 2회까지 내려간다:
//
//	tank_01(65.1kg, 1회최대 163g) 기준
//	  8.85℃  1일 940g → 최소 6회 (회당 157g)   ← 종전엔 2회·470g 였다
//	  7.0℃   1일 228g → 2회      (회당 114g)
//	  6.0℃   1일 130g → 2회      (회당  65g)
//
// dailyFeedG/mealMaxG 가 0 이하이면(산출 불가) 종전처럼 fewCyclesCount 로 떨어뜨리지
// 않고 호출자가 넘긴 횟수를 그대로 둔다 — 모르는 상태에서 회당 양을 키우지 않는다.
func coldDailyCycles(cycles int, tempC float64, dailyFeedG, mealMaxG float64) int {
	if tempC >= fewCyclesBelowTempC || cycles <= fewCyclesCount {
		return cycles
	}
	if dailyFeedG <= 0 || mealMaxG <= 0 {
		return cycles
	}
	n := fewCyclesCount
	if need := int(math.Ceil(dailyFeedG / mealMaxG)); need > n {
		n = need
	}
	if n >= cycles {
		return cycles
	}
	return n
}

// rapidChangeFactorFor — 수온 급변 감산 계수. 방향을 가리지 않는다.
//
//	|24h 변화| ≥ 1℃          → 0.5 (급변 당일)
//	24h 안정 + |48h 변화| ≥ 1℃ → 0.7 (급변 다음 날 = 적응 기간)
//	그 외                     → 1.0
//
// 상승에도 감산하는 이유(2026-10-01 운영자 합의): 수온계는 올라가도 물고기 소화 능력은
// 바로 따라오지 않는다. 하강에는 식욕이 떨어져 안 먹고, 상승에는 소화를 못 한다.
// 양방향 모두 평소보다 적게 주는 쪽이 안전하다.
//
// 실측 근거: 9-30 하루 -1.8℃, 10-01 9시간 +4.4℃ — 취수 수온이 양방향으로 흔들린다.
func rapidChangeFactorFor(tempNow, temp24h, temp48h float64) float64 {
	if tempNow <= 0 {
		return 1.0
	}
	if temp24h > 0 && math.Abs(tempNow-temp24h) >= rapidChangeDeltaC {
		return rapidChangeFactor
	}
	if temp24h > 0 && temp48h > 0 && math.Abs(temp24h-temp48h) >= rapidChangeDeltaC {
		return adaptationFactor
	}
	return 1.0
}

// tempCAgo — hoursAgo 시간 전 수온(±1시간 창의 평균). 없으면 ok=false.
//
// ⚠ 반드시 수조로 거른다 — 수조별 센서가 분리되면 섞인 평균이 엉뚱한 급변을 만든다
// (2026-10-01 초기 구현의 결함. 지금은 센서 1개 fan-out 이라 증상이 없었다).
// 단일 관측이 아니라 창 평균을 쓰는 이유는 센서 노이즈 한 점으로 급이량이 절반이
// 되는 것을 막기 위해서다.
func (a *AIScheduler) tempCAgo(ctx context.Context, tankID string, hoursAgo int) (float64, bool) {
	now := time.Now().UTC()
	from := now.Add(-time.Duration(hoursAgo+1) * time.Hour)
	to := now.Add(-time.Duration(hoursAgo-1) * time.Hour)
	evs, err := a.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		Since:     &from,
		Until:     &to,
		Limit:     500,
	})
	if err != nil || len(evs) == 0 {
		return 0, false
	}
	var sum float64
	var n int
	for _, e := range evs {
		// ⚠ tank_id 는 location 안에 있다 — 최상위로 읽으면 항상 비어 필터가 전부 탈락한다.
		var p struct {
			Metric   string   `json:"metric"`
			Value    *float64 `json:"value"`
			Location struct {
				TankID string `json:"tank_id"`
			} `json:"location"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if p.Metric != events.MetricWaterTemperature || p.Value == nil || p.Location.TankID != tankID {
			continue
		}
		sum += *p.Value
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}
