// Package wtg holds the domain model for Water Treatment Groups.
//
// A WTG groups tanks that share heat pump / UV / biofilter / circulation pump
// capacity. Primarily used for land RAS where multiple tanks are physically
// plumbed through one treatment loop.
//
// WTGs are inputs to:
//   - D-7 (capacity model) — current processing capacity vs feeding load.
//   - C-3p (predictive water-quality gate) — pause/block new feed cycles when
//     the WTG is forecasted to breach a safety threshold.
//
// Phase 1 (skeleton): struct definitions only.
//
// References:
//   - docs/39-multi-tank-feeder-system-design.md §1.3, §4.2.
package wtg
