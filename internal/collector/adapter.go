package collector

import "context"

// Adapter is the common interface for all field data collectors.
// Implementations translate device protocols into canonical edge events.
type Adapter interface {
	ID() string
	Type() string
	Run(ctx context.Context)
}
