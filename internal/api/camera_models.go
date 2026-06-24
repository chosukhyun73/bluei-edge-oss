package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"bluei.kr/edge/internal/storage"
)

// C-11 camera_models — 카메라 모델 라이브러리 API.
// GET  /v1/camera-models         — list
// POST /v1/camera-models         — upsert (model_id 필수)
// DELETE /v1/camera-models/{id}  — delete (자식 인스턴스 있으면 409)

type cameraModelRequest struct {
	ModelID               string   `json:"model_id"`
	Vendor                string   `json:"vendor"`
	ProductCode           string   `json:"product_code"`
	DisplayName           string   `json:"display_name"`
	LensType              string   `json:"lens_type"`
	BaselineMM            *float64 `json:"baseline_mm,omitempty"`
	StereoCalibrationJSON string   `json:"stereo_calibration_json,omitempty"`
	ResolutionW           *int     `json:"resolution_w,omitempty"`
	ResolutionH           *int     `json:"resolution_h,omitempty"`
	FOVDeg                *float64 `json:"fov_deg,omitempty"`
	FPS                   *int     `json:"fps,omitempty"`
	NightMode             bool     `json:"night_mode"`
	Protocols             []string `json:"protocols,omitempty"`
	Notes                 string   `json:"notes,omitempty"`
}

var validCameraLensTypes = map[string]bool{
	"single": true, "dual": true, "fisheye": true, "ptz": true, "other": true,
}

func (s *Server) handleCameraModelsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListCameraModels(w, r)
	case http.MethodPost:
		s.handleUpsertCameraModel(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCameraModelItem(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/camera-models/"), "/")
	if rel == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/camera-models/{model_id}", "")
		return
	}
	modelID := rel
	switch r.Method {
	case http.MethodGet:
		m, err := s.store.GetCameraModel(r.Context(), modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if m == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "camera model not found", "")
			return
		}
		writeJSON(w, http.StatusOK, m)
	case http.MethodDelete:
		// FK 보호 — 자식 카메라 인스턴스 있으면 reject.
		n, err := s.store.CountCameraProfilesForModel(r.Context(), modelID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if n > 0 {
			writeError(w, http.StatusConflict, "CAMERA_MODEL_IN_USE",
				"이 모델을 사용하는 카메라 인스턴스가 있습니다. 먼저 인스턴스를 삭제하세요.", "")
			return
		}
		if err := s.store.DeleteCameraModel(r.Context(), modelID); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": modelID})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListCameraModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.ListCameraModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	items := make([]any, 0, len(models))
	for _, m := range models {
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleUpsertCameraModel(w http.ResponseWriter, r *http.Request) {
	var req cameraModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	model, err := cameraModelFromRequest(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CAMERA_MODEL", err.Error(), "")
		return
	}
	if err := s.store.UpsertCameraModel(r.Context(), model); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": model})
}

func cameraModelFromRequest(req cameraModelRequest) (*storage.CameraModel, error) {
	if strings.TrimSpace(req.ModelID) == "" {
		return nil, errors.New("model_id is required")
	}
	if strings.TrimSpace(req.Vendor) == "" {
		return nil, errors.New("vendor is required")
	}
	if strings.TrimSpace(req.ProductCode) == "" {
		return nil, errors.New("product_code is required")
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		return nil, errors.New("display_name is required")
	}
	if !validCameraLensTypes[req.LensType] {
		return nil, errors.New("lens_type must be one of: single,dual,fisheye,ptz,other")
	}
	if req.Protocols == nil {
		req.Protocols = []string{}
	}
	return &storage.CameraModel{
		ModelID:               req.ModelID,
		Vendor:                req.Vendor,
		ProductCode:           req.ProductCode,
		DisplayName:           req.DisplayName,
		LensType:              req.LensType,
		BaselineMM:            req.BaselineMM,
		StereoCalibrationJSON: req.StereoCalibrationJSON,
		ResolutionW:           req.ResolutionW,
		ResolutionH:           req.ResolutionH,
		FOVDeg:                req.FOVDeg,
		FPS:                   req.FPS,
		NightMode:             req.NightMode,
		Protocols:             req.Protocols,
		Notes:                 req.Notes,
	}, nil
}
