package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DeviceCreds — device-auth(폰 승인) 흐름으로 발급된 GX10 device 토큰.
// 클라우드 POST /device/auth/poll 이 approved 시 1회 발급하는 node_code/access_token 을
// <data_dir>/device_credentials.json(0600)에 영속한다. pair_credentials.json(QR 부트스트랩)과
// 별개 — 이쪽은 "이 GX10이 어느 사용자 계정에 로그인되어 있는가"의 세션이다.
type DeviceCreds struct {
	NodeCode    string `json:"node_code"`
	AccessToken string `json:"access_token"`
	Endpoint    string `json:"endpoint,omitempty"`
	UserEmail   string `json:"user_email,omitempty"`
}

const deviceCredsFile = "device_credentials.json"

// LoadDeviceCreds: creds 파일 로드. 없거나 비면 zero value + error.
func LoadDeviceCreds(dataDir string) (DeviceCreds, error) {
	if dataDir == "" {
		dataDir = "."
	}
	var c DeviceCreds
	b, err := os.ReadFile(filepath.Join(dataDir, deviceCredsFile))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return DeviceCreds{}, err
	}
	return c, nil
}

// SaveDeviceCreds: device 토큰을 파일(0600)에 영속.
func SaveDeviceCreds(dataDir string, c DeviceCreds) error {
	if dataDir == "" {
		dataDir = "."
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(filepath.Join(dataDir, deviceCredsFile), b, 0o600); err != nil {
		return fmt.Errorf("write device creds: %w", err)
	}
	return nil
}

// ApplyDeviceCreds: device-auth로 받은 device 토큰이 있으면 sync 자격증명을 그 토큰으로
// override한다. device-auth 승인 시 백엔드가 노드의 token_hash를 device 토큰으로 갱신하므로,
// sync도 device 토큰으로 POST해야 인증된다(pair 토큰은 더 이상 매칭 안 됨). ApplyPairCreds 다음에 호출.
func ApplyDeviceCreds(cfg *Config) {
	if cfg == nil {
		return
	}
	c, err := LoadDeviceCreds(cfg.Edge.DataDir)
	if err != nil || c.AccessToken == "" {
		return
	}
	cfg.Sync.AccessToken = c.AccessToken
	if c.NodeCode != "" {
		cfg.Sync.NodeCode = c.NodeCode
	}
}
