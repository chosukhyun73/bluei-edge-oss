package baseline

import "fmt"

// CommandPlan — DecisionData 에서 추출한 control 명령 구체화.
// SupportedKind=false 면 control 발행 X (audit only).
type CommandPlan struct {
	SupportedKind bool
	DeviceID      string         // ESP32 device_id (feeder)
	AdapterID     string         // Submit 이 필요 시 lookup — 여기서는 비워둠
	CommandType   string         // 예: "dispense_feed"
	CommandBody   map[string]any // ESP32 가 받을 명령 본문
	Reason        string         // 미지원 사유 (SupportedKind=false 일 때)
}

// PlanCommand — DecisionKind + DecisionData → CommandPlan.
// C-3 narrow scope: feeding 만 지원. 다른 kind 는 SupportedKind=false.
func PlanCommand(decisionKind string, data map[string]any) CommandPlan {
	switch decisionKind {
	case "feeding":
		return planFeedingCommand(data)
	default:
		return CommandPlan{
			SupportedKind: false,
			Reason:        fmt.Sprintf("control wiring pending for kind=%s", decisionKind),
		}
	}
}

// planFeedingCommand — feeding kind 의 명령 구체화.
// feeder_id + feed_amount_g 필수.
func planFeedingCommand(data map[string]any) CommandPlan {
	if data == nil {
		return CommandPlan{SupportedKind: false, Reason: "missing_field: decision_data is nil"}
	}

	feederID, ok := data["feeder_id"].(string)
	if !ok || feederID == "" {
		return CommandPlan{SupportedKind: false, Reason: "missing_field: feeder_id"}
	}

	// feed_amount_g 는 JSON 숫자 → float64
	var amountG float64
	switch v := data["feed_amount_g"].(type) {
	case float64:
		amountG = v
	case int:
		amountG = float64(v)
	case int64:
		amountG = float64(v)
	default:
		return CommandPlan{SupportedKind: false, Reason: "missing_field: feed_amount_g"}
	}

	body := map[string]any{
		"type":          "dispense_feed",
		"feed_amount_g": amountG,
	}

	return CommandPlan{
		SupportedKind: true,
		DeviceID:      feederID,
		AdapterID:     "",
		CommandType:   "dispense_feed",
		CommandBody:   body,
	}
}
