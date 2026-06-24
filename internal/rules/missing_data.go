package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

func evaluateMissingData(ctx context.Context, app *runtime.App, store storage.Store, rule config.RuleEntry) error {
	maxAge := time.Duration(rule.MaxAgeSec) * time.Second
	if maxAge <= 0 {
		maxAge = 300 * time.Second
	}
	cutoff := common.NowUTC().Add(-maxAge)

	// Find the most recent reading for this metric.
	events, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: "sensor.reading.recorded",
		Limit:     1000,
	})
	if err != nil {
		return err
	}

	var latestForMetric *storage.Event
	var latestPayload map[string]any
	for _, e := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload["metric"] == rule.Metric {
			latestForMetric = e
			latestPayload = payload
			break
		}
	}

	dedupeKey := fmt.Sprintf("%s|sensor|%s|%s",
		rule.Alert.AlertType, rule.Metric, rule.RuleID)

	isMissing := latestForMetric == nil || latestForMetric.RecordedAt.Before(cutoff)
	if isMissing {
		if rule.Subject.Kind == "" {
			rule.Subject.Kind = "sensor"
		}
		if rule.Subject.ID == "" {
			rule.Subject.ID = rule.Metric
		}
		evidence := []map[string]any{{
			"metric":         rule.Metric,
			"max_age_sec":    rule.MaxAgeSec,
			"latest_reading": latestPayload,
		}}
		return raiseOrUpdateAlert(ctx, app, store, rule, dedupeKey, evidence)
	}

	// Clear if previously open.
	existing, err := store.GetOpenAlert(ctx, dedupeKey)
	if err != nil {
		return err
	}
	if existing != nil {
		evidence := []map[string]any{latestPayload}
		return clearAlert(ctx, app, store, existing, evidence)
	}
	return nil
}
