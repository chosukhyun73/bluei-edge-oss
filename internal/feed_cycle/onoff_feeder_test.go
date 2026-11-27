package feed_cycle

import (
	"testing"
	"time"

	"bluei.kr/edge/internal/onoff_feeder"
)

// TestFeederActuateTimeout — stop 은 run 보다 짧아야 한다.
// 2026-08-18 tank_02 p7: 공통 8초 타임아웃 탓에 stop 이 8초를 다 쓰고 실패해
// 모터가 최대 29.5초 돌았다(설계 13.2초). stop 이 늦으면 그동안 모터가 돈다.
func TestFeederActuateTimeout(t *testing.T) {
	stop := feederActuateTimeout("feeder.stop")
	run := feederActuateTimeout("feeder.run")
	// 실측 246발: stop ACK 지연 P99 0.540초 → 네트워크 예산 1초(P99 의 약 2배).
	// 2026-10-02 부터 여기에 게이트 최소간격을 더한다 — 큐 대기가 예산을 먹어
	// stop 이 네트워크에 나가지도 못하던 것을 고쳤다(아래 전용 테스트 참조).
	if want := onoff_feeder.DefaultMinInterval + feederStopNetworkBudget; stop != want {
		t.Errorf("stop 타임아웃 = %v, want %v", stop, want)
	}
	if run != 8*time.Second {
		t.Errorf("run 타임아웃 = %v, want 8s", run)
	}
	if stop >= run {
		t.Errorf("stop(%v) 은 run(%v) 보다 짧아야 한다 — 늦은 stop 은 모터 초과 회전", stop, run)
	}
}

// TestFeederStopQuickRetry_Budget — 즉시 재전송은 "같은 시간 안에 더 많이 시도하되,
// 포기는 종전보다 늦어지지 않아야" 한다. 두 조건이 함께 걸려야 의미가 있다.
//
//	종전 경로   t=0 시도(8초 타임아웃) → gap +5초 → t=13 에 2번째 시도
//
// 재전송을 늘리면 시도 횟수는 늘지만 실패 확정이 밀려 critical 알림이 늦어진다.
// 이 테스트가 그 균형을 고정한다 (2026-08-18 설계 중 3회안이 여기서 걸렸다).
func TestFeederStopQuickRetry_Budget(t *testing.T) {
	const legacyWindow = 13 * time.Second // 종전에 2번째 시도가 나가던 시점
	perAttempt := feederActuateTimeout("feeder.stop") + feederStopQuickRetryGap
	attempts := feederStopQuickRetries + 1

	if total := perAttempt * time.Duration(attempts); total > legacyWindow {
		t.Errorf("재전송 총 예산 %v > 종전 %v — 실패 확정이 밀려 알림이 늦어진다", total, legacyWindow)
	}
	if attempts < 3 {
		t.Errorf("시도 %d회 — 종전(13초 안에 2회)보다 많아야 개선이다", attempts)
	}
	// 알림 지연 회귀 방지: 1회 호출이 6번 시도로 늘었으므로 임계를 함께 낮춰야
	// critical 알림이 종전(21초)보다 밀리지 않는다. 2026-08-18 에 여기서 걸렸다.
	burst := perAttempt * time.Duration(attempts)
	if alertAt := burst * time.Duration(feederStopFailureThreshold); alertAt > 21*time.Second {
		t.Errorf("연속실패 알림까지 %v > 종전 21초 — 임계(%d)를 낮춰야 한다",
			alertAt, feederStopFailureThreshold)
	}
	if feederStopQuickRetryGap < 1*time.Second {
		t.Errorf("재전송 간격 %v — onoff_feeder 1req/s 를 위반한다", feederStopQuickRetryGap)
	}
	if perAttempt >= 8*time.Second {
		t.Errorf("2번째 시도가 %v 뒤 — 종전 8초 타임아웃보다 이르지 않다", perAttempt)
	}
}

// stop 타임아웃은 onoff_feeder 전역 게이트의 최소간격보다 길어야 한다.
// 짧으면 stop 이 네트워크에 나가보지도 못하고 우리 큐에서 죽는다 — 2026-10-02 에
// tank_02 사이클이 그 이유로 safety_block 됐다(급이기는 실제로 멈춰 있었음).
// 두 상수가 다른 패키지에 있어 각각 손으로 바뀌면 또 어긋나므로 여기서 묶어 지킨다.
func TestFeederStopTimeoutExceedsOnoffFeederGateInterval(t *testing.T) {
	stop := feederActuateTimeout("feeder.stop")
	if stop <= onoff_feeder.DefaultMinInterval {
		t.Fatalf("stop 타임아웃 %v 가 게이트 최소간격 %v 이하다 — 큐 대기만으로 항상 실패한다",
			stop, onoff_feeder.DefaultMinInterval)
	}
	// 네트워크 왕복 여유가 실제로 남아야 한다 (간격만 겨우 넘기면 ACK 를 못 받는다).
	if got := stop - onoff_feeder.DefaultMinInterval; got < feederStopNetworkBudget {
		t.Fatalf("네트워크 예산 %v 가 기대치 %v 미만", got, feederStopNetworkBudget)
	}
	// run 은 종전대로 — 늦어도 급이가 늦어질 뿐이라 넉넉히 둔다.
	if run := feederActuateTimeout("feeder.run"); run != 8*time.Second {
		t.Fatalf("run 타임아웃이 바뀌었다: %v", run)
	}
}
