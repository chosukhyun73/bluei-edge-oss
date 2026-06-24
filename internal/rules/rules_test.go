package rules

import (
	"testing"
)

func TestViolates(t *testing.T) {
	cases := []struct {
		val, threshold float64
		op             string
		want           bool
	}{
		{4.0, 5.0, "<", true},
		{6.0, 5.0, "<", false},
		{5.0, 5.0, "<=", true},
		{5.1, 5.0, "<=", false},
		{6.0, 5.0, ">", true},
		{4.0, 5.0, ">", false},
		{5.0, 5.0, ">=", true},
		{4.9, 5.0, ">=", false},
		{5.0, 5.0, "==", true},
		{5.1, 5.0, "==", false},
		{5.0, 5.0, "unknown_op", false},
	}
	for _, c := range cases {
		got := violates(c.val, c.op, c.threshold)
		if got != c.want {
			t.Errorf("violates(%v, %q, %v) = %v, want %v", c.val, c.op, c.threshold, got, c.want)
		}
	}
}
