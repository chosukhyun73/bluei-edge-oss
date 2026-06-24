package inference_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bluei.kr/edge/internal/inference"
)

func TestOllamaAnalyzerReturnsAdvisoryAnalysis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "gemma4:26b" {
			t.Fatalf("unexpected model: %v", req["model"])
		}
		if req["stream"] != false {
			t.Fatalf("stream must be false for deterministic edge parsing")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"response": `{"observation":"DO is below target","confidence":0.82,"reasons":["latest dissolved oxygen is 4.8 mg/L"],"recommended_action":"check aeration","safety_flags":["operator_review_required"],"evidence_refs":["tank:tank-a","reading:do"]}`,
		})
	}))
	defer server.Close()

	analyzer := inference.NewOllamaAnalyzer(inference.OllamaConfig{
		Endpoint: server.URL,
		Model:    "gemma4:26b",
	})

	result, err := analyzer.Analyze(context.Background(), inference.Request{
		TankID: "tank-a",
		Kind:   inference.KindSensorAnomaly,
		Context: map[string]any{
			"dissolved_oxygen_mg_l": 4.8,
		},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if result.Observation != "DO is below target" {
		t.Fatalf("unexpected observation: %q", result.Observation)
	}
	if result.Confidence != 0.82 {
		t.Fatalf("unexpected confidence: %v", result.Confidence)
	}
	if result.ControlAllowed {
		t.Fatal("AI inference result must not directly allow control")
	}
	if len(result.SafetyFlags) != 1 || result.SafetyFlags[0] != "operator_review_required" {
		t.Fatalf("unexpected safety flags: %#v", result.SafetyFlags)
	}
}

func TestOllamaAnalyzerRejectsMissingTankID(t *testing.T) {
	analyzer := inference.NewOllamaAnalyzer(inference.OllamaConfig{
		Endpoint: "http://127.0.0.1:11435",
		Model:    "gemma4:26b",
	})

	_, err := analyzer.Analyze(context.Background(), inference.Request{Kind: inference.KindSensorAnomaly})
	if err == nil {
		t.Fatal("expected missing tank_id to fail")
	}
}
