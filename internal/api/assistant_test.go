package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bluei.kr/edge/internal/llm"
	"bluei.kr/edge/internal/storage"
)

func postAssistantChat(t *testing.T, s *Server, msgs []llm.ChatTurn) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(assistantChatRequest{Messages: msgs})
	req := httptest.NewRequest(http.MethodPost, "/v1/assistant/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAssistantChat(w, req)
	return w
}

// LLM 비활성(client nil) 이면 503.
func TestAssistantChatNilClient(t *testing.T) {
	s := newTestServerWithApp(t) // llmClient 기본 nil
	w := postAssistantChat(t, s, []llm.ChatTurn{{Role: "user", Content: "안녕하세요"}})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d (%s)", w.Code, w.Body.String())
	}
}

// 입력 검증 — LLM 호출 전에 거부되어야 한다 (네트워크 불필요).
func TestAssistantChatValidation(t *testing.T) {
	s := newTestServerWithApp(t)
	// non-nil client 로 두어 nil 가드를 통과시키되, 검증이 LLM 호출보다 먼저라 네트워크는 안 탄다.
	s.llmClient = llm.NewClient(llm.Config{Endpoint: "http://127.0.0.1:1"})

	if w := postAssistantChat(t, s, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("empty messages: want 400, got %d", w.Code)
	}
	// 마지막 메시지가 user 가 아니면 400.
	if w := postAssistantChat(t, s, []llm.ChatTurn{{Role: "assistant", Content: "이전 답변"}}); w.Code != http.StatusBadRequest {
		t.Fatalf("last not user: want 400, got %d", w.Code)
	}
	// 마지막 user 메시지가 공백이면 400.
	if w := postAssistantChat(t, s, []llm.ChatTurn{{Role: "user", Content: "   "}}); w.Code != http.StatusBadRequest {
		t.Fatalf("blank user: want 400, got %d", w.Code)
	}
}

// 라이브 컨텍스트에 시스템 요약 + 수조 줄이 포함된다.
func TestBuildAssistantContext(t *testing.T) {
	s := newTestServerWithApp(t)
	ctx := context.Background()
	if err := s.store.UpsertTankProfile(ctx, &storage.TankProfile{
		TankID:      "ras_tank_02",
		DisplayName: "수조 2",
		Species:     "atlantic_salmon",
		SystemType:  "ras",
		Metadata:    map[string]any{},
	}); err != nil {
		t.Fatalf("seed tank: %v", err)
	}

	got := s.buildAssistantContext(httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(got, "■ 시스템:") {
		t.Fatalf("context missing system summary: %q", got)
	}
	if !strings.Contains(got, "ras_tank_02") {
		t.Fatalf("context missing seeded tank: %q", got)
	}
	if !strings.Contains(got, "atlantic_salmon") {
		t.Fatalf("context missing species: %q", got)
	}
}
