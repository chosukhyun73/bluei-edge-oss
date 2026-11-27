package haccp

import "testing"

func fp(v float64) *float64 { return &v }

const minute = int64(60000)

func TestEvaluateLimit(t *testing.T) {
	instant := Limit{Item: "temperature", RuleKind: RuleInstant, Max: fp(4.4), Unit: "℃", SourceKind: SourceSensor}
	cases := []struct {
		name string
		in   LimitInput
		want string
	}{
		{"범위에 들어오기 전", LimitInput{Limit: instant, Now: 100}, StatusPending},
		{"한계기준 안", LimitInput{Limit: instant, Windows: []Window{{Start: 0}}, Now: 20000,
			Samples: []Sample{{5000, 2.1}, {15000, 3.0}}}, StatusOK},
		{"순간값 이탈", LimitInput{Limit: instant, Windows: []Window{{Start: 0}}, Now: 20000,
			Samples: []Sample{{5000, 2.1}, {15000, 5.2}}}, StatusDeviation},
		{"범위 밖 시각의 값은 보지 않는다", LimitInput{Limit: instant, Windows: []Window{{Start: 10000, End: 20000}}, Now: 90000,
			Samples: []Sample{{5000, 9.9}, {15000, 3.0}, {30000, 9.9}}}, StatusOK},
		{"센서 끊김", LimitInput{Limit: instant, Windows: []Window{{Start: 0}}, Now: 5 * minute, OutageAfterSec: 60,
			Samples: []Sample{{10000, 2.0}}}, StatusOutage},
		{"끊김 판정을 끄면 정상", LimitInput{Limit: instant, Windows: []Window{{Start: 0}}, Now: 5 * minute,
			Samples: []Sample{{10000, 2.0}}}, StatusOK},
		{"사람 입력이 없음", LimitInput{Limit: Limit{Item: "core_temperature", RuleKind: RuleInstant, Max: fp(4.4), SourceKind: SourceOperator},
			Windows: []Window{{Start: 0}}, Now: 5 * minute, OutageAfterSec: 60}, StatusNeedsInput},
		{"대기 시간 초과", LimitInput{Limit: Limit{Item: "wait_time", RuleKind: RuleInstant, Max: fp(30), Unit: "min", SourceKind: SourceDerived},
			Windows: []Window{{Start: 0}}, Now: 40 * minute, OutageAfterSec: 60,
			Samples: []Sample{{40 * minute, 40}}}, StatusDeviation},
		{"금속 검출 0건", LimitInput{Limit: Limit{Item: "metal", RuleKind: RuleCount, Max: fp(0), SourceKind: SourceSensor},
			Windows: []Window{{Start: 0}}, Now: 30000, Samples: []Sample{{10000, 0}, {20000, 0}}}, StatusOK},
		{"금속 검출 1건", LimitInput{Limit: Limit{Item: "metal", RuleKind: RuleCount, Max: fp(0), SourceKind: SourceSensor},
			Windows: []Window{{Start: 0}}, Now: 30000, Samples: []Sample{{10000, 0}, {20000, 1}}}, StatusDeviation},
		{"주기 점검 누락", LimitInput{Limit: Limit{Item: "test_piece", RuleKind: RulePeriodicCheck, Params: map[string]float64{"interval_min": 120}},
			Windows: []Window{{Start: 0}}, Now: 130 * minute}, StatusDeviation},
		{"주기 점검 지킴", LimitInput{Limit: Limit{Item: "test_piece", RuleKind: RulePeriodicCheck, Params: map[string]float64{"interval_min": 120}},
			Windows: []Window{{Start: 0}}, Now: 130 * minute, Samples: []Sample{{60 * minute, 1}}}, StatusOK},
		{"유지 시간 채우는 중", LimitInput{Limit: Limit{Item: "core", RuleKind: RuleDuration, Min: fp(63), Params: map[string]float64{"min_duration_sec": 60}, SourceKind: SourceSensor},
			Windows: []Window{{Start: 0}}, Now: 30000, Samples: []Sample{{10000, 64}, {30000, 65}}}, StatusInProgress},
		{"유지 시간 못 채우고 끝남", LimitInput{Limit: Limit{Item: "core", RuleKind: RuleDuration, Min: fp(63), Params: map[string]float64{"min_duration_sec": 60}, SourceKind: SourceSensor},
			Windows: []Window{{Start: 0, End: 40000}}, Now: 90000, Samples: []Sample{{10000, 64}, {30000, 65}}}, StatusDeviation},
		{"유지 시간 채움", LimitInput{Limit: Limit{Item: "core", RuleKind: RuleDuration, Min: fp(63), Params: map[string]float64{"min_duration_sec": 60}, SourceKind: SourceSensor},
			Windows: []Window{{Start: 0, End: 100000}}, Now: 200000, Samples: []Sample{{10000, 64}, {40000, 65}, {80000, 66}}}, StatusOK},
	}
	for _, c := range cases {
		if got := EvaluateLimit(c.in); got.Status != c.want {
			t.Errorf("%s: status = %s (%s), want %s", c.name, got.Status, got.Detail, c.want)
		}
	}
}

func TestCumulativeExposure(t *testing.T) {
	l := Limit{Item: "exposure_time", RuleKind: RuleCumulativeExposure, SourceKind: SourceDerived,
		Params: map[string]float64{"threshold": 4.4, "max_minutes": 480, "high_threshold": 21.1, "max_minutes_high": 240}}
	// 10℃ 로 300분: 한도(480분) 안
	r := EvaluateLimit(LimitInput{Limit: l, Windows: []Window{{Start: 0}}, Now: 300 * minute,
		Samples: []Sample{{0, 10}, {150 * minute, 10}}})
	if r.Status != StatusOK || r.ExposureMin < 299 || r.ExposureMin > 301 {
		t.Fatalf("10℃ 300분: %+v", r)
	}
	// 같은 300분인데 22℃ 가 한 번 있었으면 짧은 한도(240분)가 적용돼 이탈
	r = EvaluateLimit(LimitInput{Limit: l, Windows: []Window{{Start: 0}}, Now: 300 * minute,
		Samples: []Sample{{0, 10}, {100 * minute, 22}, {110 * minute, 10}}})
	if r.Status != StatusDeviation {
		t.Fatalf("고온 구간이 있으면 240분 한도: %+v", r)
	}
	// 기준 온도 이하로 보낸 시간은 더하지 않는다. 범위를 벗어난 사이(냉장 복귀)도 더하지 않는다
	r = EvaluateLimit(LimitInput{Limit: l, Windows: []Window{{Start: 0, End: 100 * minute}, {Start: 400 * minute}}, Now: 500 * minute,
		Samples: []Sample{{0, 10}, {50 * minute, 3}, {400 * minute, 10}}})
	if r.Status != StatusOK || r.ExposureMin < 149 || r.ExposureMin > 151 {
		t.Fatalf("50분 + 100분 = 150분이어야 함: %+v", r)
	}
}

// 단계를 측정 주기보다 빨리 지나가도 진입 직전의 센서 값으로 판정한다. 값이 전혀 없으면 적합이 아니다.
func TestCarryForwardAndNoData(t *testing.T) {
	instant := Limit{Item: "temperature", RuleKind: RuleInstant, Max: fp(4.4), SourceKind: SourceSensor}
	// 3초 머묾, 그 사이 측정 없음. 진입 2초 전 값 3.0 → 적합
	r := EvaluateLimit(LimitInput{Limit: instant, Windows: []Window{{Start: 10000, End: 13000}}, Now: 60000,
		Samples: []Sample{{8000, 3.0}}, CarryMs: 60000})
	if r.Status != StatusOK || r.Count != 1 {
		t.Fatalf("진입 직전 값으로 적합이어야 함: %+v", r)
	}
	// 진입 직전 값이 한계를 넘었으면 이탈
	r = EvaluateLimit(LimitInput{Limit: instant, Windows: []Window{{Start: 10000, End: 13000}}, Now: 60000,
		Samples: []Sample{{8000, 9.0}}, CarryMs: 60000})
	if r.Status != StatusDeviation {
		t.Fatalf("진입 직전 값이 이탈이면 이탈: %+v", r)
	}
	// 너무 오래된 값은 쓰지 않는다 → 값 없이 지나감 = 모니터링 불능
	r = EvaluateLimit(LimitInput{Limit: instant, Windows: []Window{{Start: 500000, End: 503000}}, Now: 600000,
		Samples: []Sample{{8000, 3.0}}, CarryMs: 60000})
	if r.Status != StatusOutage {
		t.Fatalf("값 없이 지나가면 모니터링 불능: %+v", r)
	}
	// 누적 노출도 진입 시점부터 센다
	exp := Limit{Item: "exposure_time", RuleKind: RuleCumulativeExposure, SourceKind: SourceDerived,
		Params: map[string]float64{"threshold": 4.4, "max_minutes": 480}}
	r = EvaluateLimit(LimitInput{Limit: exp, Windows: []Window{{Start: 0, End: 30 * minute}}, Now: 60 * minute,
		Samples: []Sample{{-5000, 12}}, CarryMs: 60000})
	if r.ExposureMin < 29.9 || r.ExposureMin > 30.1 {
		t.Fatalf("12℃ 로 30분 = 누적 30분: %+v", r)
	}
}
