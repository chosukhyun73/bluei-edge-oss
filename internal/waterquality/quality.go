package waterquality

import (
	"bluei.kr/edge/internal/events"
)

const (
	ReasonNormal       = "normal"
	ReasonInvalidZero  = "invalid_zero"
	ReasonInvalidRange = "invalid_range"
	ReasonNoValue      = "no_value"
	ReasonNotUpdated   = "not_updated"
)

type Classification struct {
	Quality string
	Reason  string
}

// ClassifyReading maps raw water-quality values into the existing Phase 1
// quality enum while preserving the more specific reason for audit/projection.
// Stale is intentionally not decided here; it depends on the latest normal
// reading state and must be computed by projection/rules, not one raw value.
func ClassifyReading(metric string, value *float64) Classification {
	if value == nil {
		return Classification{Quality: events.QualityMissing, Reason: ReasonNoValue}
	}

	v := *value
	if v == 0 && isZeroInvalidMetric(metric) {
		return Classification{Quality: events.QualitySuspect, Reason: ReasonInvalidZero}
	}
	if outsideInitialOperatingRange(metric, v) {
		return Classification{Quality: events.QualitySuspect, Reason: ReasonInvalidRange}
	}
	return Classification{Quality: events.QualityOK, Reason: ReasonNormal}
}

func isZeroInvalidMetric(metric string) bool {
	switch metric {
	case events.MetricWaterTemperature, events.MetricPH, events.MetricDissolvedOxygen:
		return true
	default:
		return false
	}
}

func outsideInitialOperatingRange(metric string, value float64) bool {
	switch metric {
	case events.MetricPH:
		return value < 4 || value > 10
	case events.MetricDissolvedOxygen:
		return value < 0 || value > 20
	case events.MetricWaterTemperature:
		return value < 0 || value > 35
	default:
		return false
	}
}
