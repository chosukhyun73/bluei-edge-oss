package vision_test

import (
	"testing"

	"bluei.kr/edge/internal/vision"
)

func TestClientNoService(t *testing.T) {
	c := vision.NewClient()
	_, err := c.State()
	// vision 서비스 미실행 시 에러 반환이 정상
	if err == nil {
		t.Log("vision 서비스가 실행 중 — 실제 상태 조회됨")
	} else {
		t.Logf("vision 서비스 없음 (예상된 동작): %v", err)
	}
}
