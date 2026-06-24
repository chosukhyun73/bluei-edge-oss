package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

type liveFeedRequest struct {
	GroupID         string  `json:"group_id"`
	TankID          string  `json:"tank_id"`
	FeedType        string  `json:"feed_type"`
	Strain          string  `json:"strain"`
	StartDate       string  `json:"start_date"`
	VolumeL         float64 `json:"volume_l"`
	DensityPerML    float64 `json:"density_per_ml"`
	LastHarvestDate string  `json:"last_harvest_date"`
	HarvestAmount   string  `json:"harvest_amount"`
	Status          string  `json:"status"`
	Notes           string  `json:"notes"`
}

var liveFeedTypes = map[string]bool{
	"rotifer": true, "artemia": true, "microalgae": true, "copepod": true, "other": true,
}
var liveFeedStatuses = map[string]bool{
	"culturing": true, "harvesting": true, "crashed": true, "ended": true,
}

func (s *Server) handleLiveFeedRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListLiveFeed(w, r)
	case http.MethodPost:
		s.handleCreateLiveFeed(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLiveFeedItemRoute(w http.ResponseWriter, r *http.Request) {
	cultureID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/live-feed/"), "/")
	if cultureID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/live-feed/{culture_id}", "")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.handleUpdateLiveFeed(w, r, cultureID)
	case http.MethodDelete:
		s.handleDeleteLiveFeed(w, r, cultureID)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListLiveFeed(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "group_id query param is required", "")
		return
	}
	items, err := s.store.ListLiveFeedByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if items == nil {
		items = make([]*storage.LiveFeedCulture, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{"group_id": groupID, "items": items, "count": len(items)})
}

func (s *Server) handleCreateLiveFeed(w http.ResponseWriter, r *http.Request) {
	var req liveFeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateLiveFeedRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_LIVEFEED_BODY", err.Error(), "")
		return
	}
	c := liveFeedFromRequest(req)
	c.CultureID = common.NewID("lfc")
	if err := s.store.UpsertLiveFeedCulture(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": c})
}

func (s *Server) handleUpdateLiveFeed(w http.ResponseWriter, r *http.Request, cultureID string) {
	existing, err := s.store.GetLiveFeedCulture(r.Context(), cultureID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "CULTURE_NOT_FOUND", "live feed culture not found", "")
		return
	}
	var req liveFeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateLiveFeedRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_LIVEFEED_BODY", err.Error(), "")
		return
	}
	c := liveFeedFromRequest(req)
	c.CultureID = cultureID
	c.CreatedAt = existing.CreatedAt
	if err := s.store.UpsertLiveFeedCulture(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": c})
}

func (s *Server) handleDeleteLiveFeed(w http.ResponseWriter, r *http.Request, cultureID string) {
	existing, err := s.store.GetLiveFeedCulture(r.Context(), cultureID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "CULTURE_NOT_FOUND", "live feed culture not found", "")
		return
	}
	if err := s.store.DeleteLiveFeedCulture(r.Context(), cultureID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": cultureID})
}

func validateLiveFeedRequest(req liveFeedRequest) error {
	if strings.TrimSpace(req.GroupID) == "" {
		return errRequired("group_id")
	}
	if !liveFeedTypes[req.FeedType] {
		return apiInputError("feed_type must be one of rotifer|artemia|microalgae|copepod|other")
	}
	if req.VolumeL < 0 || req.DensityPerML < 0 {
		return apiInputError("volume_l/density_per_ml must be >= 0")
	}
	if req.Status != "" && !liveFeedStatuses[req.Status] {
		return apiInputError("status must be one of culturing|harvesting|crashed|ended")
	}
	return nil
}

func liveFeedFromRequest(req liveFeedRequest) *storage.LiveFeedCulture {
	return &storage.LiveFeedCulture{
		GroupID:         strings.TrimSpace(req.GroupID),
		TankID:          strings.TrimSpace(req.TankID),
		FeedType:        req.FeedType,
		Strain:          strings.TrimSpace(req.Strain),
		StartDate:       strings.TrimSpace(req.StartDate),
		VolumeL:         req.VolumeL,
		DensityPerML:    req.DensityPerML,
		LastHarvestDate: strings.TrimSpace(req.LastHarvestDate),
		HarvestAmount:   strings.TrimSpace(req.HarvestAmount),
		Status:          strings.TrimSpace(req.Status),
		Notes:           strings.TrimSpace(req.Notes),
	}
}
