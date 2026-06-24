package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

func (s *Server) handleReadingsRecent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		DeviceID:  q.Get("sensor_id"),
		Limit:     50,
	}
	if lim := q.Get("limit"); lim != "" {
		if n, err := strconv.Atoi(lim); err == nil {
			if n > 500 {
				n = 500
			}
			f.Limit = n
		}
	}
	if since := q.Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PARAM", "since must be RFC3339", "")
			return
		}
		f.Since = &t
	}

	events, err := s.store.QueryEvents(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	readings := make([]any, 0, len(events))
	for _, e := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
			readings = append(readings, payload)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"readings": readings, "count": len(readings)})
}

func (s *Server) handleReadingsLatest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if lim := q.Get("limit"); lim != "" {
		if n, err := strconv.Atoi(lim); err == nil {
			if n > 500 {
				n = 500
			}
			if n > 0 {
				limit = n
			}
		}
	}

	readings, err := s.store.LatestSensorReadings(r.Context(), storage.LatestReadingFilter{
		TankID:   q.Get("tank_id"),
		SensorID: q.Get("sensor_id"),
		Metric:   q.Get("metric"),
		Limit:    limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if readings == nil {
		readings = []*storage.LatestSensorReading{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"readings": readings, "count": len(readings)})
}
