package feed_cycle

import (
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

func TestSafetyBlockStreak(t *testing.T) {
	at := time.Now().UTC()
	c := func(reason string, completed bool) *storage.FeedCycle {
		fc := &storage.FeedCycle{TerminationReason: reason}
		if completed {
			fc.CompletedAt = &at
		}
		return fc
	}
	cases := []struct {
		name string
		rows []*storage.FeedCycle // 최신순
		want int
	}{
		{"2연속 차단", []*storage.FeedCycle{c("safety_block", true), c("safety_block", true), c("max_pulses", true)}, 2},
		{"직전만 차단", []*storage.FeedCycle{c("safety_block", true), c("satiation_or_target", true)}, 1},
		{"정상 사이클이 streak 리셋", []*storage.FeedCycle{c("safety_block", true), c("max_pulses", true), c("safety_block", true)}, 1},
		{"진행 중 행은 건너뛰고 집계", []*storage.FeedCycle{c("", false), c("safety_block", true), c("safety_block", true)}, 2},
		{"차단 없음", []*storage.FeedCycle{c("max_pulses", true)}, 0},
		{"빈 목록", nil, 0},
	}
	for _, tc := range cases {
		if got := safetyBlockStreak(tc.rows); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
