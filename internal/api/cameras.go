package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type cameraProfileRequest struct {
	CameraID          string         `json:"camera_id"`
	TankID            string         `json:"tank_id"`
	DisplayName       string         `json:"display_name"`
	Vendor            string         `json:"vendor"`
	Host              string         `json:"host"`
	RTSPPort          int            `json:"rtsp_port"`
	HTTPPort          int            `json:"http_port"`
	Username          string         `json:"username"`
	Password          string         `json:"password"`
	PasswordSecretRef string         `json:"password_secret_ref"`
	Position          string         `json:"position"`
	Purpose           []string       `json:"purpose"`
	StreamProfiles    map[string]any `json:"stream_profiles"`
	ClipPolicy        map[string]any `json:"clip_policy"`
	Status            string         `json:"status"`
	Metadata          map[string]any `json:"metadata"`
	// C-11 camera model library link + 설치 정보 (인스턴스 고유).
	ModelID          string   `json:"model_id"`
	MountingHeightM  *float64 `json:"mounting_height_m"`
	UnderwaterDepthM *float64 `json:"underwater_depth_m"`
	// C-12 — 카메라 메타 정정. position 의미 분리 + 수면 기준 명시.
	MountLocation    string   `json:"mount_location"`
	ViewAngle        string   `json:"view_angle"`
	HeightFromWaterM *float64 `json:"height_from_water_m"`
	TiltDeg          *float64 `json:"tilt_deg"`
}

// C-12 — enum 화이트리스트. 모르는 값은 422.
var validMountLocations = map[string]bool{
	"":             true, // 미지정 허용 (선택값)
	"feeder_zone":  true,
	"water_intake": true,
	"water_outlet": true,
	"tank_center":  true,
	"tank_side":    true,
	"other":        true,
}

var validViewAngles = map[string]bool{
	"":                true,
	"top_down":        true,
	"oblique_top":     true,
	"side_horizontal": true,
	"underwater_top":  true,
	"underwater_side": true,
}

var validCameraPurposes = map[string]bool{
	"behavior":              true,
	"counting":              true,
	"size_estimation":       true,
	"feeding_detect":        true,
	"operator_view":         true,
	"intake_outlet_monitor": true,
	// C-11 호환성 — 기존 'vision_ai' 도 잠시 허용 (deprecate 예정).
	"vision_ai": true,
}

func (s *Server) handleCamerasRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListCameras(w, r)
	case http.MethodPost:
		s.handleUpsertCamera(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCameraRoute(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/cameras/"), "/")
	parts := strings.Split(rel, "/")
	cameraID := parts[0]
	if cameraID == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "expected /v1/cameras/{camera_id}", "")
		return
	}
	if cameraID == "status" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleCameraStatuses(w, r)
		return
	}
	if cameraID == "discover" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleCameraDiscovery(w, r)
		return
	}
	if cameraID == "test-profile" {
		if len(parts) > 1 && strings.Join(parts[1:], "/") == "snapshot.jpg" {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraProfileSnapshot(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleCameraProfileTest(w, r)
		return
	}
	if len(parts) > 1 {
		switch strings.Join(parts[1:], "/") {
		case "secret":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleSetCameraSecret(w, r, cameraID)
		case "test":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraTest(w, r, cameraID)
		case "live.mjpeg":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraMJPEG(w, r, cameraID)
		case "preview/start":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraPreviewStart(w, r, cameraID)
		case "preview/stop":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraPreviewStop(w, r, cameraID)
		case "preview/index.m3u8":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraPreviewFile(w, r, cameraID, "index.m3u8")
		case "snapshot.jpg":
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleCameraSnapshot(w, r, cameraID)
		default:
			if len(parts) == 3 && parts[1] == "preview" && strings.HasSuffix(parts[2], ".ts") {
				s.handleCameraPreviewFile(w, r, cameraID, parts[2])
				return
			}
			writeError(w, http.StatusNotFound, "NOT_FOUND", "unknown camera route", "")
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		profile, err := s.store.GetCameraProfile(r.Context(), cameraID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		if profile == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "camera profile not found", "")
			return
		}
		writeJSON(w, http.StatusOK, redactCameraProfile(profile))
	case http.MethodDelete:
		if err := s.store.DeleteCameraProfile(r.Context(), cameraID); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": cameraID})
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSetCameraSecret(w http.ResponseWriter, r *http.Request, cameraID string) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if body.Password == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CAMERA_SECRET", "password is required", "")
		return
	}
	ref := cameraSecretRef(cameraID)
	if err := s.store.KVSet(r.Context(), ref, body.Password); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	profile, err := s.store.GetCameraProfile(r.Context(), cameraID)
	if err == nil && profile != nil && profile.PasswordSecretRef == "" {
		profile.PasswordSecretRef = ref
		_ = s.store.UpsertCameraProfile(r.Context(), profile)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "password_secret_ref": ref})
}

func (s *Server) handleCameraSnapshot(w http.ResponseWriter, r *http.Request, cameraID string) {
	profile, err := s.store.GetCameraProfile(r.Context(), cameraID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "camera profile not found", "")
		return
	}
	tier := r.URL.Query().Get("profile")
	if tier == "" {
		tier = "sub"
	}
	rtspURL, err := s.cameraRTSPURL(r.Context(), profile, tier)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "CAMERA_URL_ERROR", err.Error(), "")
		return
	}
	worker, err := s.ensureCameraJPEGWorker(r.Context(), profile, tier, rtspURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "CAMERA_SNAPSHOT_FAILED", err.Error(), "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	frame, ok := worker.Hub.WaitLatest(ctx)
	if !ok {
		writeError(w, http.StatusGatewayTimeout, "CAMERA_SNAPSHOT_TIMEOUT", "latest camera frame is not ready", "")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(frame.Data)
}

func (s *Server) handleCameraProfileSnapshot(w http.ResponseWriter, r *http.Request) {
	var req cameraProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	rtspURL, err := cameraRTSPURLFromRequest(req, r.URL.Query().Get("profile"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "CAMERA_URL_ERROR", err.Error(), "")
		return
	}
	img, err := snapshotJPEG(r.Context(), rtspURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "CAMERA_SNAPSHOT_FAILED", err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(img)
}

func (s *Server) handleCameraProfileTest(w http.ResponseWriter, r *http.Request) {
	var req cameraProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	if req.CameraID == "" || req.TankID == "" {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CAMERA_PROFILE", "camera_id and tank_id are required", "")
		return
	}
	encoded, _ := json.Marshal(req)
	if bytes.Contains(bytes.ToLower(encoded), []byte("rtsp://")) && bytes.Contains(encoded, []byte("@")) {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CAMERA_PROFILE", errCredentialedRTSPURL.Error(), "")
		return
	}
	result := map[string]any{"camera_id": req.CameraID, "tcp": map[string]any{}, "snapshot_ok": false, "would_save": false}
	if req.Host != "" {
		ports := []int{req.RTSPPort, req.HTTPPort, 8000}
		if ports[0] == 0 {
			ports[0] = 554
		}
		if ports[1] == 0 {
			ports[1] = 80
		}
		tcp := map[string]bool{}
		for _, port := range ports {
			if port > 0 {
				tcp[fmt.Sprintf("%d", port)] = tcpOpen(r.Context(), req.Host, port, 900*time.Millisecond)
			}
		}
		result["tcp"] = tcp
	}
	rtspURL, err := cameraRTSPURLFromRequest(req, r.URL.Query().Get("profile"))
	if err != nil {
		result["snapshot_error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	if _, err := snapshotJPEG(r.Context(), rtspURL); err != nil {
		result["snapshot_error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result["snapshot_ok"] = true
	result["would_save"] = true
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListCameras(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.store.ListCameraProfiles(r.Context(), r.URL.Query().Get("tank_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	items := make([]any, 0, len(profiles))
	for _, p := range profiles {
		items = append(items, redactCameraProfile(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleUpsertCamera(w http.ResponseWriter, r *http.Request) {
	var req cameraProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body", "")
		return
	}
	profile, err := cameraProfileFromRequest(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "INVALID_CAMERA_PROFILE", err.Error(), "")
		return
	}
	if err := s.store.UpsertCameraProfile(r.Context(), profile); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	_, _ = s.app.AppendEvent(r.Context(), "api", "camera_registry", profile.CameraID, events.EventCameraProfileUpserted, profile.CameraID, redactCameraProfile(profile))
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "item": redactCameraProfile(profile)})
}

func cameraProfileFromRequest(req cameraProfileRequest) (*storage.CameraProfile, error) {
	if strings.TrimSpace(req.Password) != "" {
		return nil, errPlaintextCameraPassword
	}
	encoded, _ := json.Marshal(req)
	if bytes.Contains(bytes.ToLower(encoded), []byte("rtsp://")) && bytes.Contains(encoded, []byte("@")) {
		return nil, errCredentialedRTSPURL
	}
	if req.CameraID == "" {
		return nil, errRequired("camera_id")
	}
	if req.DisplayName == "" {
		return nil, errRequired("display_name")
	}
	if req.TankID == "" {
		return nil, errRequired("tank_id")
	}
	if req.Status == "" {
		req.Status = "configured"
	}
	if req.RTSPPort == 0 {
		req.RTSPPort = 554
	}
	if req.HTTPPort == 0 {
		req.HTTPPort = 80
	}
	if req.Purpose == nil {
		req.Purpose = []string{"operator_view"}
	}
	// C-12 — enum 검증.
	if !validMountLocations[req.MountLocation] {
		return nil, errInvalidEnum("mount_location", req.MountLocation)
	}
	if !validViewAngles[req.ViewAngle] {
		return nil, errInvalidEnum("view_angle", req.ViewAngle)
	}
	for _, p := range req.Purpose {
		if !validCameraPurposes[p] {
			return nil, errInvalidEnum("purpose", p)
		}
	}
	if req.StreamProfiles == nil {
		req.StreamProfiles = map[string]any{}
	}
	if req.ClipPolicy == nil {
		req.ClipPolicy = map[string]any{}
	}
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	return &storage.CameraProfile{
		CameraID:          req.CameraID,
		TankID:            req.TankID,
		DisplayName:       req.DisplayName,
		Vendor:            req.Vendor,
		Host:              req.Host,
		RTSPPort:          req.RTSPPort,
		HTTPPort:          req.HTTPPort,
		Username:          req.Username,
		PasswordSecretRef: req.PasswordSecretRef,
		Position:          req.Position,
		Purpose:           req.Purpose,
		StreamProfiles:    req.StreamProfiles,
		ClipPolicy:        req.ClipPolicy,
		Status:            req.Status,
		Metadata:          req.Metadata,
		ModelID:           req.ModelID,
		MountingHeightM:   req.MountingHeightM,
		UnderwaterDepthM:  req.UnderwaterDepthM,
		MountLocation:     req.MountLocation,
		ViewAngle:         req.ViewAngle,
		HeightFromWaterM:  req.HeightFromWaterM,
		TiltDeg:           req.TiltDeg,
	}, nil
}

func redactCameraProfile(p *storage.CameraProfile) map[string]any {
	out := map[string]any{
		"camera_id":           p.CameraID,
		"tank_id":             p.TankID,
		"display_name":        p.DisplayName,
		"vendor":              p.Vendor,
		"host":                p.Host,
		"rtsp_port":           p.RTSPPort,
		"http_port":           p.HTTPPort,
		"username":            p.Username,
		"has_password_secret": p.PasswordSecretRef != "",
		"position":            p.Position,
		"purpose":             p.Purpose,
		"stream_profiles":     p.StreamProfiles,
		"clip_policy":         p.ClipPolicy,
		"status":              p.Status,
		"metadata":            p.Metadata,
		"updated_at":          p.UpdatedAt,
		"model_id":            p.ModelID,
	}
	if p.MountingHeightM != nil {
		out["mounting_height_m"] = *p.MountingHeightM
	}
	if p.UnderwaterDepthM != nil {
		out["underwater_depth_m"] = *p.UnderwaterDepthM
	}
	if p.MountLocation != "" {
		out["mount_location"] = p.MountLocation
	}
	if p.ViewAngle != "" {
		out["view_angle"] = p.ViewAngle
	}
	if p.HeightFromWaterM != nil {
		out["height_from_water_m"] = *p.HeightFromWaterM
	}
	if p.TiltDeg != nil {
		out["tilt_deg"] = *p.TiltDeg
	}
	return out
}

// vendorRTSPPaths — 벤더별 기본 RTSP 경로 [main, sub]. stream profile 에 path/url_hint
// 가 없을 때 fallback (대부분 카메라가 모델별 경로 정보를 안 주므로 벤더로 추정).
// 미상 벤더는 빈 문자열 → 경로 수동 지정 필요.
var vendorRTSPPaths = map[string][2]string{
	"hikvision": {"/Streaming/Channels/101", "/Streaming/Channels/102"},
	"dahua":     {"/cam/realmonitor?channel=1&subtype=0", "/cam/realmonitor?channel=1&subtype=1"},
	"hanwha":    {"/profile2/media.smp", "/profile5/media.smp"},
	"samsung":   {"/profile2/media.smp", "/profile5/media.smp"},
	"axis":      {"/axis-media/media.amp", "/axis-media/media.amp?resolution=320x240"},
	"reolink":   {"/h264Preview_01_main", "/h264Preview_01_sub"},
	"balus":     {"/0", "/1"},
}

func defaultRTSPPath(vendor, tier string) string {
	p, ok := vendorRTSPPaths[strings.ToLower(strings.TrimSpace(vendor))]
	if !ok {
		return ""
	}
	if tier == "main" {
		return p[0]
	}
	return p[1]
}

// buildRTSPURL — host/port/path(쿼리 포함 가능)/계정으로 RTSP URL 조립.
func buildRTSPURL(host string, port int, path, username, password string) string {
	if port == 0 {
		port = 554
	}
	rawPath, rawQuery := path, ""
	if i := strings.IndexByte(path, '?'); i >= 0 {
		rawPath, rawQuery = path[:i], path[i+1:]
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	u := &url.URL{Scheme: "rtsp", Host: fmt.Sprintf("%s:%d", host, port), Path: rawPath, RawQuery: rawQuery}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String()
}

func cameraRTSPURLFromRequest(req cameraProfileRequest, tier string) (string, error) {
	if tier == "" {
		tier = "sub"
	}
	profile, _ := req.StreamProfiles[tier].(map[string]any)
	if profile == nil && tier != "main" {
		profile, _ = req.StreamProfiles["main"].(map[string]any)
	}
	if hint, _ := profile["url_hint"].(string); strings.HasPrefix(hint, "rtsp://") {
		if strings.Contains(hint, "@") {
			return "", errCredentialedRTSPURL
		}
		return hint, nil
	}
	path, _ := profile["path"].(string)
	if path == "" {
		path = defaultRTSPPath(req.Vendor, tier) // 벤더 기본 경로 fallback
	}
	if path == "" {
		return "", fmt.Errorf("stream profile %q 에 url_hint/path 없음 + vendor %q 기본 경로 미상 — RTSP 경로를 직접 지정하세요", tier, req.Vendor)
	}
	if req.Host == "" {
		return "", fmt.Errorf("camera host is required")
	}
	return buildRTSPURL(req.Host, req.RTSPPort, path, req.Username, req.Password), nil
}

func (s *Server) cameraRTSPURL(ctx context.Context, p *storage.CameraProfile, tier string) (string, error) {
	if tier == "" {
		tier = "sub"
	}
	profile, _ := p.StreamProfiles[tier].(map[string]any)
	if profile == nil && tier != "main" {
		profile, _ = p.StreamProfiles["main"].(map[string]any)
	}
	if hint, _ := profile["url_hint"].(string); strings.HasPrefix(hint, "rtsp://") {
		if strings.Contains(hint, "@") {
			return "", errCredentialedRTSPURL
		}
		return hint, nil
	}
	path, _ := profile["path"].(string)
	if path == "" {
		path = defaultRTSPPath(p.Vendor, tier) // 벤더 기본 경로 fallback
	}
	if path == "" {
		return "", fmt.Errorf("stream profile %q 에 url_hint/path 없음 + vendor %q 기본 경로 미상", tier, p.Vendor)
	}
	if p.Host == "" {
		return "", fmt.Errorf("camera host is required")
	}
	password := ""
	if p.Username != "" {
		secretRef := p.PasswordSecretRef
		if secretRef == "" {
			secretRef = cameraSecretRef(p.CameraID)
		}
		pw, ok, err := s.store.KVGet(ctx, secretRef)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("camera password secret not found")
		}
		password = pw
	}
	return buildRTSPURL(p.Host, p.RTSPPort, path, p.Username, password), nil
}

func snapshotJPEG(parent context.Context, rtspURL string) ([]byte, error) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		return nil, fmt.Errorf("gst-launch-1.0 is not installed")
	}
	dir, err := os.MkdirTemp("", "bluei-camera-snapshot-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "snapshot.jpg")
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q",
		"rtspsrc", "location="+rtspURL, "latency=800", "protocols=tcp",
		"!", "decodebin",
		"!", "videoconvert",
		"!", "jpegenc", "snapshot=true", "quality=82",
		"!", "filesink", "location="+out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("snapshot pipeline failed: %s", msg)
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func cameraSecretRef(cameraID string) string { return "camera_secret:" + cameraID + ":password" }

type apiInputError string

func (e apiInputError) Error() string { return string(e) }

const errPlaintextCameraPassword apiInputError = "plaintext camera password is not accepted; use password_secret_ref"
const errCredentialedRTSPURL apiInputError = "credentialed RTSP URL is not accepted; store host/path and password_secret_ref separately"

func errRequired(field string) error { return apiInputError(field + " is required") }

func errInvalidEnum(field, value string) error {
	return apiInputError(field + ` has invalid value "` + value + `"`)
}
