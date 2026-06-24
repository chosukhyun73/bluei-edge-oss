// Package site holds the domain model for a physical operating location.
//
// Sites come in two flavors:
//   - land: RAS facilities, addressed, fixed coordinates, owns water treatment groups.
//   - marine: cages, identified by GPS points, mutable lifecycle (그물 교체).
//
// Phase 1 (skeleton): struct definitions only.
// In SQLite, both flavors live in a single `sites` table with a `site_type` discriminator.
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §1.3 (sites_land / sites_marine).
package site
