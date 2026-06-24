package api

import (
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/control"
)

type commandRequest struct {
	IdempotencyKey string         `json:"idempotency_key"`
	RequestedBy    map[string]any `json:"requested_by"`
	Target         map[string]any `json:"target"`
	Command        map[string]any `json:"command"`
	ExpiresInSec   int            `json:"expires_in_sec"`
}

func (s *Server) handlePostCommand(w http.ResponseWriter, r *http.Request) {
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error(), "")
		return
	}

	corrID := common.NewCorrID()

	result, err := s.ctrl.Submit(r.Context(), control.CommandRequest{
		IdempotencyKey: req.IdempotencyKey,
		RequestedBy:    trustedRequestedBy(r, req.RequestedBy),
		Target:         req.Target,
		Command:        req.Command,
		ExpiresInSec:   req.ExpiresInSec,
		CorrelationID:  corrID,
	})
	if err != nil {
		var code int
		var errCode string
		switch err.(type) {
		case *control.RejectionError:
			code = http.StatusUnprocessableEntity
			errCode = err.(*control.RejectionError).Code
		case *control.ConflictError:
			code = http.StatusConflict
			errCode = "IDEMPOTENCY_CONFLICT"
		default:
			code = http.StatusInternalServerError
			errCode = "INTERNAL_ERROR"
		}
		writeError(w, code, errCode, err.Error(), corrID)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"command_id":      result.CommandID,
		"status":          result.Status,
		"idempotency_key": req.IdempotencyKey,
	})
}

func (s *Server) handleGetCommand(w http.ResponseWriter, r *http.Request) {
	commandID := commandIDFromPath(r.URL.Path)
	if commandID == "" || commandID == r.URL.Path {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "command not found", "")
		return
	}
	cmd, err := s.store.GetCommand(r.Context(), commandID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if cmd == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "command not found", "")
		return
	}
	var payload map[string]any
	json.Unmarshal([]byte(cmd.PayloadJSON), &payload)
	writeJSON(w, http.StatusOK, map[string]any{
		"command_id":       cmd.CommandID,
		"idempotency_key":  cmd.IdempotencyKey,
		"target_device_id": cmd.TargetDeviceID,
		"command_type":     cmd.CommandType,
		"status":           cmd.Status,
		"requested_at":     common.FormatTime(cmd.RequestedAt),
		"expires_at":       common.FormatTime(cmd.ExpiresAt),
	})
}

func trustedRequestedBy(r *http.Request, fallback map[string]any) map[string]any {
	operatorID := r.Header.Get("X-BlueI-Operator-ID")
	if operatorID == "" {
		operatorID = r.Header.Get("X-Operator-ID")
	}
	if operatorID == "" {
		operatorID = "local_operator"
	}
	out := map[string]any{
		"operator_id": operatorID,
		"source":      "operator_session_header",
	}
	if fallback != nil {
		if displayName, ok := fallback["display_name"]; ok {
			out["display_name"] = displayName
		}
		if role, ok := fallback["role"]; ok {
			out["role"] = role
		}
	}
	return out
}
