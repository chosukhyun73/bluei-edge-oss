// Package vision은 Python vision 서비스와 UNIX 소켓으로 통신하는 클라이언트입니다.
// vision 서비스가 실행 중이지 않으면 안전하게 no-op 동작합니다.
package vision

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const defaultSocket = "/tmp/bluei-vision.sock"

// TankState는 vision 서비스에서 받은 수조 상태입니다.
type TankState struct {
	HungerIndex float64 `json:"hunger_index"`
	FishCount   int     `json:"fish_count"`
	HungryRatio float64 `json:"hungry_ratio"`
	ShouldFeed  bool    `json:"should_feed"`
	Timestamp   float64 `json:"ts"`
}

// FeedRequest는 Go → Python으로 전달하는 급이 명령입니다.
type FeedRequest struct {
	Cmd       string  `json:"cmd"`
	Active    bool    `json:"active"`
	Intensity float64 `json:"intensity"`
}

// Client는 vision 서비스 클라이언트입니다.
type Client struct {
	socketPath string
	timeout    time.Duration
}

// NewClient는 기본 소켓 경로로 클라이언트를 생성합니다.
func NewClient() *Client {
	return &Client{
		socketPath: defaultSocket,
		timeout:    500 * time.Millisecond,
	}
}

// State는 현재 수조 상태를 조회합니다.
// vision 서비스가 없으면 빈 상태와 에러를 반환합니다.
func (c *Client) State() (TankState, error) {
	resp, err := c.call(map[string]any{"cmd": "state"})
	if err != nil {
		return TankState{}, err
	}
	var st TankState
	if err := json.Unmarshal(resp, &st); err != nil {
		return TankState{}, fmt.Errorf("vision: state 파싱 실패: %w", err)
	}
	return st, nil
}

// Feed는 급이 명령을 전송합니다.
func (c *Client) Feed(active bool, intensity float64) error {
	_, err := c.call(map[string]any{
		"cmd":       "feed",
		"active":    active,
		"intensity": intensity,
	})
	return err
}

func (c *Client) call(req map[string]any) ([]byte, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("vision: 소켓 연결 실패 (%s): %w", c.socketPath, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(c.timeout))

	payload, _ := json.Marshal(req)
	if _, err := conn.Write(payload); err != nil {
		return nil, fmt.Errorf("vision: 전송 실패: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("vision: 응답 읽기 실패: %w", err)
	}
	return buf[:n], nil
}
