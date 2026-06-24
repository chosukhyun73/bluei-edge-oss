package control

import (
	"errors"
	"fmt"
)

// Feed command type constants.
const (
	CmdFeedDispense         = "feed.dispense"          // legacy — 변경 금지
	CmdFeedDispenseAdaptive = "feed.dispense_adaptive" // Phase 3
	CmdFeedDispenseFixed    = "feed.dispense_fixed"    // Phase 3
	CmdFeedStop             = "feed.stop"              // Phase 3
	CmdFeedSetSpeed         = "feed.set_speed"         // Phase 3
	CmdFeedSetAmount        = "feed.set_amount"        // Phase 3
)

// ValidateFeedCommand performs Phase 3 command-type–specific validation.
// Returns nil for legacy feed.dispense (unchanged) and unknown types (device-level gate handles those).
func ValidateFeedCommand(commandType string, params map[string]any) error {
	switch commandType {
	case CmdFeedDispenseAdaptive:
		v, ok := params["target_amount_g"]
		if !ok {
			return errors.New("feed.dispense_adaptive: target_amount_g is required")
		}
		f, ok := toFloat(v)
		if !ok || f <= 0 {
			return errors.New("feed.dispense_adaptive: target_amount_g must be > 0")
		}
	case CmdFeedDispenseFixed:
		if err := requirePositiveInt(params, "pulse_duration_ms", commandType); err != nil {
			return err
		}
		if err := requireNonNegativeInt(params, "gap_ms", commandType); err != nil {
			return err
		}
		if err := requirePositiveInt(params, "total_pulses", commandType); err != nil {
			return err
		}
	}
	return nil
}

func requirePositiveInt(params map[string]any, key, cmdType string) error {
	v, ok := params[key]
	if !ok {
		return fmt.Errorf("%s: %s is required", cmdType, key)
	}
	f, ok := toFloat(v)
	if !ok || f <= 0 {
		return fmt.Errorf("%s: %s must be > 0", cmdType, key)
	}
	return nil
}

func requireNonNegativeInt(params map[string]any, key, cmdType string) error {
	v, ok := params[key]
	if !ok {
		return fmt.Errorf("%s: %s is required", cmdType, key)
	}
	f, ok := toFloat(v)
	if !ok || f < 0 {
		return fmt.Errorf("%s: %s must be >= 0", cmdType, key)
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// RejectionError signals a policy/validation rejection.
type RejectionError struct {
	Code    string
	Message string
}

func (e *RejectionError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// ConflictError signals an idempotency key conflict with a different payload.
type ConflictError struct {
	ExistingCommandID string
}

func (e *ConflictError) Error() string {
	return "idempotency key already used by command " + e.ExistingCommandID
}

// CommandRequest carries a control command submitted via the local API.
type CommandRequest struct {
	IdempotencyKey string
	RequestedBy    map[string]any
	Target         map[string]any
	Command        map[string]any
	ExpiresInSec   int
	CorrelationID  string
}

// CommandResult is returned from a successful Submit.
type CommandResult struct {
	CommandID string
	Status    string
}
