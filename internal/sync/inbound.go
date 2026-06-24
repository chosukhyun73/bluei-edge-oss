package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// tankDeltasCursorKey is the runtime_kv key holding the inbound pull cursor
// (max created_at seen). Persisted so re-pulls resume idempotently.
const tankDeltasCursorKey = "sync.tank_deltas.cursor"

// inboundTankDelta mirrors backend schema GX10TankDelta (one tank_inventory_ledger
// row: a platform-originated stock reduction such as a confirmed sale).
type inboundTankDelta struct {
	ID             string   `json:"id"`             // ledger row id — idempotent dedup key
	GX10TankID     string   `json:"gx10_tank_id"`   // local tank id (resolved by reverse binding)
	PlatformTankID string   `json:"platform_tank_id"`
	EventType      string   `json:"event_type"` // e.g. TRADE_SOLD — NOT echoed back UP
	DeltaQtyKg     *float64 `json:"delta_qty_kg"`
	DeltaCount     *int     `json:"delta_count"`
	RefType        string   `json:"ref_type"`
	RefID          string   `json:"ref_id"`
	CreatedAt      string   `json:"created_at"`
}

// tankDeltasResponse mirrors backend schema GX10TankDeltasResponse.
type tankDeltasResponse struct {
	OK              bool               `json:"ok"`
	NodeCode        string             `json:"node_code"`
	InventoryDeltas []inboundTankDelta `json:"inventory_deltas"`
	NextCursor      string             `json:"next_cursor"`
	HasMore         bool               `json:"has_more"`
}

// tankDeltasURL derives the inbound pull endpoint from the sync endpoint origin
// (https://api.bluei.kr/gx10/sync/batches → https://api.bluei.kr/gx10/sync/tank-deltas).
func (s *Service) tankDeltasURL() string {
	origin := "https://api.bluei.kr"
	if s.cfg.Endpoint != nil && *s.cfg.Endpoint != "" {
		if u, err := url.Parse(*s.cfg.Endpoint); err == nil && u.Scheme != "" && u.Host != "" {
			origin = u.Scheme + "://" + u.Host
		}
	}
	return origin + "/gx10/sync/tank-deltas"
}

// pullTankDeltas pulls platform-originated inventory deltas (e.g. confirmed
// sales) DOWN from the backend and applies them to local tank lifecycle counts
// idempotently (ApplyInboundEvent dedups by ledger row id). The cursor is
// persisted in runtime_kv; has_more paginates within one tick. No-op when the
// sync endpoint/token are unset (offline queue mode).
func (s *Service) pullTankDeltas(ctx context.Context) {
	if s.cfg.Endpoint == nil || *s.cfg.Endpoint == "" || s.cfg.AccessToken == "" {
		return
	}
	endpoint := s.tankDeltasURL()
	for page := 0; page < 50; page++ { // pagination guard
		cursor, _, _ := s.store.KVGet(ctx, tankDeltasCursorKey)
		resp, err := s.fetchTankDeltas(ctx, endpoint, cursor)
		if err != nil {
			slog.Warn("inbound tank-deltas pull failed", "error", err)
			return
		}
		applied := 0
		for _, d := range resp.InventoryDeltas {
			reduce := 0
			if d.DeltaCount != nil {
				reduce = *d.DeltaCount
			}
			payload, _ := json.Marshal(d)
			ok, err := s.store.ApplyInboundEvent(ctx, d.ID, d.EventType, d.GX10TankID, reduce, string(payload), "", "")
			if err != nil {
				slog.Warn("apply inbound delta failed", "id", d.ID, "error", err)
				continue
			}
			if ok {
				applied++
			}
		}
		if resp.NextCursor != "" {
			if err := s.store.KVSet(ctx, tankDeltasCursorKey, resp.NextCursor); err != nil {
				slog.Warn("inbound cursor persist failed", "error", err)
			}
		}
		if applied > 0 {
			slog.Info("inbound tank-deltas applied", "applied", applied, "page", page)
		}
		if !resp.HasMore {
			return
		}
	}
}

func (s *Service) fetchTankDeltas(ctx context.Context, endpoint, since string) (*tankDeltasResponse, error) {
	reqURL := endpoint + "?limit=500"
	if since != "" {
		reqURL += "&since=" + url.QueryEscape(since)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if s.cfg.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.AccessToken)
	}
	if s.cfg.NodeCode != "" {
		req.Header.Set("X-GX10-Node", s.cfg.NodeCode)
	}
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	var out tankDeltasResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode tank-deltas: %w", err)
	}
	return &out, nil
}
