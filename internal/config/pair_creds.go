package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PairCreds — GX10 페어링 자격증명. bluei 앱이 스캔하는 QR과 클라우드 sync가 같은
// access_token을 쓰도록, <data_dir>/pair_credentials.json에 한 번 생성·영속한다.
type PairCreds struct {
	NodeCode    string `json:"node_code"`
	AccessToken string `json:"access_token"`
}

const pairCredsFile = "pair_credentials.json"

// EnsurePairCreds: creds 파일 로드. 없거나 비면 node_code+access_token을 생성해
// 파일(0600)에 영속하고 반환.
func EnsurePairCreds(dataDir string) (PairCreds, error) {
	if dataDir == "" {
		dataDir = "."
	}
	path := filepath.Join(dataDir, pairCredsFile)
	if b, err := os.ReadFile(path); err == nil {
		var c PairCreds
		if json.Unmarshal(b, &c) == nil && c.AccessToken != "" && c.NodeCode != "" {
			return c, nil
		}
	}
	c := PairCreds{
		NodeCode:    "gx10-" + randHex(4),
		AccessToken: "gx10_" + randToken(32),
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return c, err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return c, fmt.Errorf("write pair creds: %w", err)
	}
	return c, nil
}

// ApplyPairCreds: cfg.Sync의 node_code/access_token이 비어있으면 creds 파일에서
// 채운다(없으면 생성). sync 레이어와 /v1/pair가 같은 토큰을 공유하도록 보장한다.
func ApplyPairCreds(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if cfg.Sync.AccessToken != "" && cfg.Sync.NodeCode != "" {
		return nil
	}
	c, err := EnsurePairCreds(cfg.Edge.DataDir)
	if err != nil {
		return err
	}
	if cfg.Sync.NodeCode == "" {
		cfg.Sync.NodeCode = c.NodeCode
	}
	if cfg.Sync.AccessToken == "" {
		cfg.Sync.AccessToken = c.AccessToken
	}
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
