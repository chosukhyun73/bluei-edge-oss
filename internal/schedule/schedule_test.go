package schedule

import (
	"testing"
)

func TestParseCronTimes_single(t *testing.T) {
	times, err := parseCronTimes("0 6 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != 1 || times[0] != "06:00" {
		t.Fatalf("want [06:00], got %v", times)
	}
}

func TestParseCronTimes_multiHour(t *testing.T) {
	times, err := parseCronTimes("0 6,12,18 * * *")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"06:00", "12:00", "18:00"}
	if len(times) != len(want) {
		t.Fatalf("want %v, got %v", want, times)
	}
	for i, v := range want {
		if times[i] != v {
			t.Errorf("times[%d]: want %q got %q", i, v, times[i])
		}
	}
}

func TestParseCronTimes_withMinute(t *testing.T) {
	times, err := parseCronTimes("30 8,20 * * *")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"08:30", "20:30"}
	if len(times) != len(want) {
		t.Fatalf("want %v, got %v", want, times)
	}
	for i, v := range want {
		if times[i] != v {
			t.Errorf("times[%d]: want %q got %q", i, v, times[i])
		}
	}
}

func TestParseCronTimes_dayFieldsIgnored(t *testing.T) {
	// Day-of-month / month / day-of-week are accepted but ignored
	times, err := parseCronTimes("0 9 15 3 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != 1 || times[0] != "09:00" {
		t.Fatalf("want [09:00], got %v", times)
	}
}

func TestParseCronTimes_wrongFieldCount(t *testing.T) {
	_, err := parseCronTimes("0 6")
	if err == nil {
		t.Fatal("expected error for 2-field cron")
	}
}

func TestParseCronTimes_wildcardHour(t *testing.T) {
	_, err := parseCronTimes("0 * * * *")
	if err == nil {
		t.Fatal("expected error for wildcard hour")
	}
}

func TestParseCronTimes_outOfRange(t *testing.T) {
	_, err := parseCronTimes("60 6 * * *")
	if err == nil {
		t.Fatal("expected error for minute=60")
	}
}

// --- times list form ---

func TestEffectiveTimes_timesList(t *testing.T) {
	s := &scheduleForTest{cron: "", times: []string{"06:00", "12:00"}}
	got := effectiveFireTimesTest(s)
	want := []string{"06:00", "12:00"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestEffectiveTimes_cronOverridesTimes(t *testing.T) {
	s := &scheduleForTest{cron: "0 8 * * *", times: []string{"06:00"}}
	got := effectiveFireTimesTest(s)
	if len(got) != 1 || got[0] != "08:00" {
		t.Fatalf("want [08:00], got %v", got)
	}
}

// test helpers

type scheduleForTest struct {
	cron  string
	times []string
}

func effectiveFireTimesTest(s *scheduleForTest) []string {
	if s.cron != "" {
		times, err := parseCronTimes(s.cron)
		if err == nil {
			return times
		}
	}
	return s.times
}
