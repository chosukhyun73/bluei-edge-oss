package sync

import (
	"context"
	"time"

	"bluei.kr/edge/internal/common"
)

// PushTreatment bridges a hatchery treatment (MT sex-reversal, disinfection,
// antibiotic, ...) to the platform as a GDST TREATMENT KDE on the seed lot.
// The caller passes only the KDE subset (no raw 운영필드: item_id/consumed_qty/
// notes). Wrapped as a `hatchery.treatment` event and POSTed via the batch
// envelope; backend _project_gx10_treatment attaches it to the seed lot
// (lot_code) lineage idempotently by treatment_id.
func (s *Service) PushTreatment(ctx context.Context, payload map[string]any) (*GX10SyncBatchResponse, error) {
	event := map[string]any{
		"event_id":    common.NewID("htreat"),
		"event_type":  "hatchery.treatment",
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
