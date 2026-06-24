package api

import (
	"encoding/json"
	"net/http"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
)

type gatewayReadingsRequest struct {
	GatewayID string                        `json:"gateway_id"`
	AdapterID string                        `json:"adapter_id"`
	Readings  []events.SensorReadingPayload `json:"readings"`
}

type gatewayDeviceHealthRequest struct {
	GatewayID string                       `json:"gateway_id"`
	AdapterID string                       `json:"adapter_id"`
	Devices   []events.DeviceHealthPayload `json:"devices"`
}

type gatewayIngestItemResult struct {
	Index         int    `json:"index"`
	Sequence      int64  `json:"sequence,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Error         string `json:"error,omitempty"`
}

func (s *Server) handlePostGatewayReadings(w http.ResponseWriter, r *http.Request) {
	var req gatewayReadingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.GatewayID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GATEWAY_BATCH", "gateway_id is required", "")
		return
	}
	if req.AdapterID == "" {
		req.AdapterID = req.GatewayID
	}
	if len(req.Readings) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GATEWAY_BATCH", "readings must not be empty", "")
		return
	}

	results := make([]gatewayIngestItemResult, 0, len(req.Readings))
	accepted := 0
	for i, payload := range req.Readings {
		if payload.ReadingID == "" {
			payload.ReadingID = common.NewID("reading")
		}
		if payload.ObservedAt == "" {
			payload.ObservedAt = common.FormatTime(common.NowUTC())
		}
		if payload.Quality == "" {
			payload.Quality = events.QualityOK
		}
		if err := payload.Validate(); err != nil {
			results = append(results, gatewayIngestItemResult{Index: i, Error: err.Error()})
			continue
		}
		seq, err := s.app.AppendEvent(r.Context(), "gateway", req.AdapterID, payload.DeviceID, events.EventSensorReadingRecorded, payload.ReadingID, payload)
		if err != nil {
			results = append(results, gatewayIngestItemResult{Index: i, Error: err.Error()})
			continue
		}
		accepted++
		results = append(results, gatewayIngestItemResult{Index: i, Sequence: seq, CorrelationID: payload.ReadingID})
	}

	status := http.StatusCreated
	if accepted == 0 {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]any{"ok": accepted == len(req.Readings), "accepted": accepted, "rejected": len(req.Readings) - accepted, "results": results})
}

func (s *Server) handlePostGatewayDeviceHealth(w http.ResponseWriter, r *http.Request) {
	var req gatewayDeviceHealthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.GatewayID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GATEWAY_BATCH", "gateway_id is required", "")
		return
	}
	if req.AdapterID == "" {
		req.AdapterID = req.GatewayID
	}
	if len(req.Devices) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_GATEWAY_BATCH", "devices must not be empty", "")
		return
	}

	results := make([]gatewayIngestItemResult, 0, len(req.Devices))
	accepted := 0
	for i, payload := range req.Devices {
		if payload.Quality == "" {
			payload.Quality = events.QualityOK
		}
		if payload.Status == "" {
			payload.Status = events.DeviceStatusUnknown
		}
		if payload.LastSeenAt == "" {
			payload.LastSeenAt = common.FormatTime(common.NowUTC())
		}
		if err := payload.Validate(); err != nil {
			results = append(results, gatewayIngestItemResult{Index: i, Error: err.Error()})
			continue
		}
		seq, err := s.app.AppendEvent(r.Context(), "gateway", req.AdapterID, payload.DeviceID, events.EventDeviceHealthUpdated, payload.DeviceID, payload)
		if err != nil {
			results = append(results, gatewayIngestItemResult{Index: i, Error: err.Error()})
			continue
		}
		accepted++
		results = append(results, gatewayIngestItemResult{Index: i, Sequence: seq, CorrelationID: payload.DeviceID})
	}

	status := http.StatusCreated
	if accepted == 0 {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]any{"ok": accepted == len(req.Devices), "accepted": accepted, "rejected": len(req.Devices) - accepted, "results": results})
}
