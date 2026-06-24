package schedule

import (
	"fmt"
	"strconv"
	"strings"
)

// parseCronTimes extracts HH:MM fire times from a minimal cron expression.
//
// Supported: "M H * * *" and "M H D M W" where M and H can be:
//   - a single integer: "0 6 * * *"  → ["06:00"]
//   - a comma-separated list: "0 6,12,18 * * *" → ["06:00","12:00","18:00"]
//
// Day-of-month, month, and day-of-week fields are accepted but ignored.
// Returns an error if the expression cannot be parsed.
func parseCronTimes(expr string) ([]string, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("schedule: cron must have 5 fields, got %d in %q", len(fields), expr)
	}
	minuteField := fields[0]
	hourField := fields[1]

	minutes, err := parseIntList(minuteField)
	if err != nil {
		return nil, fmt.Errorf("schedule: cron minute field: %w", err)
	}
	hours, err := parseIntList(hourField)
	if err != nil {
		return nil, fmt.Errorf("schedule: cron hour field: %w", err)
	}

	for _, m := range minutes {
		if m < 0 || m > 59 {
			return nil, fmt.Errorf("schedule: cron minute %d out of range", m)
		}
	}
	for _, h := range hours {
		if h < 0 || h > 23 {
			return nil, fmt.Errorf("schedule: cron hour %d out of range", h)
		}
	}

	var times []string
	for _, h := range hours {
		for _, m := range minutes {
			times = append(times, fmt.Sprintf("%02d:%02d", h, m))
		}
	}
	return times, nil
}

func parseIntList(s string) ([]int, error) {
	if s == "*" {
		return nil, fmt.Errorf("wildcard not supported for minute/hour")
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		v, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("not an integer: %q", p)
		}
		out = append(out, v)
	}
	return out, nil
}
