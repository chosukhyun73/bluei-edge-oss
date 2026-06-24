package api

import "net/http"

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.store.ListDeviceStatuses(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if devices == nil {
		devices = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices, "count": len(devices)})
}
