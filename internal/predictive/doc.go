// Package predictive implements Phase 4 D-6 feed-to-waste estimation,
// D-7 (light) WTG capacity headroom, and C-3p predictive safety gate.
//
// Design principles:
//   - Off-by-default. Config.PredictiveSafety.Enabled must be explicitly true.
//   - Conservative fail-open: if species profile or WTG capacity is missing,
//     the gate returns Allow to avoid blocking legitimate feeding operations.
//   - No LLM involvement. Realtime safety decisions belong to deterministic math.
//   - D-8 LSTM is out of scope for Phase 4 minimal.
//
// References: docs/39-multi-tank-feeder-system-design.md §4.
package predictive
