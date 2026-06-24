package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// bootstrapSnapshotDir is where the original camera frames the operator
// labeled get persisted. Training scripts read from here; the path is also
// written into the event payload's snapshot_ref field for traceability.
const bootstrapSnapshotDir = "local-ai/training/data/bootstrap/snapshots"

type bootstrapLabelRequest struct {
	CameraID    string                      `json:"camera_id"`
	TankID      string                      `json:"tank_id"`
	SnapshotRef string                      `json:"snapshot_ref"`
	OperatorID  string                      `json:"operator_id"`
	Boxes       []events.VisionBootstrapBox `json:"boxes"`
	// Image is a data URL ("data:image/jpeg;base64,....") OR plain base64
	// JPEG bytes captured by the frontend at label time. Optional; when
	// omitted the label is recorded but cannot be used for YOLO training.
	Image string `json:"image,omitempty"`
}

func (s *Server) handleVisionBootstrapLabelsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePostBootstrapLabel(w, r)
	case http.MethodGet:
		s.handleListBootstrapLabels(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePostBootstrapLabel(w http.ResponseWriter, r *http.Request) {
	var req bootstrapLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.OperatorID == "" {
		req.OperatorID = "operator"
	}
	labelID := common.NewID("vis_boot")
	snapshotRef := req.SnapshotRef
	if req.Image != "" {
		path, err := saveBootstrapSnapshot(labelID, req.Image)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_IMAGE", err.Error(), "")
			return
		}
		// snapshot_ref is replaced with the on-disk path so training scripts
		// can find the source frame. Original frontend ref is kept in the
		// raw event JSON if needed for audit (we drop it here for simplicity).
		snapshotRef = path
	}
	payload := events.VisionBootstrapLabelRecordedPayload{
		LabelID:     labelID,
		CameraID:    req.CameraID,
		TankID:      req.TankID,
		SnapshotRef: snapshotRef,
		OperatorID:  req.OperatorID,
		Boxes:       req.Boxes,
		RecordedAt:  common.FormatTime(common.NowUTC()),
	}
	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_BOOTSTRAP_LABEL", err.Error(), "")
		return
	}
	seq, err := s.app.AppendEvent(r.Context(), "api", "vision", payload.CameraID,
		events.EventVisionBootstrapLabelRecorded, payload.LabelID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "APPEND_FAILED", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sequence": seq, "label": payload})
}

func (s *Server) handleListBootstrapLabels(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50, 500)
	cameraID := r.URL.Query().Get("camera_id")
	es, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionBootstrapLabelRecorded, Limit: limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	out := make([]map[string]any, 0, len(es))
	totalBoxes := 0
	for _, e := range es {
		var p events.VisionBootstrapLabelRecordedPayload
		if err := json.Unmarshal([]byte(e.PayloadJSON), &p); err != nil {
			continue
		}
		if cameraID != "" && p.CameraID != cameraID {
			continue
		}
		totalBoxes += len(p.Boxes)
		out = append(out, map[string]any{
			"sequence":    e.Sequence,
			"event_id":    e.EventID,
			"recorded_at": common.FormatTime(e.RecordedAt),
			"payload":     p,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":       out,
		"count":       len(out),
		"total_boxes": totalBoxes,
	})
}

// saveBootstrapSnapshot persists the operator-labeled camera frame to disk so
// training scripts can read it. Accepts either a data URL or raw base64 JPEG.
func saveBootstrapSnapshot(labelID, image string) (string, error) {
	if i := strings.Index(image, ","); strings.HasPrefix(image, "data:") && i > 0 {
		image = image[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(image))
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if len(data) < 200 {
		return "", fmt.Errorf("image too small (%d bytes)", len(data))
	}
	if err := os.MkdirAll(bootstrapSnapshotDir, 0o755); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}
	path := filepath.Join(bootstrapSnapshotDir, labelID+".jpg")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write snapshot: %w", err)
	}
	return path, nil
}
