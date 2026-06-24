package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	unsynced, _ := s.store.UnsyncedCount(ctx)
	oldestAge, _ := s.store.OldestUnsyncedAge(ctx)
	pending, _ := s.store.CountPendingBatches(ctx)
	failed, _ := s.store.CountFailedBatches(ctx)

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":                    s.cfg.Sync.Mode,
		"endpoint_configured":     s.cfg.Sync.Endpoint != nil,
		"unsynced_events":         unsynced,
		"pending_batches":         pending,
		"failed_batches":          failed,
		"oldest_unsynced_age_sec": int(oldestAge.Seconds()),
		"next_retry_at":           nil,
	})
}
