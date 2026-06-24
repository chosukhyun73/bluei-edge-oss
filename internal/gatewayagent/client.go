package gatewayagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
)

type Client struct {
	baseURL   string
	gatewayID string
	adapterID string
	http      *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		baseURL:   strings.TrimRight(cfg.EdgeBaseURL, "/"),
		gatewayID: cfg.GatewayID,
		adapterID: cfg.AdapterID,
		http:      &http.Client{Timeout: time.Duration(cfg.PostTimeoutSec) * time.Second},
	}
}

func (c *Client) PostReadings(ctx context.Context, readings []events.SensorReadingPayload) error {
	body := map[string]any{"gateway_id": c.gatewayID, "adapter_id": c.adapterID, "readings": readings}
	return c.post(ctx, "/v1/gateway/readings", body)
}

func (c *Client) PostHealth(ctx context.Context, status, quality string, details map[string]any) error {
	if status == "" {
		status = events.DeviceStatusOnline
	}
	if quality == "" {
		quality = events.QualityOK
	}
	body := map[string]any{
		"gateway_id": c.gatewayID,
		"adapter_id": c.adapterID,
		"devices": []events.DeviceHealthPayload{{
			DeviceID:   c.gatewayID,
			DeviceType: events.DeviceTypeLocalGateway,
			Status:     status,
			Quality:    quality,
			LastSeenAt: common.FormatTime(common.NowUTC()),
			Details:    details,
		}},
	}
	return c.post(ctx, "/v1/gateway/device-health", body)
}

func (c *Client) post(ctx context.Context, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s returned HTTP %d", path, resp.StatusCode)
	}
	return nil
}
