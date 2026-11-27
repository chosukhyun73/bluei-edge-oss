package feed_cycle

// 사이클 피크 기반 만복 게이트 (2026-07-20 운영자 결정).
//
// 종전 게이트(compensationSatiated)는 gap 끝 last5/기저 비율(compensationSatiationFactor
// =1.2)로 만복을 판정했으나, 저진폭 카메라(tank_02)에서는 배고픔 사이클도 gap 끝엔
// 기저 근처로 잦아들어(7-20 실측: 낮 활발 0.954 vs 저녁 저활동 0.925 — 판별 불가)
// 보상을 구조적으로 상시 폐기했다. 사이클 요동 피크는 두 상태를 잘 가른다(같은 날
// 낮 활발 0.36~1.00 vs 저녁 저활동 0.22, 카메라 P90 정규화). 그래서 게이트를 사이클
// 피크로 교체한다 — "이번 사이클이 이 카메라 기준으로 활발했나".

import (
	"context"
	"strings"

	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

const (
	// riseP90Window — 카메라 P90 산정용 최근 요동 이벤트 스캔 상한.
	riseP90Window = 500
	// riseP90MinSample — 카메라당 이 미만이면 P90 미확보 → legacy 게이트 폴백.
	riseP90MinSample = 10
)

// tankTopviewRiseP90 — tank 의 탑뷰 카메라 요동 상승률 P90 (자기 진폭 기준).
// api.riseP90ByCamera 와 동일 계열 — 여기선 tank_id 로 필터하고 수중캠은 제외한다
// (수면 요동은 탑뷰가 정본). 표본 부족/산정 불가면 (0, false).
func (w *Worker) tankTopviewRiseP90(tankID string) (float64, bool) {
	if w.store == nil {
		return 0, false
	}
	evs, err := w.store.QueryEvents(context.Background(), storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded,
		Limit:     riseP90Window,
	})
	if err != nil {
		return 0, false
	}
	var rises []float64
	for _, e := range evs {
		p, perr := vision_pipeline.ParseAgitationPayload(e.PayloadJSON)
		if perr != nil || p.TankID != tankID || p.Baseline <= 0 {
			continue
		}
		if isUnderwaterCamera(p.CameraID) {
			continue
		}
		rises = append(rises, vision_pipeline.PeakRiseFrac(p.Baseline, p.Peak))
	}
	p90, ok := percentile90(rises)
	if !ok {
		return 0, false
	}
	// 증폭 상한 — P90 에 하한을 두어 과증폭을 막는다 (2026-07-31 운영자).
	//
	// 정규화 목적은 "카메라 간 진폭 차이 보정"이지 "활동이 줄면 더 키우기"가 아니다.
	// 그런데 P90 이 최근 7일 자기 데이터라, 활동이 낮아지면 P90 도 같이 낮아져
	// 정규화값이 오히려 올라가는 되먹임이 생긴다. 실측: tank_02 P90 이 7-28 0.885 →
	// 7-30 0.454 로 45% 하락해 증폭이 1.13배 → 2.20배로 뛰었다(tank_01 은 1.2배 안정).
	// 만복 게이트는 이 정규화 피크를 임계 0.30 과 비교하므로, 과증폭되면 저활동
	// 사이클도 "활발"로 읽혀 게이트가 무력화된다.
	//
	// 하한 0.70 = 정상기(7-26~28) 두 카메라 P90 0.78~0.89 에 근접한 값. tank_01 은
	// 현재 0.787 이라 영향 없고, 무너진 tank_02 만 1.43배로 되돌린다.
	if floor := w.cfg.SatiationRiseP90Floor; floor > 0 && p90 < floor {
		return floor, true
	}
	return p90, true
}

// percentile90 — 표본≥riseP90MinSample 이고 값이 유의미하면 (P90, true).
func percentile90(rises []float64) (float64, bool) {
	if len(rises) < riseP90MinSample {
		return 0, false
	}
	sorted := append([]float64(nil), rises...)
	insertionSortFloats(sorted)
	v := sorted[int(float64(len(sorted))*0.9)]
	if v < 1e-6 {
		return 0, false
	}
	return v, true
}

func insertionSortFloats(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func isUnderwaterCamera(camID string) bool {
	return strings.Contains(camID, "underwater")
}
