package media

import "time"

const (
	DefaultReconnectBaseDelay = time.Second
	DefaultReconnectMaxDelay  = 30 * time.Second
)

type ReconnectPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

func (p ReconnectPolicy) Delay(attempt int) time.Duration {
	base := p.BaseDelay
	if base <= 0 {
		base = DefaultReconnectBaseDelay
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultReconnectMaxDelay
	}
	if attempt < 0 {
		attempt = 0
	}
	delay := base
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}
