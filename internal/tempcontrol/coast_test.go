package tempcontrol

import "testing"

// 코스팅 예측 조기 정지: 냉각 중 하강률로 lead분 뒤 착지 온도를 예측해 목표 도달 시 정지.
func TestAnticipatedCoastOff(t *testing.T) {
	const setpoint = 12.0
	const lead = 7.0
	cases := []struct {
		name string
		temp float64
		rate float64 // ℃/h
		want bool
	}{
		{"승온 중이면 정지 안 함", 12.4, +0.5, false},
		{"정지(rate 0)면 정지 안 함", 12.1, 0, false},
		{"약하게 냉각·아직 목표 위 → 계속", 12.30, -0.5, false},     // 12.30-0.058=12.24 > 12
		{"냉각 중 예상 착지 목표 이하 → 지금 정지", 12.05, -0.5, true}, // 12.05-0.058=11.99 ≤ 12
		{"강한 냉각이면 더 일찍 정지", 12.34, -3.0, true},          // 12.34-0.35=11.99 ≤ 12
		{"이미 목표 아래면 당연히 정지", 11.9, -0.2, true},
	}
	for _, c := range cases {
		if got := anticipatedCoastOff(c.temp, c.rate, lead, setpoint); got != c.want {
			t.Errorf("%s: anticipatedCoastOff(%.2f, %.2f) = %v, want %v", c.name, c.temp, c.rate, got, c.want)
		}
	}
}

func TestAnticipatedCoastOffLeadDisabled(t *testing.T) {
	if anticipatedCoastOff(12.05, -0.5, 0, 12.0) {
		t.Error("lead=0 이면 예측 정지 비활성이어야 함")
	}
}
