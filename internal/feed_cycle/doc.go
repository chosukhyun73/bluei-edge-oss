// Package feed_cycle implements the adaptive / fixed feed cycle state machine.
//
// # State Machine
//
//	idle
//	  ↓ feed.dispense_adaptive / feed.dispense_fixed
//	pulse_active
//	  ↓ feed.dispense command queued to controller
//	  ↓ emit feed.pulse.started
//	pulse_complete
//	  ↓ timer elapsed (pulse_duration_ms) — Phase 4 will use controller ACK instead
//	  ↓ emit feed.pulse.completed { actual_duration_ms, estimated_amount_g }
//	gap_observation           ← key decision point
//	  ↓ wait gap_ms (from params or species default)
//	  ↓ emit feed.gap.started / feed.gap.completed
//	  ↓ termination check:
//	     target reached          → cycle_complete (reason: "satiation_or_target")
//	     max_pulses reached      → cycle_complete (reason: "max_pulses")
//	     max_duration reached    → cycle_complete (reason: "max_duration")
//	     safety_block signal     → cycle_complete (reason: "safety_block")
//	     operator_stop           → cycle_complete (reason: "operator_stop")
//	     else                    → pulse_active (next pulse)
//	cycle_complete
//	  ↓ emit feed.cycle.completed { pulses_executed, total_amount_g, termination_reason }
//
// # Phase 4 Hook Points
//
//   - SafetyGate interface: defaults to no-op; Phase 4 C-3p wires predictive block logic.
//   - AckSource interface: currently timer-based; Phase 4 uses real controller ACK.
//   - Vision termination: gap_observation currently uses count/duration only;
//     Phase 4/5 will plug in feed residue detection from vision pipeline.
package feed_cycle
