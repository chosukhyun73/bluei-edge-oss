package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bluei.kr/edge/internal/storage"
)

// TestLearnedRulesEnableDisableToggle — C-3l dashboard toggle round-trip.
// disable 라우트 + 신규 enable 라우트가 LearnedRule.Enabled 를 양방향으로 갱신함을 확인.
func TestLearnedRulesEnableDisableToggle(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	rule := &storage.LearnedRule{
		RuleID:        "lrule_test_toggle",
		ConditionJSON: `{"metric":"water_temperature","operator":"gt","threshold":28,"window_h":168}`,
		Severity:      "high",
		Source:        "operator_dispute",
		Confidence:    0.5,
		HitCount:      0,
		CreatedAt:     time.Now().UTC(),
		Enabled:       true,
	}
	if err := s.store.InsertLearnedRule(ctx, rule); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// disable
	req := httptest.NewRequest(http.MethodPost,
		"/v1/learned-rules/"+rule.RuleID+"/disable", nil)
	w := httptest.NewRecorder()
	s.handleLearnedRulesRoute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", w.Code, w.Body.String())
	}
	var disResp struct {
		RuleID  string `json:"rule_id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &disResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if disResp.Enabled {
		t.Errorf("expected enabled=false after disable, got true")
	}

	// confirm storage state
	rules, err := s.store.ListLearnedRules(ctx, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *storage.LearnedRule
	for _, r := range rules {
		if r.RuleID == rule.RuleID {
			found = r
			break
		}
	}
	if found == nil || found.Enabled {
		t.Fatalf("rule not disabled in storage: %+v", found)
	}

	// enable (C-3l 신규)
	req = httptest.NewRequest(http.MethodPost,
		"/v1/learned-rules/"+rule.RuleID+"/enable", nil)
	w = httptest.NewRecorder()
	s.handleLearnedRulesRoute(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", w.Code, w.Body.String())
	}
	var enResp struct {
		RuleID  string `json:"rule_id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &enResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !enResp.Enabled {
		t.Errorf("expected enabled=true after enable, got false")
	}

	rules, _ = s.store.ListLearnedRules(ctx, true)
	hit := false
	for _, r := range rules {
		if r.RuleID == rule.RuleID && r.Enabled {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("rule not enabled in storage after enable route")
	}
}
