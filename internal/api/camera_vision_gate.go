package api

import (
	"context"
	"fmt"
	"time"
)

// tankVisionMaxFrameAge — 이 시간 동안 프레임이 없으면 "눈이 없다"로 본다.
//
// 스냅샷 신선도(5초)보다 훨씬 길게 잡았다. 5초로 잡으면 재연결 백오프(최대 15초)나
// 키프레임 대기 같은 정상 순단에도 급이가 멈춘다. 60초를 넘겼다는 것은 워치독
// (jpegFrameGapTimeout 30초)이 한 번 파이프라인을 죽이고 재연결에도 실패했다는 뜻이라
// 순단이 아니라 단절이다.
const tankVisionMaxFrameAge = 60 * time.Second

// TankVisionGate — 이 수조의 **판정 카메라**(상단)가 지금 프레임을 주고 있는가.
// feed_cycle.VisionGate 로 주입된다 (main.go).
//
// 상단만 보는 이유: 배출·중단 판정이 상단 곡선만 쓴다(shadow_stop.go 의
// segmentChartValue 가 수중 카메라를 건너뛴다). 수중이 끊겨도 "먹는지 안 먹는지"는
// 여전히 보이므로 급이를 막지 않는다 — 2026-08-25 운영자 결정.
//
// 판정 불가일 때는 통과시킨다. 알 수 없다는 이유로 어체를 굶기지 않는다.
func (s *Server) TankVisionGate(tankID string) (bool, string) {
	profiles, err := s.store.ListCameraProfiles(context.Background(), tankID)
	if err != nil {
		return true, "" // 조회 실패로 급이를 막지 않는다
	}
	now := time.Now().Unix()
	maxAgeSec := int64(tankVisionMaxFrameAge / time.Second)
	checked := 0
	for _, p := range profiles {
		if p.Position != "top" {
			continue
		}
		checked++
		cameraJPEGWorkers.Lock()
		worker := cameraJPEGWorkers.items[p.CameraID+":sub"]
		cameraJPEGWorkers.Unlock()
		if worker == nil || !workerAlive(worker) {
			return false, fmt.Sprintf("%s 프레임허브 없음", p.CameraID)
		}
		// ⚠ lastFrameUnix 가 아니라 lastRealFrameUnix 를 본다. 전자는 파이프라인을
		// 새로 띄울 때마다 재장전되어 상류 단절 중에도 계속 갱신된다(2026-08-27 실측:
		// 카메라 5대 전부 504 인데 lastFrameUnix 는 1~18초 전).
		last := worker.lastRealFrameUnix.Load()
		if last == 0 {
			// 아직 한 장도 못 받은 워커 — 방금 뜬 것일 수 있으므로 생성 시각을 기준으로
			// 같은 유예를 준다. 유예가 지나도 프레임이 없으면 눈이 없는 것이다.
			last = worker.started.Unix()
		}
		if age := now - last; age > maxAgeSec {
			return false, fmt.Sprintf("%s 마지막 프레임 %d초 전", p.CameraID, age)
		}
	}
	if checked == 0 {
		return true, "" // 상단 카메라가 없는 수조는 게이트 대상이 아니다
	}
	return true, ""
}
