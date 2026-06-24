package media

import (
	"testing"
	"time"
)

func TestReconnectPolicyBackoffCapsAtMax(t *testing.T) {
	p := ReconnectPolicy{BaseDelay: time.Second, MaxDelay: 10 * time.Second}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 10 * time.Second},
		{8, 10 * time.Second},
	}
	for _, tc := range cases {
		if got := p.Delay(tc.attempt); got != tc.want {
			t.Fatalf("attempt %d delay = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

func TestReconnectPolicyDefaults(t *testing.T) {
	p := ReconnectPolicy{}
	if got := p.Delay(0); got <= 0 {
		t.Fatalf("default delay = %s, want positive", got)
	}
	if got := p.Delay(20); got > DefaultReconnectMaxDelay {
		t.Fatalf("default delay = %s, exceeds max %s", got, DefaultReconnectMaxDelay)
	}
}
