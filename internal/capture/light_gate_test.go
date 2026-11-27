package capture

import (
	"log/slog"
	"testing"
	"time"
)

// 실조도 야간 휴면 상태 전이 — 어두움 연속 2회 → 휴면, 프로브 주기, 점등 복귀.
func TestLightGate_Transitions(t *testing.T) {
	w := &Worker{log: slog.Default()}
	tank := "tank_01"
	now := time.Now()

	// 깨어 있음 — 밝음: 배포
	if !w.lightGateDispatch(tank, 120, true) {
		t.Fatal("bright: expected dispatch")
	}
	// 어두움 1회째: 아직 배포 (streak 1 < 2)
	if !w.lightGateDispatch(tank, 20, true) {
		t.Fatal("dark #1: expected dispatch (streak not reached)")
	}
	if w.darkSleepShouldSkip(tank, now) {
		t.Fatal("dark #1: should not sleep yet")
	}
	// 어두움 2회째: 휴면 진입 → 폐기
	if w.lightGateDispatch(tank, 18, true) {
		t.Fatal("dark #2: expected discard (sleeping)")
	}
	// 휴면 직후 — 프로브 주기 전이므로 캡처 자체 skip
	if !w.darkSleepShouldSkip(tank, time.Now()) {
		t.Fatal("sleeping: expected skip before probe interval")
	}
	// 프로브 주기 경과 — 캡처 허용 (프로브)
	w.lightMu.Lock()
	w.light[tank].lastProbe = time.Now().Add(-captureProbeEvery - time.Second)
	w.lightMu.Unlock()
	if w.darkSleepShouldSkip(tank, time.Now()) {
		t.Fatal("probe due: expected capture allowed")
	}
	// 프로브가 여전히 어두움 → 계속 폐기
	if w.lightGateDispatch(tank, 15, true) {
		t.Fatal("probe dark: expected discard")
	}
	// 중간 대역(35~50) — 휴면 유지 (히스테리시스)
	if w.lightGateDispatch(tank, 42, true) {
		t.Fatal("mid band while sleeping: expected discard")
	}
	// 점등 감지 → 즉시 복귀 + 배포
	if !w.lightGateDispatch(tank, 90, true) {
		t.Fatal("bright probe: expected resume + dispatch")
	}
	if w.darkSleepShouldSkip(tank, time.Now()) {
		t.Fatal("resumed: should not skip")
	}
	// 측정 실패(fail-open) — 깨어 있으면 배포
	if !w.lightGateDispatch(tank, 0, false) {
		t.Fatal("luma unknown while awake: expected dispatch")
	}
	// 중간 대역 — 깨어 있으면 배포 유지
	if !w.lightGateDispatch(tank, 42, true) {
		t.Fatal("mid band while awake: expected dispatch")
	}
}
