// Command synccheck is the GX10↔app.bluei.kr end-to-end sync verification
// harness. It pulls real recent events from the local edge DB, maps them to a
// chosen backend event_type, and (optionally) POSTs them to the live backend —
// printing only the ACK (accepted/projected/ignored/errors). The device token
// is loaded from device_credentials.json and never printed.
//
// It exists because the sync pipeline was built without an end-to-end check:
// this lets every subsequent fix be proven by observing a real event land as
// projected>0 (and, downstream, a row in the backend env_readings table).
//
//	synccheck -config configs/edge.example.yaml -type sensor.reading.recorded \
//	          -backend-type sensor.reading.recorded -n 1 -post
//
// Without -post it is a dry run: it prints the envelope it would send.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
	syncpkg "bluei.kr/edge/internal/sync"
)

func main() {
	cfgPath := flag.String("config", "configs/edge.yaml", "edge config path")
	edgeType := flag.String("type", "sensor.reading.recorded", "edge event_type to pull from the local DB")
	backendType := flag.String("backend-type", "", "backend event_type to send as (default: same as -type)")
	metric := flag.String("metric", "", "only include events whose payload.metric matches (e.g. water_temperature)")
	n := flag.Int("n", 1, "number of most-recent matching events to include")
	post := flag.Bool("post", false, "actually POST to the backend (default: dry run, print envelope only)")
	flag.Parse()

	if *backendType == "" {
		*backendType = *edgeType
	}

	cfg, _, _, err := config.Load(*cfgPath)
	if err != nil {
		fatal("load config: %v", err)
	}
	if err := config.ApplyPairCreds(cfg); err != nil {
		fatal("apply pair creds: %v", err)
	}
	config.ApplyDeviceCreds(cfg)

	if cfg.Sync.Endpoint == nil || *cfg.Sync.Endpoint == "" {
		fatal("sync.endpoint not configured")
	}
	fmt.Printf("endpoint   : %s\n", *cfg.Sync.Endpoint)
	fmt.Printf("node_code  : %s\n", cfg.Sync.NodeCode)
	fmt.Printf("auth       : %s\n", tokenState(cfg.Sync.AccessToken))

	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		fatal("open store %s: %v", cfg.Storage.SQLitePath, err)
	}
	defer store.Close()

	ctx := context.Background()
	// Over-fetch when filtering by metric so we still land -n matches.
	queryLimit := *n
	if *metric != "" {
		queryLimit = *n * 200
	}
	events, err := store.QueryEvents(ctx, storage.EventFilter{EventType: *edgeType, Limit: queryLimit})
	if err != nil {
		fatal("query events: %v", err)
	}
	if *metric != "" {
		events = filterByMetric(events, *metric, *n)
	}
	if len(events) == 0 {
		fatal("no events of type %q (metric %q) in local DB", *edgeType, *metric)
	}

	envelope := buildEnvelope(events, *backendType)
	fmt.Printf("edge_type  : %s  → backend_type: %s  (%d event[s])\n", *edgeType, *backendType, len(envelope.Events))

	if !*post {
		b, _ := json.MarshalIndent(envelope, "", "  ")
		fmt.Printf("\n--- DRY RUN envelope (add -post to send) ---\n%s\n", b)
		return
	}

	resp, err := syncpkg.PostEnvelope(ctx, &cfg.Sync, envelope)
	if err != nil {
		fatal("POST failed: %v", err)
	}
	fmt.Printf("\n--- ACK ---\n")
	fmt.Printf("ok=%v  accepted=%d  projected=%d  ignored=%d  node_code=%s\n",
		resp.OK, resp.Accepted, resp.Projected, resp.Ignored, resp.NodeCode)
	if len(resp.Errors) > 0 {
		fmt.Printf("errors    : %v\n", resp.Errors)
	}
	switch {
	case resp.Projected > 0:
		fmt.Printf("RESULT     : ✅ projected — 백엔드 데이터 모델까지 도달 (env_readings 등)\n")
	case resp.Ignored > 0:
		fmt.Printf("RESULT     : ⚠️  accepted but ignored — 원인은 errors 참조 (tank 미해석/중복/미투영)\n")
	default:
		fmt.Printf("RESULT     : ⚠️  projected=0 — 투영 대상 아님 또는 payload 형태 불일치\n")
	}
}

// buildEnvelope mirrors the shape produced by sync.BuildBatchEnvelope but lets
// the caller choose the backend event_type, so a fix to the edge allowlist can
// be verified before it is applied. tank routing is preserved from the real
// payload (top-level tank_id or nested location.tank_id) so the backend can
// resolve platform_tank_id.
func buildEnvelope(events []*storage.Event, backendType string) *syncpkg.BatchEnvelope {
	out := make([]map[string]any, 0, len(events))
	var minSeq, maxSeq int64
	for i, e := range events {
		if i == 0 || e.Sequence < minSeq {
			minSeq = e.Sequence
		}
		if i == 0 || e.Sequence > maxSeq {
			maxSeq = e.Sequence
		}
		var payload map[string]any
		if e.PayloadJSON != "" {
			_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
		}
		if payload == nil {
			payload = map[string]any{}
		}
		ev := map[string]any{
			"event_id":    e.EventID,
			"event_type":  backendType,
			"recorded_at": e.RecordedAt.UTC().Format(time.RFC3339Nano),
			"payload":     payload,
		}
		if tid := tankID(payload); tid != "" {
			ev["gx10_tank_id"] = tid
		}
		if e.SourceModule != "" || e.SourceAdapter != "" || e.SourceDevice != "" {
			ev["source"] = map[string]string{
				"module": e.SourceModule, "adapter": e.SourceAdapter, "device_id": e.SourceDevice,
			}
		}
		out = append(out, ev)
	}
	return &syncpkg.BatchEnvelope{
		BatchID:       fmt.Sprintf("synccheck-%d", time.Now().UnixNano()),
		SchemaVersion: "1.0",
		FromSequence:  minSeq,
		ToSequence:    maxSeq,
		EventCount:    len(out),
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Events:        out,
	}
}

// filterByMetric keeps at most n events whose payload.metric equals metric,
// preserving the DB's most-recent-first order.
func filterByMetric(events []*storage.Event, metric string, n int) []*storage.Event {
	out := make([]*storage.Event, 0, n)
	for _, e := range events {
		var p map[string]any
		if e.PayloadJSON != "" {
			_ = json.Unmarshal([]byte(e.PayloadJSON), &p)
		}
		if m, _ := p["metric"].(string); m == metric {
			out = append(out, e)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

func tankID(payload map[string]any) string {
	if v, ok := payload["tank_id"].(string); ok && v != "" {
		return v
	}
	if loc, ok := payload["location"].(map[string]any); ok {
		if v, ok := loc["tank_id"].(string); ok {
			return v
		}
	}
	return ""
}

func tokenState(t string) string {
	if t == "" {
		return "없음(빈 토큰)"
	}
	return fmt.Sprintf("설정됨(%d자)", len(t))
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "synccheck: "+format+"\n", a...)
	os.Exit(1)
}
