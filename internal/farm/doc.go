// Package farm holds the domain model for the top of the
// 3-layer farm → site → tank hierarchy.
//
// A Farm represents the business / license holder (operator, certifications).
// It does not own equipment directly; it owns one or more sites.
//
// Phase 1 (skeleton): struct definitions only. Loader, validator, and storage
// upserts are deferred to Phase 2.
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §1 (3-layer domain).
package farm
