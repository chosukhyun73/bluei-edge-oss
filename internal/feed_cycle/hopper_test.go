package feed_cycle

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

type fakeHopperStore struct {
	refills  []*storage.Event
	feedings []*storage.Event
	recals   []*storage.Event
}

func (f *fakeHopperStore) QueryEvents(_ context.Context, fl storage.EventFilter) ([]*storage.Event, error) {
	switch fl.EventType {
	case events.EventFeederHopperRefilled:
		return f.refills, nil
	case events.EventFeedingRecorded:
		return f.feedings, nil
	case events.EventFeederHopperRecalibrated:
		return f.recals, nil
	}
	return nil, nil
}

func hopperEvt(seq int64, payload string) *storage.Event {
	return &storage.Event{Sequence: seq, RecordedAt: time.Now().UTC(), PayloadJSON: payload}
}

func TestEstimateHopperNoRefill(t *testing.T) {
	est, err := EstimateHopper(context.Background(), &fakeHopperStore{}, "tank_01")
	if err != nil {
		t.Fatal(err)
	}
	if est.RemainingG != nil {
		t.Fatalf("보충 기록 없으면 추정 불가여야 함, got %v", *est.RemainingG)
	}
}

func TestEstimateHopperLedger(t *testing.T) {
	fs := &fakeHopperStore{
		refills: []*storage.Event{
			hopperEvt(100, `{"amount_g":5000}`),
		},
		feedings: []*storage.Event{
			hopperEvt(50, `{"feed_amount_g":85}`),  // 보충 이전 — 무시
			hopperEvt(200, `{"feed_amount_g":85}`), // -85
			hopperEvt(300, `{"feed_amount_g":56.4}`),
		},
	}
	est, err := EstimateHopper(context.Background(), fs, "tank_01")
	if err != nil {
		t.Fatal(err)
	}
	if est.RemainingG == nil {
		t.Fatal("추정값이 있어야 함")
	}
	want := 5000 - 85 - 56.4
	if diff := *est.RemainingG - want; diff > 0.01 || diff < -0.01 {
		t.Fatalf("remaining = %v, want %v", *est.RemainingG, want)
	}
	if est.Low {
		t.Fatal("4858g 는 low 아님")
	}
}

func TestEstimateHopperClampAndLow(t *testing.T) {
	fs := &fakeHopperStore{
		refills: []*storage.Event{hopperEvt(100, `{"amount_g":100}`)},
		feedings: []*storage.Event{
			hopperEvt(200, `{"feed_amount_g":85}`),
			hopperEvt(300, `{"feed_amount_g":85}`), // 잔량 음수 → 0 클램프
		},
	}
	est, err := EstimateHopper(context.Background(), fs, "tank_01")
	if err != nil {
		t.Fatal(err)
	}
	if est.RemainingG == nil || *est.RemainingG != 0 {
		t.Fatalf("클램프 후 0 이어야 함, got %v", est.RemainingG)
	}
	if !est.Low {
		t.Fatal("0g 는 low 여야 함")
	}
}

func TestEstimateHopperRefillResetsConsumed(t *testing.T) {
	fs := &fakeHopperStore{
		refills: []*storage.Event{
			hopperEvt(100, `{"amount_g":1000}`),
			hopperEvt(400, `{"amount_g":2000}`),
		},
		feedings: []*storage.Event{
			hopperEvt(200, `{"feed_amount_g":300}`),
			hopperEvt(500, `{"feed_amount_g":100}`),
		},
	}
	est, err := EstimateHopper(context.Background(), fs, "tank_01")
	if err != nil {
		t.Fatal(err)
	}
	want := 1000.0 - 300 + 2000 - 100
	if est.RemainingG == nil || *est.RemainingG != want {
		t.Fatalf("remaining = %v, want %v", est.RemainingG, want)
	}
	if est.ConsumedSinceRefillG != 100 {
		t.Fatalf("마지막 보충 이후 소비 = %v, want 100", est.ConsumedSinceRefillG)
	}
	if est.LastRefillG != 2000 {
		t.Fatalf("last refill = %v, want 2000", est.LastRefillG)
	}
}

// 재보정(운영자 실측)은 그 시점 잔량을 확정하고 이전 원장을 계산에서 제외한다.
// 2026-07-28 실사례: 실제 5kg 인데 누적 가산으로 원장이 8kg 이 됐던 드리프트 교정.
func TestEstimateHopperRecalibrateResetsBaseline(t *testing.T) {
	fs := &fakeHopperStore{
		refills: []*storage.Event{
			hopperEvt(100, `{"amount_g":5000}`),
			hopperEvt(300, `{"amount_g":5000}`), // 잔량 남은 상태에서 보충 → 과대 계상
		},
		feedings: []*storage.Event{
			hopperEvt(200, `{"feed_amount_g":2000}`),
			hopperEvt(500, `{"feed_amount_g":150}`), // 재보정 이후 소비만 차감돼야 함
		},
		recals: []*storage.Event{
			hopperEvt(400, `{"remaining_g":5000}`),
		},
	}
	est, err := EstimateHopper(context.Background(), fs, "tank_01")
	if err != nil {
		t.Fatal(err)
	}
	want := 5000.0 - 150
	if est.RemainingG == nil || *est.RemainingG != want {
		t.Fatalf("remaining = %v, want %v", est.RemainingG, want)
	}
	if est.ConsumedSinceRefillG != 150 {
		t.Fatalf("재보정 이후 소비 = %v, want 150", est.ConsumedSinceRefillG)
	}
	if est.LastRecalibratedAt == "" {
		t.Fatal("재보정 시각이 기록돼야 함")
	}
}

// 보충 기록이 없고 재보정만 있어도 추정이 가능해야 한다.
func TestEstimateHopperRecalibrateOnly(t *testing.T) {
	fs := &fakeHopperStore{
		recals:   []*storage.Event{hopperEvt(10, `{"remaining_g":3000}`)},
		feedings: []*storage.Event{hopperEvt(20, `{"feed_amount_g":100}`)},
	}
	est, err := EstimateHopper(context.Background(), fs, "tank_01")
	if err != nil {
		t.Fatal(err)
	}
	if est.RemainingG == nil || *est.RemainingG != 2900 {
		t.Fatalf("remaining = %v, want 2900", est.RemainingG)
	}
}
