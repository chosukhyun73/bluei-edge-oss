package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"bluei.kr/edge/internal/storage"
)

var groupColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type groupRequest struct {
	GroupID     string         `json:"group_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Color       string         `json:"color"`
	Metadata    map[string]any `json:"metadata"`
}

// handleGroupsRoute — /v1/groups (collection).
func (s *Server) handleGroupsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListGroups(w, r)
	case http.MethodPost:
		s.handleCreateGroup(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGroupRoute — /v1/groups/{group_id}[/tanks].
func (s *Server) handleGroupRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/groups/"), "/")
	parts := strings.Split(rel, "/")
	groupID := parts[0]
	if groupID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/groups/{group_id}", "")
		return
	}

	// /v1/groups/{group_id}/tanks
	if len(parts) > 1 && parts[1] == "tanks" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleListTanksByGroup(w, r, groupID)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetGroup(w, r, groupID)
	case http.MethodPut:
		s.handleUpdateGroup(w, r, groupID)
	case http.MethodDelete:
		s.handleDeleteGroup(w, r, groupID)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroupProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if groups == nil {
		groups = make([]*storage.GroupProfile, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": groups,
		"count": len(groups),
	})
}

func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	g, err := s.store.GetGroupProfile(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if g == nil {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "group not found", "")
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if err := validateGroupRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GROUP_BODY", err.Error(), "")
		return
	}
	p := groupFromRequest(req)
	if err := s.store.UpsertGroupProfile(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": p})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	existing, err := s.store.GetGroupProfile(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "group not found", "")
		return
	}

	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	req.GroupID = groupID // path 우선
	if err := validateGroupRequest(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GROUP_BODY", err.Error(), "")
		return
	}
	p := groupFromRequest(req)
	if err := s.store.UpsertGroupProfile(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": p})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	existing, err := s.store.GetGroupProfile(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "group not found", "")
		return
	}

	// group_id 를 참조하는 Cage/Tank가 있으면 삭제 거부.
	tanks, err := s.store.ListTanksByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if len(tanks) > 0 {
		writeError(w, http.StatusConflict, "GROUP_HAS_TANKS",
			"cannot delete group that has Cage/Tank references; clear group_id from tanks first", "")
		return
	}

	if err := s.store.DeleteGroupProfile(r.Context(), groupID); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": groupID})
}

func (s *Server) handleListTanksByGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	g, err := s.store.GetGroupProfile(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if g == nil {
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND", "group not found", "")
		return
	}
	tanks, err := s.store.ListTanksByGroup(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if tanks == nil {
		tanks = make([]*storage.TankProfile, 0)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group_id": groupID,
		"items":    tanks,
		"count":    len(tanks),
	})
}

func validateGroupRequest(req groupRequest) error {
	if req.GroupID == "" || len(req.GroupID) > 64 {
		return apiInputError("group_id is required and must be 1–64 characters")
	}
	if req.Name == "" {
		return errRequired("name")
	}
	if req.Color != "" && !groupColorRe.MatchString(req.Color) {
		return apiInputError("color must be #RRGGBB hex if provided")
	}
	return nil
}

func groupFromRequest(req groupRequest) *storage.GroupProfile {
	meta := req.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	return &storage.GroupProfile{
		GroupID:     req.GroupID,
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
		Metadata:    meta,
	}
}
