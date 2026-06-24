// Package sensor holds the domain model for measurement-only devices
// (water-quality probes, GPS, feed-weight cells, …).
//
// Sensors are separated from actuators. A single physical node can host
// both kinds of capabilities (e.g., a marine combined WQ+GPS LattePanda),
// in which case it appears in both `sensors` and `actuators` with distinct
// device IDs.
//
// Phase 1 (skeleton): struct definitions only.
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §1.3.
package sensor
