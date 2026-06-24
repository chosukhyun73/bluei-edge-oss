// Package schedule provides operator-defined feeding schedules for the
// multi-tank edge runtime.
//
// Schedules are stored in the feeding_schedules SQLite table (migration 014).
// Each schedule targets one or more Cage/Tank IDs and defines when to fire
// via either a times list ([]"HH:MM") or a minimal cron expression.
//
// # Time forms
//
//   - times_json: ["06:00","12:00","18:00"] — fires at the listed HH:MM each day.
//   - cron: "M H * * *" or "M H D M W" — only minute+hour fields are parsed;
//     day-of-month/month/day-of-week are accepted but ignored in this
//     minimal implementation.
//
// If cron is non-empty it takes precedence; otherwise times_json is used.
//
// # Worker
//
// Worker polls every 30 s (configurable) and compares current wall-clock HH:MM
// against each enabled schedule's next-fire list. Deduplication is in-memory
// (schedule_id + YYYY-MM-DDTHH:MM), so a restart within the same minute is safe.
//
// # Safety
//
// The schedule worker triggers feed cycles via the existing feed_cycle.Worker,
// which already applies the C-3/C-3p/C-3l/C-3w safety gates.
// The schedule layer itself performs no safety decisions.
package schedule
