package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/runtime"
	"bluei.kr/edge/internal/storage"
)

func evaluateThreshold(ctx context.Context, app *runtime.App, store storage.Store, rule config.RuleEntry, events []*storage.Event) error {
	window := time.Duration(rule.Condition.DurationSec) * time.Second
	if window <= 0 {
		window = 1 * time.Second
	}
	cutoff := common.NowUTC().Add(-window)

	// Collect readings for this rule's metric/subject within the window
	var evidence []map[string]any
	for _, e := range events {
		if e.EventType != "sensor.reading.recorded" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		if payload["metric"] != rule.Metric {
			continue
		}
		quality, _ := payload["quality"].(string)
		if quality == "stale" || quality == "missing" || quality == "error" {
			continue
		}
		// Check if within window
		if e.RecordedAt.Before(cutoff) {
			continue
		}
		evidence = append(evidence, payload)
	}

	if len(evidence) == 0 {
		return nil
	}

	// Check if all readings in window violate threshold (continuous_latest mode)
	violated := true
	for _, ev := range evidence {
		val, ok := ev["value"].(float64)
		if !ok {
			continue
		}
		if !violates(val, rule.Condition.Operator, rule.Condition.Value) {
			violated = false
			break
		}
	}

	dedupeKey := fmt.Sprintf("%s|%s|%s|%s",
		rule.Alert.AlertType, rule.Subject.Kind, rule.Subject.ID, rule.RuleID)

	if violated {
		return raiseOrUpdateAlert(ctx, app, store, rule, dedupeKey, evidence)
	}
	// Clear if previously open
	existing, err := store.GetOpenAlert(ctx, dedupeKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return clearAlert(ctx, app, store, existing, evidence)
	}
	return nil
}

func violates(val float64, op string, threshold float64) bool {
	switch op {
	case "<":
		return val < threshold
	case "<=":
		return val <= threshold
	case ">":
		return val > threshold
	case ">=":
		return val >= threshold
	case "==":
		return val == threshold
	}
	return false
}

func raiseOrUpdateAlert(ctx context.Context, app *runtime.App, store storage.Store, rule config.RuleEntry, dedupeKey string, evidence []map[string]any) error {
	existing, err := store.GetOpenAlert(ctx, dedupeKey)
	if err != nil {
		return err
	}
	now := common.NowUTC()

	if existing == nil {
		alertID := common.NewAlertID()
		payload := events.AlertPayload{
			AlertID:   alertID,
			AlertType: rule.Alert.AlertType,
			Severity:  rule.Alert.Severity,
			Status:    events.AlertStatusOpen,
			Subject:   events.AlertSubject{Kind: rule.Subject.Kind, ID: rule.Subject.ID},
			RuleID:    rule.RuleID,
			Message:   rule.Alert.Message,
			Evidence: map[string]any{
				"readings": evidence,
			},
			RaisedAt:  common.FormatTime(now),
			UpdatedAt: common.FormatTime(now),
		}
		payloadJSON, _ := json.Marshal(payload)
		a := &storage.OpenAlert{
			AlertID:        alertID,
			AlertDedupeKey: dedupeKey,
			AlertType:      rule.Alert.AlertType,
			Severity:       rule.Alert.Severity,
			SubjectKind:    rule.Subject.Kind,
			SubjectID:      rule.Subject.ID,
			RuleID:         rule.RuleID,
			Status:         events.AlertStatusOpen,
			RaisedAt:       now,
			UpdatedAt:      now,
			PayloadJSON:    string(payloadJSON),
		}
		created, err := store.UpsertAlert(ctx, a)
		if err != nil {
			return err
		}
		eventType := events.EventAlertUpdated
		if created {
			eventType = events.EventAlertRaised
		}
		_, err = app.AppendEvent(ctx, "rules", "", "", eventType, alertID, payload)
		return err
	}

	existing.Severity = rule.Alert.Severity
	existing.Status = events.AlertStatusOpen
	existing.UpdatedAt = now
	payload := events.AlertPayload{
		AlertID:   existing.AlertID,
		AlertType: existing.AlertType,
		Severity:  existing.Severity,
		Status:    events.AlertStatusOpen,
		Subject:   events.AlertSubject{Kind: existing.SubjectKind, ID: existing.SubjectID},
		RuleID:    existing.RuleID,
		Message:   rule.Alert.Message,
		Evidence: map[string]any{
			"readings": evidence,
		},
		RaisedAt:  common.FormatTime(existing.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	payloadJSON, _ := json.Marshal(payload)
	existing.PayloadJSON = string(payloadJSON)
	if _, err := store.UpsertAlert(ctx, existing); err != nil {
		return err
	}
	_, err = app.AppendEvent(ctx, "rules", "", "", events.EventAlertUpdated, existing.AlertID, payload)
	return err
}

func clearAlert(ctx context.Context, app *runtime.App, store storage.Store, a *storage.OpenAlert, evidence []map[string]any) error {
	now := common.NowUTC()
	payload := events.AlertPayload{
		AlertID:   a.AlertID,
		AlertType: a.AlertType,
		Severity:  a.Severity,
		Status:    events.AlertStatusResolved,
		Subject:   events.AlertSubject{Kind: a.SubjectKind, ID: a.SubjectID},
		RuleID:    a.RuleID,
		Message:   "alert resolved",
		Evidence: map[string]any{
			"resolution_reason": "condition_recovered",
			"readings":          evidence,
		},
		RaisedAt:  common.FormatTime(a.RaisedAt),
		UpdatedAt: common.FormatTime(now),
	}
	_, err := app.AppendEvent(ctx, "rules", "", "", events.EventAlertUpdated, a.AlertID, payload)
	if err != nil {
		return err
	}
	return store.ClearAlert(ctx, a.AlertDedupeKey)
}
