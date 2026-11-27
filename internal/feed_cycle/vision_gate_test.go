package feed_cycle

import "testing"

// 게이트 미설정이면 종전 동작 — 절대 막지 않는다.
func TestVisionBlockedNoGate(t *testing.T) {
	w := &Worker{}
	if blind, _ := w.visionBlocked("tank_01"); blind {
		t.Error("게이트 미설정인데 급이를 막았다")
	}
}

func TestVisionBlockedPasses(t *testing.T) {
	w := &Worker{}
	w.SetVisionGate(func(string) (bool, string) { return true, "" })
	if blind, _ := w.visionBlocked("tank_01"); blind {
		t.Error("카메라 정상인데 급이를 막았다")
	}
}

func TestVisionBlockedStops(t *testing.T) {
	w := &Worker{}
	w.SetVisionGate(func(string) (bool, string) {
		return false, "cam_top_7_1_2_L 마지막 프레임 240초 전"
	})
	blind, detail := w.visionBlocked("tank_01")
	if !blind {
		t.Fatal("카메라가 끊겼는데 급이를 막지 않았다")
	}
	if detail != "cam_top_7_1_2_L 마지막 프레임 240초 전" {
		t.Errorf("사유가 전달되지 않았다: %q", detail)
	}
}

// 사유가 비어 있어도 차단은 유지되고 기본 문구가 채워진다
// (알림 메시지가 빈 괄호로 나가지 않도록).
func TestVisionBlockedFillsEmptyDetail(t *testing.T) {
	w := &Worker{}
	w.SetVisionGate(func(string) (bool, string) { return false, "" })
	blind, detail := w.visionBlocked("tank_01")
	if !blind {
		t.Fatal("차단되어야 한다")
	}
	if detail == "" {
		t.Error("기본 사유 문구가 채워지지 않았다")
	}
}

// 수조별로 독립 판정되어야 한다 — 한 수조가 막혀도 다른 수조는 급이가 나간다.
func TestVisionGateIsPerTank(t *testing.T) {
	w := &Worker{}
	w.SetVisionGate(func(tankID string) (bool, string) {
		return tankID != "tank_01", "tank_01 상단 카메라 단절"
	})
	if blind, _ := w.visionBlocked("tank_01"); !blind {
		t.Error("tank_01 은 막혔어야 한다")
	}
	if blind, _ := w.visionBlocked("tank_02"); blind {
		t.Error("tank_02 까지 막았다 — 수조별 판정이 아니다")
	}
}

// safety_block 과 다른 사유여야 한다. 같은 값이면 연속 2회 시 수조가
// manual 로 내려가(safety_escalation) 카메라 복구 후에도 급이가 안 나간다.
func TestVisionBlackoutReasonIsDistinct(t *testing.T) {
	if ReasonVisionBlackout == ReasonSafetyBlock {
		t.Fatal("vision_blackout 이 safety_block 과 같다 — 급이 일시정지가 오발동한다")
	}
	if ReasonVisionBlackout != "vision_blackout" {
		t.Errorf("사유 문자열 = %q", ReasonVisionBlackout)
	}
}
