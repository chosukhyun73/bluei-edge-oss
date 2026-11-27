package tempcontrol

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

// 알림 저장만 흉내내는 최소 store — 이 테스트가 보는 것은 raise/clear 호출뿐이다.
type alertRecorder struct {
	storage.Store
	upserted []*storage.OpenAlert
	cleared  []string
}

func (a *alertRecorder) UpsertAlert(_ context.Context, row *storage.OpenAlert) (bool, error) {
	a.upserted = append(a.upserted, row)
	return true, nil
}
func (a *alertRecorder) ClearAlert(_ context.Context, key string) error {
	a.cleared = append(a.cleared, key)
	return nil
}

func newStaleWorker(staleSec int) (*Worker, *alertRecorder) {
	rec := &alertRecorder{}
	w := NewWorker(rec, config.TempControlConfig{}, 0)
	w.SetSensorStaleSec(staleSec)
	return w, rec
}

func rfc(ago time.Duration) string {
	return time.Now().Add(-ago).UTC().Format(time.RFC3339)
}

// 관측이 임계를 넘으면 알림. 8-28 과냉각 사고의 재발 방지선.
func TestStaleRaisesAlert(t *testing.T) {
	w, rec := newStaleWorker(300)
	w.checkTempStaleness(context.Background(), rfc(30*time.Minute), true)
	if len(rec.upserted) != 1 {
		t.Fatalf("알림 %d건 — 1건이어야 한다", len(rec.upserted))
	}
	got := rec.upserted[0]
	if got.Severity != "critical" {
		t.Errorf("severity=%s — 수온은 어체 생존에 직결된다", got.Severity)
	}
	if got.AlertDedupeKey != staleAlertDedupeKey {
		t.Errorf("dedupe key=%s", got.AlertDedupeKey)
	}
}

// 관측이 최신이면 알림을 올리지 않고, 열려 있던 알림을 내린다.
func TestFreshClearsAlert(t *testing.T) {
	w, rec := newStaleWorker(300)
	w.checkTempStaleness(context.Background(), rfc(10*time.Second), true)
	if len(rec.upserted) != 0 {
		t.Errorf("정상인데 알림을 올렸다 (%d건)", len(rec.upserted))
	}
	if len(rec.cleared) != 1 {
		t.Errorf("회복 시 알림을 내리지 않았다")
	}
}

// 관측값 자체가 없는 경우도 알림 대상이다.
func TestNoReadingRaisesAlert(t *testing.T) {
	w, rec := newStaleWorker(300)
	w.checkTempStaleness(context.Background(), "", false)
	if len(rec.upserted) != 1 {
		t.Fatalf("관측값 없음인데 알림 %d건", len(rec.upserted))
	}
}

// 관측 시각을 못 읽으면 안전 측(알림)으로 간다.
func TestUnparsableObservedAtRaisesAlert(t *testing.T) {
	w, rec := newStaleWorker(300)
	w.checkTempStaleness(context.Background(), "not-a-time", true)
	if len(rec.upserted) != 1 {
		t.Fatalf("해석 불가인데 알림 %d건", len(rec.upserted))
	}
}

// 임계 미설정 시 기본 300초 — arbiter 안전 게이트 기본값과 같아야 한다.
func TestDefaultStaleCutoff(t *testing.T) {
	w, _ := newStaleWorker(0)
	if got := w.staleCutoff(); got != 300*time.Second {
		t.Errorf("기본 임계 = %v, want 5m", got)
	}
}
