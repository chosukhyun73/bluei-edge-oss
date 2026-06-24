package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractJSONBlock(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", `{"a":1}`, `{"a":1}`},
		{"with prose", `여기 결과입니다: {"a":1} 끝.`, `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"plain fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"multi line", "응답:\n```json\n{\n  \"a\": 1\n}\n```", "{\n  \"a\": 1\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONBlock(tt.in)
			if got != tt.want {
				t.Errorf("extractJSONBlock(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateAndEnforce_SafetyViolation(t *testing.T) {
	a := &Analysis{
		CanApply: true,
		Adjustment: map[string]any{
			"min_interval_min": float64(15), // < 30 violates
		},
	}
	violations := ValidateAndEnforce(a)
	if len(violations) == 0 {
		t.Fatal("expected violation for min_interval_min < 30")
	}
	if a.CanApply {
		t.Error("expected CanApply forced to false")
	}
	if len(a.BlockedBy) == 0 {
		t.Error("expected BlockedBy populated")
	}
}

func TestValidateAndEnforce_AllValid(t *testing.T) {
	a := &Analysis{
		CanApply: true,
		Adjustment: map[string]any{
			"min_interval_min":          float64(45),
			"max_daily_cycles_override": float64(5),
			"get_factor":                float64(0.9),
			"bsf_mode_override":         "aggressive",
		},
	}
	violations := ValidateAndEnforce(a)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %v", violations)
	}
	if !a.CanApply {
		t.Error("expected CanApply preserved")
	}
}

func TestValidateAndEnforce_BadBsfMode(t *testing.T) {
	a := &Analysis{
		CanApply: true,
		Adjustment: map[string]any{
			"bsf_mode_override": "yolo",
		},
	}
	violations := ValidateAndEnforce(a)
	if len(violations) == 0 {
		t.Fatal("expected violation for invalid bsf_mode_override")
	}
	if a.CanApply {
		t.Error("expected CanApply forced to false")
	}
}

// TestClient_AnalyzeRoundTrip — fake ollama 서버로 happy path.
func TestClient_AnalyzeRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer devtoken" {
			t.Errorf("missing/wrong auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant",
					"content": "```json\n{\"can_apply\":true,\"reason\":\"ok\",\"scope\":\"GET-20%\",\"blocked_by\":[],\"adjustment\":{\"get_factor\":0.8},\"explanation_ko\":\"적용 가능\",\"confidence\":0.8}\n```"}},
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	c := NewClient(Config{
		Endpoint:  srv.URL,
		AuthToken: "devtoken",
		Primary:   "test-primary",
		Fallback:  "test-fallback",
		Timeout:   2 * time.Second,
	})
	a, err := c.Analyze(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !a.CanApply || a.Confidence < 0.7 || a.ModelUsed != "test-primary" || a.Fallback {
		t.Errorf("unexpected analysis: %+v", a)
	}
}

// TestClient_FallbackOnLowConfidence — primary 가 낮은 confidence 반환 시 fallback 호출.
func TestClient_FallbackOnLowConfidence(t *testing.T) {
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls = append(calls, req.Model)
		var content string
		if req.Model == "primary" {
			content = `{"can_apply":false,"reason":"unsure","confidence":0.2}`
		} else {
			content = `{"can_apply":true,"reason":"ok","confidence":0.9}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(Config{
		Endpoint: srv.URL,
		Primary:  "primary",
		Fallback: "fallback",
		Timeout:  2 * time.Second,
	})
	a, err := c.Analyze(context.Background(), "p")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !a.Fallback || a.ModelUsed != "fallback" {
		t.Errorf("expected fallback model used, got %+v", a)
	}
	if len(calls) != 2 || calls[0] != "primary" || calls[1] != "fallback" {
		t.Errorf("expected [primary, fallback], got %v", calls)
	}
}

func TestBuildOperatorIntentPrompt_HasReason(t *testing.T) {
	p := BuildOperatorIntentPrompt(OperatorIntentContext{
		TankID:         "tank_01",
		Species:        "salmon",
		OperatorReason: "GET시간 80% 단축 필요",
	})
	if !strings.Contains(p, "GET시간 80% 단축 필요") {
		t.Error("prompt missing operator reason")
	}
	if !strings.Contains(p, "JSON") {
		t.Error("prompt missing schema mention")
	}
}
