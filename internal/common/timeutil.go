package common

import "time"

func NowUTC() time.Time { return time.Now().UTC() }

func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func ParseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}
