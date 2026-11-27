package feed_cycle

import (
	"testing"

	"bluei.kr/edge/internal/config"
)

// WP-F — onoff_feeder 마이크로펄스 파라미터를 회당 목표량에서 도출하는지 검증.
// run 1s(≈9.4g) → stop 2s 반복으로 목표를 채운다(한 번에 다 주면 못 먹고 침전).
func TestDeriveOnoffFeederPulseParams(t *testing.T) {
	// onoff_feeder adaptive, 목표 100g, run 1s / stop 2s, 배출률 9.4g/s
	c := NewAdaptiveCycle("cyc", "tank_02", "feeder_tank_02", AdaptiveParams{TargetAmountG: 100})
	if !c.DeriveOnoffFeederPulseParams(1, 2, 9.4) {
		t.Fatal("expected derive applied")
	}
	if got := c.adaptive.PulseDurationMs; got != 1000 { // run 1s
		t.Errorf("PulseDurationMs=%d, want 1000", got)
	}
	if got := c.adaptive.GapMs; got != 2000 { // stop 2s
		t.Errorf("GapMs=%d, want 2000", got)
	}
	if got := c.adaptive.MaxPulses; got != 12 { // ceil(100/9.4)+1 = 11+1
		t.Errorf("MaxPulses=%d, want 12", got)
	}
	// 핵심: max_pulses × 버스트량(9.4) ≥ 목표 (조기 멈춤 없이 목표 도달 가능)
	if float64(c.adaptive.MaxPulses)*9.4 < 100 {
		t.Error("max_pulses too low to reach target — 조기 멈춤 버그 재발")
	}

	// 운영자가 펄스 수동 지정(PulseDurationMs>0) → 도출하지 않고 존중
	c2 := NewAdaptiveCycle("cyc2", "tank_02", "f", AdaptiveParams{TargetAmountG: 100, PulseDurationMs: 1000})
	if c2.DeriveOnoffFeederPulseParams(1, 2, 9.4) {
		t.Error("manual pulse duration should not be overridden")
	}

	// 목표 없음 → 도출 안 함
	c3 := NewAdaptiveCycle("cyc3", "tank_02", "f", AdaptiveParams{})
	if c3.DeriveOnoffFeederPulseParams(1, 2, 9.4) {
		t.Error("no target should not derive")
	}

	// fixed 모드 → 도출 안 함
	c4 := NewFixedCycle("cyc4", "tank_02", "f", FixedParams{PulseDurationMs: 1000, GapMs: 2000, TotalPulses: 5})
	if c4.DeriveOnoffFeederPulseParams(1, 2, 9.4) {
		t.Error("fixed mode should not derive")
	}
}

// 마지막 펄스 트림 — remainingToTargetG 가 누적 배출을 반영해 잔여 목표량을 반환하는지,
// 그리고 non-adaptive / 목표 도달 시 0 을 반환하는지 검증(worker 트림 판단의 근거).
func TestRemainingToTargetG(t *testing.T) {
	c := NewAdaptiveCycle("cyc", "tank_02", "feeder_tank_02", AdaptiveParams{TargetAmountG: 100})
	if got := c.remainingToTargetG(); got != 100 {
		t.Errorf("초기 remaining=%v, want 100", got)
	}
	// 펄스 3회(각 28.2g) 기록 → 84.6g 누적, 잔여 15.4g (< 한 펄스량 → 트림 대상).
	for i := 0; i < 3; i++ {
		c.recordPulseResult(28.2, 28.2)
	}
	if got := c.remainingToTargetG(); got < 15.3 || got > 15.5 {
		t.Errorf("3펄스 후 remaining=%v, want ~15.4", got)
	}
	// 목표 초과 → 0 (음수 아님).
	c.recordPulseResult(28.2, 28.2) // 112.8g > 100
	if got := c.remainingToTargetG(); got != 0 {
		t.Errorf("목표 초과 시 remaining=%v, want 0", got)
	}
	// fixed 모드 → 목표 개념 없음 → 0.
	cf := NewFixedCycle("cf", "tank_02", "f", FixedParams{PulseDurationMs: 1000, GapMs: 2000, TotalPulses: 5})
	if got := cf.remainingToTargetG(); got != 0 {
		t.Errorf("fixed remaining=%v, want 0", got)
	}
}

// 공급모터 리드타임 시맨틱: 0 → 기본 10s, -1 → 리드 없음, >0 → 지정값.
func TestFeederSpinupSecSemantics(t *testing.T) {
	mk := func(v int) *Worker {
		return &Worker{cfg: config.FeedCycleConfig{FeederSpinupSec: v}}
	}
	if got := mk(0).feederSpinupSec(); got != 10 {
		t.Errorf("spinup(0) = %d, want 10 (default)", got)
	}
	if got := mk(-1).feederSpinupSec(); got != 0 {
		t.Errorf("spinup(-1) = %d, want 0 (disabled)", got)
	}
	if got := mk(8).feederSpinupSec(); got != 8 {
		t.Errorf("spinup(8) = %d, want 8", got)
	}
}

// TestOverrunDoesNotShortenCycle — 초과 회전은 장부에만 들어가고 발수를 줄이면 안 된다.
// 2026-08-19 실측: 정상 펄스도 매번 92~379ms 초과로 잡혀 8발 계획이 7발에서 끝났고
// 실제 급이가 목표보다 17g 적었다. onoff_feeder API 응답 지연이 어체 급이량을 정하는 셈이었다.
func TestOverrunDoesNotShortenCycle(t *testing.T) {
	c := NewFixedCycle("t", "tank_01", "", FixedParams{
		PulseDurationMs: 3000, GapMs: 1000, TotalPulses: 8, TargetAmountG: 214,
	})
	// 설계 28.2g/발인데 초과 회전으로 장부는 30.0g 씩 쌓인다고 가정.
	for i := 0; i < 7; i++ {
		c.recordPulseResult(30.0, 28.2)
	}
	// 장부(평가)는 초과를 포함해 210g — 운영자 지시대로 실측 기반으로 남는다.
	if got := c.TotalAmountG(); got < 209 || got > 211 {
		t.Errorf("장부 = %.1fg, want ≈210 (초과 포함)", got)
	}
	// 제어는 설계값 197.4g 이라 목표(214g) 미달 — 8발째가 나가야 한다.
	if c.isTargetReached() {
		t.Error("7발에서 목표 도달로 판정됐다 — 초과 회전이 발수를 줄이고 있다")
	}
	// 트림 크기도 설계값 기준이어야 실제 급이가 목표를 채운다.
	if rem := c.remainingToTargetG(); rem < 16 || rem > 17 {
		t.Errorf("잔여 = %.1fg, want ≈16.6 (214 − 197.4). 장부 기준이면 4g 로 잘린다", rem)
	}
}

// TestLoadCellOverridesBoth — 로드셀 실측이 오면 추정(평가·제어) 양쪽을 대체한다.
func TestLoadCellOverridesBoth(t *testing.T) {
	c := NewFixedCycle("t", "tank_01", "", FixedParams{
		PulseDurationMs: 3000, GapMs: 1000, TotalPulses: 8, TargetAmountG: 100,
	})
	c.recordPulseResult(30.0, 28.2)
	c.actualTotalAmountG = 120 // 실측이 목표 초과
	if !c.isTargetReached() {
		t.Error("로드셀 실측 120g > 목표 100g 인데 도달로 안 봤다")
	}
}
