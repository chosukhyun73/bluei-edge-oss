package media

import (
	"errors"
	"time"
)

type RetentionPolicy struct {
	MaxBytes int64
	MaxAge   time.Duration
}

type MediaFile struct {
	Path      string
	SizeBytes int64
	ModTime   time.Time
}

func (p RetentionPolicy) Validate() error {
	if p.MaxBytes <= 0 && p.MaxAge <= 0 {
		return errors.New("at least one retention limit is required")
	}
	if p.MaxBytes < 0 {
		return errors.New("max_bytes cannot be negative")
	}
	if p.MaxAge < 0 {
		return errors.New("max_age cannot be negative")
	}
	return nil
}

func (p RetentionPolicy) ShouldDelete(file MediaFile, now time.Time, totalBytes int64) bool {
	if p.MaxAge > 0 && !file.ModTime.IsZero() && now.Sub(file.ModTime) > p.MaxAge {
		return true
	}
	if p.MaxBytes > 0 && totalBytes > p.MaxBytes {
		return true
	}
	return false
}
