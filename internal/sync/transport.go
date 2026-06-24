package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GX10SyncBatchResponse mirrors the backend ACK schema returned by
// POST /v1/gx10/sync/batches. Field names match backend's Pydantic
// GX10SyncBatchResponse (docs/wip/backend-sync-endpoint-spec.md).
type GX10SyncBatchResponse struct {
	OK        bool     `json:"ok"`
	BatchID   string   `json:"batch_id"`
	Accepted  int      `json:"accepted"`
	Projected int      `json:"projected"`
	Ignored   int      `json:"ignored"`
	Errors    []string `json:"errors"`
	NodeCode  string   `json:"node_code,omitempty"`
}

// postEnvelope sends the batch envelope to the configured backend endpoint
// using Bearer token auth and returns the parsed ACK.
func (s *Service) postEnvelope(ctx context.Context, envelope *BatchEnvelope) (*GX10SyncBatchResponse, error) {
	if s.cfg.Endpoint == nil || *s.cfg.Endpoint == "" {
		return nil, fmt.Errorf("sync endpoint not configured")
	}
	endpoint := *s.cfg.Endpoint

	timeout := time.Duration(s.cfg.HTTPTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AccessToken)
	}
	// X-GX10-Node lets the backend match this node when the token is validated
	// against gx10_nodes (or the env token map). See GX10_SYNC_CONTRACT_V1.
	if s.cfg.NodeCode != "" {
		req.Header.Set("X-GX10-Node", s.cfg.NodeCode)
	}

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http POST: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 256))
	}

	var result GX10SyncBatchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(body), 256))
	}

	return &result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
