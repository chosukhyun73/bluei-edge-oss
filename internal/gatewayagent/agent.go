package gatewayagent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"bluei.kr/edge/internal/events"
)

type Agent struct {
	cfg    Config
	client *Client
	queue  *RetryQueue
}

func New(cfg Config) *Agent {
	return &Agent{cfg: cfg, client: NewClient(cfg), queue: NewRetryQueue(cfg.RetryQueuePath)}
}

func (a *Agent) Run(ctx context.Context) error {
	_ = a.flushRetry(ctx)

	var wg sync.WaitGroup
	for _, src := range a.cfg.Sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := a.runSource(ctx, src); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("gateway source stopped", "source_id", src.SourceID, "error", err)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runHealthLoop(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

func (a *Agent) runSource(ctx context.Context, src SourceConfig) error {
	f, err := os.Open(src.Path)
	if err != nil {
		return fmt.Errorf("open source %s: %w", src.Path, err)
	}
	defer f.Close()
	return a.readLoop(ctx, src, f)
}

func (a *Agent) readLoop(ctx context.Context, src SourceConfig, r io.Reader) error {
	s := bufio.NewScanner(r)
	for s.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, err := ParseFrame(s.Text(), src.Defaults)
		if err != nil {
			slog.Warn("invalid sensor frame", "source_id", src.SourceID, "error", err)
			continue
		}
		if payload.Raw == nil {
			payload.Raw = map[string]any{}
		}
		payload.Raw["source_id"] = src.SourceID
		payload.Raw["source_type"] = src.Type
		if err := a.client.PostReadings(ctx, []events.SensorReadingPayload{payload}); err != nil {
			slog.Warn("reading post failed; queued", "source_id", src.SourceID, "reading_id", payload.ReadingID, "error", err)
			if qerr := a.queue.Append(payload); qerr != nil {
				slog.Error("retry queue append failed", "error", qerr)
			}
		} else {
			_ = a.flushRetry(ctx)
		}
	}
	return s.Err()
}

func (a *Agent) runHealthLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(a.cfg.HealthIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		_ = a.client.PostHealth(ctx, events.DeviceStatusOnline, events.QualityOK, map[string]any{"sources": len(a.cfg.Sources)})
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Agent) flushRetry(ctx context.Context) error {
	items, err := a.queue.LoadAll()
	if err != nil || len(items) == 0 {
		return err
	}
	remaining := make([]events.SensorReadingPayload, 0)
	for _, item := range items {
		if err := a.client.PostReadings(ctx, []events.SensorReadingPayload{item}); err != nil {
			remaining = append(remaining, item)
		}
	}
	return a.queue.Replace(remaining)
}
