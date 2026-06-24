package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// CameraSeed mirrors the on-disk shape of configs/cameras.yaml.
// 시드 용도라 boot 시 한 번만 읽고 storage.CameraProfile 로 매핑.
// password_secret_ref 만 허용 — plaintext password / credentialed RTSP URL 은 거부.
//
// storage 패키지를 직접 import 하면 internal/storage → internal/config import
// cycle 이 발생하므로 여기서는 단순 자료구조로만 반환.
type CameraSeed struct {
	CameraID          string         `yaml:"camera_id"`
	TankID            string         `yaml:"tank_id"`
	DisplayName       string         `yaml:"display_name"`
	Vendor            string         `yaml:"vendor"`
	Host              string         `yaml:"host"`
	RTSPPort          int            `yaml:"rtsp_port"`
	HTTPPort          int            `yaml:"http_port"`
	Username          string         `yaml:"username"`
	PasswordSecretRef string         `yaml:"password_secret_ref"`
	Position          string         `yaml:"position"`
	Purpose           []string       `yaml:"purpose"`
	StreamProfiles    map[string]any `yaml:"stream_profiles"`
	ClipPolicy        map[string]any `yaml:"clip_policy"`
	Status            string         `yaml:"status"`
	Metadata          map[string]any `yaml:"metadata"`
}

type camerasFile struct {
	Cameras []CameraSeed `yaml:"cameras"`
}

// LoadCameras reads cameras.yaml and returns CameraSeed entries ready to map+upsert.
// 빈 경로 / 파일 없음 → nil, nil (선택 시드).
func LoadCameras(path string) ([]CameraSeed, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cameras config %s: %w", path, err)
	}
	var file camerasFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse cameras config %s: %w", path, err)
	}
	for i, c := range file.Cameras {
		if c.CameraID == "" {
			return nil, fmt.Errorf("camera[%d]: camera_id is required", i)
		}
		if c.DisplayName == "" {
			return nil, fmt.Errorf("camera %s: display_name is required", c.CameraID)
		}
		if c.TankID == "" {
			return nil, fmt.Errorf("camera %s: tank_id is required", c.CameraID)
		}
		if c.Status == "" {
			file.Cameras[i].Status = "configured"
		}
		if c.Purpose == nil {
			file.Cameras[i].Purpose = []string{"operator_view", "vision_ai"}
		}
		if c.StreamProfiles == nil {
			file.Cameras[i].StreamProfiles = map[string]any{}
		}
		if c.ClipPolicy == nil {
			file.Cameras[i].ClipPolicy = map[string]any{}
		}
		if c.Metadata == nil {
			file.Cameras[i].Metadata = map[string]any{}
		}
	}
	return file.Cameras, nil
}
