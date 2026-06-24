// Package arbiter resolves competing feed cycle requests from multiple sources
// (operator manual, operator schedule, AI advisory, AI autonomous) and ensures
// a single authoritative decision per tank per moment.
//
// Priority order (highest → lowest):
//
//	manual_override > ai_advisory > ai_autonomous
//
// Safety gates (C-3p / C-3l / C-3w) are applied inside feed_cycle.Worker,
// not here. The arbiter only concerns itself with source-priority conflicts.
package arbiter
