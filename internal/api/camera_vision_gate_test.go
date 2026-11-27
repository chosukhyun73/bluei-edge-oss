package api

import (
	"testing"
	"time"

	"bluei.kr/edge/internal/media"
)

func newGateWorker(cameraID string, realFrameAgo time.Duration, startedAgo time.Duration) *cameraJPEGWorker {
	w := &cameraJPEGWorker{
		CameraID: cameraID,
		Tier:     "sub",
		Hub:      media.NewFrameHub(2),
		started:  time.Now().Add(-startedAgo).UTC(),
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
	}
	if realFrameAgo >= 0 {
		w.lastRealFrameUnix.Store(time.Now().Add(-realFrameAgo).Unix())
	}
	// 재연결 루프가 계속 재장전하는 값 — 게이트는 이걸 보면 안 된다.
	w.lastFrameUnix.Store(time.Now().Unix())
	return w
}

// 상류가 죽어 재연결만 도는 상황: lastFrameUnix 는 방금이지만 실제 프레임은 오래 전.
// 게이트는 반드시 차단해야 한다 (2026-08-27 실사고).
func TestVisionGateIgnoresPipelineRestartTimestamp(t *testing.T) {
	w := newGateWorker("cam_top_x", 10*time.Minute, time.Hour)
	if age := time.Now().Unix() - w.lastFrameUnix.Load(); age > 5 {
		t.Fatalf("전제 오류: lastFrameUnix 가 최신이어야 한다 (age=%d)", age)
	}
	last := w.lastRealFrameUnix.Load()
	if got := time.Now().Unix() - last; got < int64(tankVisionMaxFrameAge/time.Second) {
		t.Errorf("실제 프레임 경과 %d초 — 임계(%v)를 넘겨야 차단된다", got, tankVisionMaxFrameAge)
	}
}

// 방금 뜬 워커는 아직 프레임이 없어도 유예를 받는다 (막지 않는 방향).
func TestVisionGateGraceForFreshWorker(t *testing.T) {
	w := newGateWorker("cam_top_x", -1, 5*time.Second) // realFrameAgo<0 → 한 장도 못 받음
	if w.lastRealFrameUnix.Load() != 0 {
		t.Fatal("전제 오류: 프레임 미수신이어야 한다")
	}
	age := time.Now().Unix() - w.started.Unix()
	if age > int64(tankVisionMaxFrameAge/time.Second) {
		t.Errorf("방금 뜬 워커가 유예를 못 받는다 (age=%d)", age)
	}
}

// 유예가 지나도 프레임이 없으면 차단 대상이다.
func TestVisionGateBlocksWorkerThatNeverGotFrames(t *testing.T) {
	w := newGateWorker("cam_top_x", -1, 10*time.Minute)
	age := time.Now().Unix() - w.started.Unix()
	if age <= int64(tankVisionMaxFrameAge/time.Second) {
		t.Errorf("한 장도 못 받은 채 %d초 지났으면 차단되어야 한다", age)
	}
}
