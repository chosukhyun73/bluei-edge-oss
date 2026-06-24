// Package actuator holds the domain model for control devices (feeders, pumps,
// heat pumps, UV sterilizers, biofilters, oxygen suppliers, …).
//
// Actuators are separated from sensors so that control responsibility and
// observability responsibility have distinct surfaces. An actuator may or may
// not be backed by a physical controller (ESP32) — software-driven actuators
// have controller_id = "".
//
// Phase 1 (skeleton): struct definitions only.
// Phase 2 will migrate entries from the legacy `devices` model (001 / configs/devices.yaml)
// into this package and into the `actuators` SQLite table (012).
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §1.3 (D-10c separation).
package actuator
