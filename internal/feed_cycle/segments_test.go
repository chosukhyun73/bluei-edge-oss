package feed_cycle

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

// 실측(2026-07-05 15:37 tank_01) 펄스 리듬으로 세그먼트 유도 검증.
func TestDeriveSegments_RealCycleRhythm(t *testing.T) {
	pulses := []PulseTiming{
		{PulseNo: 1, CompletedAt: mustTime(t, "2026-07-05T06:37:38.695Z"), ActualDurationMs: 14520},
		{PulseNo: 2, CompletedAt: mustTime(t, "2026-07-05T06:38:08.632Z"), ActualDurationMs: 14935},
		{PulseNo: 3, CompletedAt: mustTime(t, "2026-07-05T06:38:38.361Z"), ActualDurationMs: 14727},
	}
	segs := DeriveSegments(pulses, 29000)
	if len(segs) != 3 {
		t.Fatalf("segments = %d, want 3", len(segs))
	}

	// 세그먼트 0 시작 = 펄스1 ON 시작 = 06:37:38.695 - 14.520s = 06:37:24.175.
	wantStart0 := mustTime(t, "2026-07-05T06:37:24.175Z")
	if !segs[0].StartAbs.Equal(wantStart0) {
		t.Errorf("seg0 start = %v, want %v", segs[0].StartAbs, wantStart0)
	}
	// 세그먼트 0 끝 = 펄스2 ON 시작 = 06:38:08.632 - 14.935s = 06:37:53.697.
	wantEnd0 := mustTime(t, "2026-07-05T06:37:53.697Z")
	if !segs[0].EndAbs.Equal(wantEnd0) {
		t.Errorf("seg0 end = %v, want %v", segs[0].EndAbs, wantEnd0)
	}
	// 세그먼트 길이 ≈ 29.5s (사용자 규격 29s 근처).
	if d := segs[0].DurationMs; d < 28000 || d > 31000 {
		t.Errorf("seg0 duration = %dms, want ~29500", d)
	}
	// 마지막 세그먼트는 측정 간격 중앙값으로 연장 → 다른 세그먼트와 유사(~29.5s).
	if d := segs[2].DurationMs; d < 28000 || d > 31000 {
		t.Errorf("last seg duration = %dms, want ~29500 (median gap)", d)
	}
}

func TestDeriveSegments_Empty(t *testing.T) {
	if segs := DeriveSegments(nil, 29000); segs != nil {
		t.Errorf("empty input = %v, want nil", segs)
	}
}

func TestDeriveSegments_SinglePulseUsesFallback(t *testing.T) {
	pulses := []PulseTiming{
		{PulseNo: 1, CompletedAt: mustTime(t, "2026-07-05T06:37:38.695Z"), ActualDurationMs: 14520},
	}
	segs := DeriveSegments(pulses, 29000)
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1", len(segs))
	}
	if segs[0].DurationMs != 29000 {
		t.Errorf("single-pulse duration = %d, want 29000 (fallback)", segs[0].DurationMs)
	}
}

// 정렬되지 않은 입력도 pulse_no 순으로 산출.
func TestDeriveSegments_Unsorted(t *testing.T) {
	pulses := []PulseTiming{
		{PulseNo: 2, CompletedAt: mustTime(t, "2026-07-05T06:38:08.632Z"), ActualDurationMs: 14935},
		{PulseNo: 1, CompletedAt: mustTime(t, "2026-07-05T06:37:38.695Z"), ActualDurationMs: 14520},
	}
	segs := DeriveSegments(pulses, 29000)
	if segs[0].PulseNo != 1 || segs[1].PulseNo != 2 {
		t.Errorf("segments not sorted by pulse_no: %d,%d", segs[0].PulseNo, segs[1].PulseNo)
	}
}
