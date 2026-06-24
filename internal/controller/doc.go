// Package controller holds the domain model for physical microcontrollers
// (ESP32 today, possibly others later) that drive actuators.
//
// Lifecycle: pending → active → disabled / fault.
// Two-step registration (2-C): the controller POSTs /v1/controllers/register
// on boot (→ pending), then an operator calls /v1/controllers/{id}/activate
// after a commissioning test.
//
// Mapping is 1 controller : 1 actuator for Phase 1.
//
// Phase 1 (skeleton): struct definitions only. The registration API,
// commissioning tests, and the provisioning CLI are Phase 2 work.
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §2.
package controller
