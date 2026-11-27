package optimize

import "testing"

func TestRecommendTemp(t *testing.T) {
	p := DefaultTempParams() // optimal14, band8-15, DOfloor6.5, margin1.5, hyst0.3
	cases := []struct {
		name       string
		in         TempInputs
		wantAction string
		wantBind   string
	}{
		{"저온+DO여유→상승허용(이중이득)", TempInputs{CurrentTempC: 11, CurrentDO: 9.3, HasDO: true}, ActionAllowRise, "none"},
		{"고온→냉각", TempInputs{CurrentTempC: 15.5, CurrentDO: 8, HasDO: true}, ActionCool, "none"},
		{"목표근처→유지", TempInputs{CurrentTempC: 14.0, CurrentDO: 8, HasDO: true}, ActionHold, "none"},
		{"저온+DO빠듯→상승제한(유지/냉각)", TempInputs{CurrentTempC: 11, CurrentDO: 6.8, HasDO: true}, ActionHold, "do"},
	}
	for _, c := range cases {
		got := RecommendTemp(c.in, p)
		if got.Action != c.wantAction {
			t.Errorf("%s: action=%s want %s (setpoint %.1f, %s)", c.name, got.Action, c.wantAction, got.RecommendedSetpointC, got.Rationale)
		}
		if got.ConstraintBinding != c.wantBind {
			t.Errorf("%s: bind=%s want %s", c.name, got.ConstraintBinding, c.wantBind)
		}
		t.Logf("%s → %s setpoint=%.1f | %s", c.name, got.Action, got.RecommendedSetpointC, got.Rationale)
	}
}
