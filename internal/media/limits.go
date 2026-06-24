package media

import "errors"

type StreamLimits struct {
	MaxWorkers          int
	MaxViewersPerWorker int
}

func (l StreamLimits) Validate() error {
	if l.MaxWorkers < 0 {
		return errors.New("max_workers cannot be negative")
	}
	if l.MaxViewersPerWorker < 0 {
		return errors.New("max_viewers_per_worker cannot be negative")
	}
	return nil
}

func (l StreamLimits) AllowWorker(currentWorkers int) bool {
	if l.MaxWorkers <= 0 {
		return true
	}
	return currentWorkers < l.MaxWorkers
}

func (l StreamLimits) AllowViewer(currentViewers int) bool {
	if l.MaxViewersPerWorker <= 0 {
		return true
	}
	return currentViewers < l.MaxViewersPerWorker
}
