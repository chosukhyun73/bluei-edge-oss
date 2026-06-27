// Package llm — Phase F.1 operator_intent 종합 판단용 로컬 LLM client.
//
// 핵심 책임:
//   - ollama HTTP API 호출 (/v1/chat/completions OpenAI-compatible).
//   - JSON 응답 파싱 (markdown 코드 블록 wrapping 처리).
//   - fallback chain (primary 실패/저신뢰/timeout → fallback model).
//
// 안전 원칙: 본 client 가 반환하는 Analysis 는 권고이며, 최종 적용 여부는
// backend 의 safety validation layer (validate.go) 가 재확인한다.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Analysis — LLM 종합 판단 결과. operator_intents.context_json 에 저장.
type Analysis struct {
	CanApply      bool           `json:"can_apply"`
	Reason        string         `json:"reason"`
	Scope         string         `json:"scope"`
	BlockedBy     []string       `json:"blocked_by"`
	Adjustment    map[string]any `json:"adjustment"`
	ExplanationKo string         `json:"explanation_ko"`
	Confidence    float64        `json:"confidence"`
	ModelUsed     string         `json:"model_used"`
	Fallback      bool           `json:"fallback"`
	RawResponse   string         `json:"-"`
}

// Config — LLM client 설정.
type Config struct {
	Endpoint  string        // "http://localhost:11435"
	AuthToken string        // optional bearer token
	Primary   string        // "gemma4:e4b"
	Fallback  string        // "gemma4:26b"
	Timeout   time.Duration // 0 → 15s
}

// Client — ollama HTTP API wrapper with fallback chain.
type Client struct {
	endpoint  string
	authToken string
	primary   string
	fallback  string
	timeout   time.Duration
	http      *http.Client
	log       *slog.Logger
}

// NewClient — Config 검증 후 Client 인스턴스 반환.
// Endpoint 가 비어있으면 default "http://localhost:11435".
func NewClient(cfg Config) *Client {
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if endpoint == "" {
		endpoint = "http://localhost:11435"
	}
	primary := cfg.Primary
	if primary == "" {
		primary = "gemma4:e4b"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	// http.Client.Timeout 는 한 번의 호출 전체에 대한 timeout. fallback 까지 포함하면
	// 호출 측 ctx 가 별도 30~35s 를 가져야 한다. timeout * 2 + 약간 여유.
	return &Client{
		endpoint:  endpoint,
		authToken: cfg.AuthToken,
		primary:   primary,
		fallback:  cfg.Fallback,
		timeout:   timeout,
		http:      &http.Client{Timeout: timeout*2 + 5*time.Second},
		log:       slog.With("component", "llm"),
	}
}

// Analyze — prompt 전달 후 Analysis JSON 받기. fallback trigger 시 자동 재호출.
//
// fallback 발화 조건:
//   - primary 호출 자체 에러 (network/HTTP 5xx/timeout)
//   - JSON parse 실패
//   - confidence < 0.4
func (c *Client) Analyze(ctx context.Context, prompt string) (*Analysis, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("llm: empty prompt")
	}

	a, err := c.callModel(ctx, c.primary, prompt)
	if err == nil && a != nil && a.Confidence >= 0.4 {
		a.ModelUsed = c.primary
		a.Fallback = false
		return a, nil
	}

	// fallback 시도 (fallback 모델 명시된 경우만).
	if c.fallback == "" {
		if err != nil {
			return nil, fmt.Errorf("llm: primary call failed and no fallback: %w", err)
		}
		// confidence 미달이지만 fallback 없음 → 그대로 반환.
		a.ModelUsed = c.primary
		a.Fallback = false
		return a, nil
	}

	primaryErr := err
	c.log.Warn("llm primary failed, falling back",
		"primary", c.primary,
		"fallback", c.fallback,
		"primary_error", primaryErr,
		"primary_confidence", confidenceOf(a),
	)

	a2, err2 := c.callModel(ctx, c.fallback, prompt)
	if err2 != nil {
		return nil, fmt.Errorf("llm: fallback also failed (primary err=%v, fallback err=%w)", primaryErr, err2)
	}
	a2.ModelUsed = c.fallback
	a2.Fallback = true
	return a2, nil
}

// ChatTurn — 운영자 가이드 채팅의 한 메시지 (role: user|assistant).
type ChatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// nativeChatRequest — ollama 네이티브 /api/chat request.
// think:false 로 thinking(영어 CoT) 생성을 끔 → 가이드 답변 지연 대폭 단축.
// KeepAlive 로 모델을 GPU 에 상주시켜 콜드 재로딩 지연 제거.
// (OpenAI 호환 /v1/chat/completions 는 think 파라미터 미지원이라 별도 경로.)
type nativeChatRequest struct {
	Model     string         `json:"model"`
	Messages  []ChatTurn     `json:"messages"`
	Stream    bool           `json:"stream"`
	Think     bool           `json:"think"`
	KeepAlive string         `json:"keep_alive,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

// nativeChatChunk — stream:true 일 때 ollama 가 줄단위(NDJSON)로 보내는 청크.
type nativeChatChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// ChatStream — 자유 텍스트 대화 (운영자 가이드 어시스턴트 전용) 를 스트리밍한다.
// 토큰 청크가 도착할 때마다 onDelta 를 호출하고, 누적된 전체 텍스트를 반환한다.
// model 이 비어있으면 c.fallback → "bluei-edge-assistant" 순으로 fallback.
// keepAlive 가 비어있으면 ollama 기본값(5m). think:false 로 thinking 생략.
func (c *Client) ChatStream(ctx context.Context, model, keepAlive string, messages []ChatTurn, onDelta func(string)) (string, error) {
	if len(messages) == 0 {
		return "", errors.New("llm: empty messages")
	}
	if model == "" {
		model = c.fallback
	}
	if model == "" {
		model = "bluei-edge-assistant"
	}

	body := nativeChatRequest{
		Model:     model,
		Messages:  messages,
		Stream:    true,
		Think:     false,
		KeepAlive: keepAlive,
		Options: map[string]any{
			"temperature": 0.4,
			"num_ctx":     8192,
			"num_predict": 1200,
		},
	}
	bb, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/chat", bytes.NewReader(bb))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	var full strings.Builder
	dec := json.NewDecoder(resp.Body)
	for {
		var chunk nativeChatChunk
		if err := dec.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return full.String(), fmt.Errorf("decode chunk: %w", err)
		}
		if chunk.Message.Content != "" {
			full.WriteString(chunk.Message.Content)
			if onDelta != nil {
				onDelta(chunk.Message.Content)
			}
		}
		if chunk.Done {
			break
		}
	}

	content := strings.TrimSpace(full.String())
	if content == "" {
		return "", errors.New("llm: empty assistant content")
	}
	return content, nil
}

// embedResponse — ollama /api/embeddings 응답.
type embedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Embed — 텍스트를 임베딩 벡터로 변환한다 (RAG 검색용).
// model 이 비어있으면 "bge-m3"(다국어). ollama /api/embeddings 사용.
func (c *Client) Embed(ctx context.Context, model, text string) ([]float64, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("llm: empty embed text")
	}
	if model == "" {
		model = "bge-m3"
	}
	bb, err := json.Marshal(map[string]any{"model": model, "prompt": text})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/embeddings", bytes.NewReader(bb))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(er.Embedding) == 0 {
		return nil, errors.New("llm: empty embedding")
	}
	return er.Embedding, nil
}

func confidenceOf(a *Analysis) float64 {
	if a == nil {
		return 0
	}
	return a.Confidence
}

// chatRequest — OpenAI-compatible /v1/chat/completions request body.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse — minimal subset of /v1/chat/completions response we need.
type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// callModel — single model 호출. ollama 의 OpenAI 호환 endpoint 사용.
func (c *Client) callModel(ctx context.Context, model, prompt string) (*Analysis, error) {
	body := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream:      false,
		Temperature: 0.2,
	}
	bb, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/chat/completions", bytes.NewReader(bb))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, errors.New("empty choices")
	}
	content := cr.Choices[0].Message.Content
	jsonStr := extractJSONBlock(content)

	var a Analysis
	if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
		// parse fail → fallback trigger.
		return nil, fmt.Errorf("parse JSON (raw=%q): %w", truncate(content, 200), err)
	}
	if a.Adjustment == nil {
		a.Adjustment = map[string]any{}
	}
	a.RawResponse = content
	return &a, nil
}

// extractJSONBlock — LLM 출력에서 JSON 부분만 추출.
// markdown 코드 블록 (```json ... ``` 또는 ``` ... ```) 처리.
// 코드 블록이 없으면 첫 '{' 부터 마지막 '}' 까지 슬라이스.
func extractJSONBlock(s string) string {
	s = strings.TrimSpace(s)
	// ```json ... ```
	if idx := strings.Index(s, "```json"); idx >= 0 {
		rest := s[idx+len("```json"):]
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	// ``` ... ```
	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		if end := strings.Index(rest, "```"); end >= 0 {
			candidate := strings.TrimSpace(rest[:end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}
	// 첫 { 부터 마지막 } 까지.
	first := strings.Index(s, "{")
	last := strings.LastIndex(s, "}")
	if first >= 0 && last > first {
		return s[first : last+1]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
