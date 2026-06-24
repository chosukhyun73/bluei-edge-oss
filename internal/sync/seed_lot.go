package sync

import (
	"context"
	"time"

	"bluei.kr/edge/internal/common"
)

// PushSeedLot bridges a hatchery spawn batch (egg/seed lot) to the platform as a
// canonical SEEDSTOCK lot. It wraps the publish payload as a single
// `hatchery.seed_lot` GX10 sync event and POSTs it via the standard batch
// envelope endpoint (POST /gx10/sync/batches). The backend
// (_project_gx10_seed_lot) issues an owner+lot_code idempotent SEEDSTOCK lot
// carrying the broodstock pedigree KDE — landBased/GDST 풀체인 연결.
//
// hatchery.seed_lot is intentionally NOT in edgeToBackendEventType: it is a
// direct, immediate "publish" push (operator action), not part of the daily
// batch path, so the envelope is built here rather than via BuildBatchEnvelope.
func (s *Service) PushSeedLot(ctx context.Context, payload map[string]any) (*GX10SyncBatchResponse, error) {
	event := map[string]any{
		"event_id":    common.NewID("seedlot"),
		"event_type":  "hatchery.seed_lot",
		"recorded_at": common.NowUTC().UTC().Format(time.RFC3339Nano),
		"payload":     payload,
	}
	envelope := &BatchEnvelope{
		BatchID:       common.NewBatchID(),
		SchemaVersion: "1.0",
		EventCount:    1,
		GeneratedAt:   common.FormatTime(common.NowUTC()),
		Events:        []map[string]any{event},
	}
	return s.postEnvelope(ctx, envelope)
}
