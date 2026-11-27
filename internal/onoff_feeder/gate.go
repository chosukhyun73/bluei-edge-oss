// Package onoff_feeder holds timing constants for on/off-type external feeders.
// The device integration itself is not part of the public build.
package onoff_feeder

import "time"

// DefaultMinInterval is the minimum spacing between outbound device calls.
const DefaultMinInterval = 1200 * time.Millisecond
