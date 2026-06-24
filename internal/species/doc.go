// Package species holds the domain model for per-species feeding pattern
// defaults and excretion (waste) model parameters.
//
// Source ordering (8-D Hybrid):
//  1. Defaults from app.bluei.kr (canonical).
//  2. Operator override (per-deployment).
//  3. AI calibration (online fine-tune against observed reality).
//
// Used by:
//   - feed_cycle state machine (pulse_duration_ms / gap_ms / total_pulses defaults).
//   - D-6 Feed-to-Waste Estimator (waste_model.* → predicted nutrient load).
//
// Phase 1 (skeleton): struct definitions only.
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §1.3, §4.1.
package species
