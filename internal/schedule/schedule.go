package schedule

import "time"

// Pattern describes one feed pulse train to dispense when the schedule fires.
type Pattern struct {
	PulseDurationMs int      `json:"pulse_duration_ms"`
	GapMs           int      `json:"gap_ms"`
	TotalPulses     int      `json:"total_pulses"`
	TargetAmountG   *float64 `json:"target_amount_g,omitempty"`
}

// Schedule is the in-memory representation of a feeding_schedules row.
type Schedule struct {
	ScheduleID string
	TankIDs    []string // decoded from tank_ids_json
	Cron       string   // optional cron expression
	Times      []string // optional HH:MM list
	Pattern    Pattern
	Priority   string // "manual_override" | "ai_advisory"
	SafetyGate bool
	Enabled    bool
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// FireTimes returns today's fire times as "HH:MM" strings.
// If Cron is non-empty, cron wins; else Times is used.
func (s *Schedule) FireTimes() []string {
	if s.Cron != "" {
		t, err := parseCronTimes(s.Cron)
		if err == nil {
			return t
		}
	}
	return s.Times
}
