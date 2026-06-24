package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type feedingRecordRequest struct {
	FeedingID              string         `json:"feeding_id"`
	TankID                 string         `json:"tank_id"`
	FeederID               string         `json:"feeder_id"`
	Source                 string         `json:"source"`
	FeedAmountG            float64        `json:"feed_amount_g"`
	FeedType               string         `json:"feed_type"`
	FeedLot                string         `json:"feed_lot"`
	FeedSupplier           string         `json:"feed_supplier"`
	ItemID                 string         `json:"item_id"`
	ConsumedQty            float64        `json:"consumed_qty"`
	FedAt                  string         `json:"fed_at"`
	RecordedBy             string         `json:"recorded_by"`
	LinkedRecommendationID string         `json:"linked_recommendation_id"`
	Quality                string         `json:"quality"`
	Evidence               map[string]any `json:"evidence"`
}

func (s *Server) handlePostFeedingRecord(w http.ResponseWriter, r *http.Request) {
	var req feedingRecordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	payload := feedingPayloadFromRequest(req)
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_FEEDING_RECORD", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "operator", payload.FeederID, events.EventFeedingRecorded, payload.FeedingID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	resp := map[string]any{"ok": true, "sequence": seq, "feeding": payload}
	// 재고 차감 — 품목 지정 시. 차감 실패는 급이 기록을 막지 않고 경고만.
	if payload.ItemID != "" && payload.ConsumedQty > 0 {
		if onHand, cerr := s.consumeInventory(r.Context(), payload.ItemID, payload.ConsumedQty,
			"feeding", payload.FeedingID, payload.TankID, payload.RecordedBy, "", payload.FedAt); cerr != nil {
			resp["inventory_warning"] = cerr.Error()
		} else {
			resp["item_on_hand"] = onHand
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

func feedingPayloadFromRequest(req feedingRecordRequest) events.FeedingRecordedPayload {
	if req.FeedingID == "" {
		req.FeedingID = common.NewID("feeding")
	}
	if req.Source == "" {
		req.Source = events.FeedingSourceManual
	}
	if req.FedAt == "" {
		req.FedAt = common.FormatTime(common.NowUTC())
	}
	if req.Quality == "" {
		req.Quality = events.QualityOK
	}
	return events.FeedingRecordedPayload{
		FeedingID:              req.FeedingID,
		TankID:                 req.TankID,
		FeederID:               req.FeederID,
		Source:                 req.Source,
		FeedAmountG:            req.FeedAmountG,
		FeedType:               req.FeedType,
		FeedLot:                req.FeedLot,
		FeedSupplier:           req.FeedSupplier,
		ItemID:                 req.ItemID,
		ConsumedQty:            req.ConsumedQty,
		FedAt:                  req.FedAt,
		RecordedBy:             req.RecordedBy,
		LinkedRecommendationID: req.LinkedRecommendationID,
		Quality:                req.Quality,
		Evidence:               req.Evidence,
	}
}

func (s *Server) handleRecentFeedings(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 20, 100)
	items, err := s.listFeedingRecords(r, limit, time.Time{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feedings": items, "count": len(items)})
}

func (s *Server) handleTodayFeedingSummary(w http.ResponseWriter, r *http.Request) {
	loc, err := time.LoadLocation(s.cfg.Site.Timezone)
	if err != nil {
		loc = time.Local
	}
	now := common.NowUTC().In(loc)
	startLocal := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	items, err := s.listFeedingRecords(r, 1000, startLocal.UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	byTank := map[string]float64{}
	total := 0.0
	for _, item := range items {
		payload, ok := item["payload"].(events.FeedingRecordedPayload)
		if !ok {
			continue
		}
		byTank[payload.TankID] += payload.FeedAmountG
		total += payload.FeedAmountG
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"date":              startLocal.Format("2006-01-02"),
		"timezone":          loc.String(),
		"total_feed_g":      total,
		"feed_by_tank_g":    byTank,
		"feeding_count":     len(items),
		"recent_feedings":   items,
		"generated_at":      common.FormatTime(common.NowUTC()),
		"source_event_type": events.EventFeedingRecorded,
	})
}

func (s *Server) listFeedingRecords(r *http.Request, limit int, since time.Time) ([]map[string]any, error) {
	var sincePtr *time.Time
	if !since.IsZero() {
		sincePtr = &since
	}
	eventsList, err := s.store.QueryEvents(r.Context(), storage.EventFilter{EventType: events.EventFeedingRecorded, Limit: limit, Since: sincePtr})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		var payload events.FeedingRecordedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"sequence":    e.Sequence,
			"event_id":    e.EventID,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"payload":     payload,
		})
	}
	return out, nil
}

func intParam(r *http.Request, key string, def, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
