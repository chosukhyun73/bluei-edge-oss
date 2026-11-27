package api

import (
	"testing"
	"time"

	"bluei.kr/edge/internal/media"
)

// newTestHubWorker — 레지스트리에 넣을 최소 워커. 파이프라인은 띄우지 않는다
// (reapIdleCameraHubs 는 프로세스를 직접 만지지 않고 stop 만 닫는다).
func newTestHubWorker(cameraID, tankID string, idle time.Duration) *cameraJPEGWorker {
	w := &cameraJPEGWorker{
		CameraID: cameraID,
		TankID:   tankID,
		Tier:     "sub",
		Hub:      media.NewFrameHub(2),
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
	}
	w.lastAccessUnix.Store(time.Now().Add(-idle).Unix())
	return w
}

func withWorkerRegistry(t *testing.T, items map[string]*cameraJPEGWorker) {
	t.Helper()
	cameraJPEGWorkers.Lock()
	prev := cameraJPEGWorkers.items
	cameraJPEGWorkers.items = items
	cameraJPEGWorkers.Unlock()
	t.Cleanup(func() {
		cameraJPEGWorkers.Lock()
		cameraJPEGWorkers.items = prev
		cameraJPEGWorkers.Unlock()
	})
}

// 유휴 워커만 회수하고, 관찰 중인 워커는 남긴다.
// 출하된 수조(tank_03)의 워커가 안 내려가 상류 연결을 물고 있던 것이 2026-08-22 실측.
func TestReapIdleCameraHubs(t *testing.T) {
	idle := newTestHubWorker("cam_top_7_3_4_L", "tank_03", cameraHubIdleTimeout+time.Minute)
	fresh := newTestHubWorker("cam_top_7_1_2_L", "tank_01", 0)
	viewed := newTestHubWorker("cam_underwater_7_1", "tank_01", cameraHubIdleTimeout+time.Minute)
	_, unsubscribe := viewed.Hub.Subscribe()
	defer unsubscribe()

	withWorkerRegistry(t, map[string]*cameraJPEGWorker{
		"idle:sub":   idle,
		"fresh:sub":  fresh,
		"viewed:sub": viewed,
	})

	reapIdleCameraHubs()

	if !stopRequested(idle) {
		t.Error("유휴 워커가 회수되지 않았다")
	}
	if stopRequested(fresh) {
		t.Error("keepalive 가 방금 ensure 한 워커를 회수했다 — 관찰 대상이 끊긴다")
	}
	if stopRequested(viewed) {
		t.Error("뷰어가 붙어 있는 워커를 회수했다")
	}

	cameraJPEGWorkers.Lock()
	defer cameraJPEGWorkers.Unlock()
	if _, ok := cameraJPEGWorkers.items["idle:sub"]; ok {
		t.Error("회수한 워커가 레지스트리에 남아 있다")
	}
	if len(cameraJPEGWorkers.items) != 2 {
		t.Errorf("남은 워커 수 = %d, want 2", len(cameraJPEGWorkers.items))
	}
}

// 이미 리턴한 워커(done 닫힘)는 stop 을 닫지 않고 레지스트리에서만 지운다.
// close 를 두 번 하면 panic 이므로 이 경로가 갈라져 있어야 한다.
func TestReapIdleCameraHubsDropsDeadWorker(t *testing.T) {
	dead := newTestHubWorker("cam_dead", "tank_01", cameraHubIdleTimeout+time.Minute)
	close(dead.done)
	withWorkerRegistry(t, map[string]*cameraJPEGWorker{"dead:sub": dead})

	reapIdleCameraHubs()

	if stopRequested(dead) {
		t.Error("이미 끝난 워커의 stop 을 닫았다")
	}
	cameraJPEGWorkers.Lock()
	defer cameraJPEGWorkers.Unlock()
	if len(cameraJPEGWorkers.items) != 0 {
		t.Error("죽은 워커가 레지스트리에 남아 있다")
	}
}

// 두 번 돌아도 안전해야 한다 — 회수된 워커는 이미 map 에서 빠져 close 가 재실행되지 않는다.
func TestReapIdleCameraHubsIsIdempotent(t *testing.T) {
	idle := newTestHubWorker("cam_idle", "tank_04", cameraHubIdleTimeout+time.Minute)
	withWorkerRegistry(t, map[string]*cameraJPEGWorker{"idle:sub": idle})

	reapIdleCameraHubs()
	reapIdleCameraHubs() // close 중복이면 여기서 panic

	if !stopRequested(idle) {
		t.Error("유휴 워커가 회수되지 않았다")
	}
}
