package feed_cycle

// 호퍼 잔량 추정 — 이벤트 원장 기반 (전용 테이블 없음).
//
// 배경 (2026-07-17): tank_02 호퍼가 비어 오전 사이클들이 전량 무배출이었는데,
// 시스템은 "무배출 연쇄"와 "식욕 저하"를 구분할 수 없었다. 보충을 운영자가
// 기록(feeder.hopper.refilled)하고 사이클 소비(feeding.recorded)를 차감하면
// 잔량을 추정해 소진 전에 경보할 수 있다.
//
// 한계 (의도된 단순화):
//   - 소비는 "기록 급이량"(무배출 차감 반영) 기준 — 진짜 무배출은 정확하지만
//     거짓 no 는 실제 유출보다 과소 소비로 잡혀 잔량이 과대 추정될 수 있다.
//     임계(HopperLowThresholdG)에 여유를 둬 완충한다.
//   - 보충 기록이 한 번도 없으면 추정 불가(RemainingG=nil) — UI 가 안내한다.

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

const (
	// HopperLowThresholdG — 잔량 경보 임계. 표준 1사이클(85g)의 약 2.5회분.
	HopperLowThresholdG = 200.0
	// hopperLedgerWindow — 원장 스캔 범위. 보충 주기(수 일) 대비 충분.
	hopperLedgerWindow = 30 * 24 * time.Hour
	hopperScanLimit    = 3000
)

// HopperEstimate — 수조별 호퍼 잔량 추정 결과.
type HopperEstimate struct {
	TankID               string   `json:"tank_id"`
	RemainingG           *float64 `json:"remaining_g_est"` // nil = 보충 기록 없음
	LastRefillAt         string   `json:"last_refill_at,omitempty"`
	LastRefillG          float64  `json:"last_refill_g,omitempty"`
	ConsumedSinceRefillG float64  `json:"consumed_since_refill_g"`
	LastRecalibratedAt   string   `json:"last_recalibrated_at,omitempty"` // 운영자 실측 재보정 시각
	ThresholdG           float64  `json:"threshold_g"`
	Low                  bool     `json:"low"`
}

type hopperStore interface {
	QueryEvents(ctx context.Context, f storage.EventFilter) ([]*storage.Event, error)
}

// EstimateHopper — 보충·소비 이벤트를 시간순으로 걸어 잔량을 추정한다.
// 첫 보충 이전 소비는 무시하고, 차감 중 음수는 0 으로 클램프한다 (호퍼는 음수 불가).
func EstimateHopper(ctx context.Context, store hopperStore, tankID string) (*HopperEstimate, error) {
	since := time.Now().UTC().Add(-hopperLedgerWindow)
	refills, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeederHopperRefilled, DeviceID: tankID,
		Since: &since, Limit: 200,
	})
	if err != nil {
		return nil, err
	}
	// 재보정(운영자 실측) — 보충과 같은 창에서 조회. 가장 최근 재보정이 원장 기준선이 된다.
	recals, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeederHopperRecalibrated, DeviceID: tankID,
		Since: &since, Limit: 200,
	})
	if err != nil {
		return nil, err
	}

	est := &HopperEstimate{TankID: tankID, ThresholdG: HopperLowThresholdG}
	if len(refills) == 0 && len(recals) == 0 {
		return est, nil // 추정 불가
	}
	feedings, err := store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeedingRecorded, DeviceID: tankID,
		Since: &since, Limit: hopperScanLimit,
	})
	if err != nil {
		return nil, err
	}

	type entry struct {
		seq     int64
		grams   float64 // +보충 / -소비 / 재보정이면 확정 잔량
		refill  bool
		recalib bool // 원장 기준선 재설정 (그 이전 기록은 계산에서 제외)
		at      time.Time
	}
	var ledger []entry
	for _, e := range refills {
		var p struct {
			AmountG float64 `json:"amount_g"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.AmountG <= 0 {
			continue
		}
		ledger = append(ledger, entry{seq: e.Sequence, grams: p.AmountG, refill: true, at: e.RecordedAt})
	}
	for _, e := range recals {
		var p struct {
			RemainingG float64 `json:"remaining_g"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.RemainingG < 0 {
			continue
		}
		ledger = append(ledger, entry{seq: e.Sequence, grams: p.RemainingG, recalib: true, at: e.RecordedAt})
	}
	if len(ledger) == 0 {
		return est, nil
	}
	for _, e := range feedings {
		var p struct {
			FeedAmountG float64 `json:"feed_amount_g"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.FeedAmountG <= 0 {
			continue
		}
		ledger = append(ledger, entry{seq: e.Sequence, grams: -p.FeedAmountG, at: e.RecordedAt})
	}
	sort.Slice(ledger, func(i, j int) bool { return ledger[i].seq < ledger[j].seq })

	balance := 0.0
	started := false
	for _, l := range ledger {
		if l.recalib {
			// 운영자 실측 — 이 시점 잔량을 확정하고 이전 원장은 버린다.
			started = true
			balance = l.grams
			est.LastRecalibratedAt = l.at.UTC().Format(time.RFC3339)
			est.ConsumedSinceRefillG = 0
			continue
		}
		if l.refill {
			started = true
			balance += l.grams
			est.LastRefillAt = l.at.UTC().Format(time.RFC3339)
			est.LastRefillG = l.grams
			est.ConsumedSinceRefillG = 0
			continue
		}
		if !started {
			continue // 첫 보충 이전 소비는 원장 밖
		}
		balance += l.grams // 소비는 음수
		est.ConsumedSinceRefillG += -l.grams
		if balance < 0 {
			balance = 0
		}
	}
	est.RemainingG = &balance
	est.Low = balance < HopperLowThresholdG
	return est, nil
}

// hopperAlerts — 사이클 보고 직후 호출. 잔량 추정 가능할 때만 동작한다.
//   - empty_suspected: 무배출 연쇄(no ≥ 3) + 잔량 < 1사이클분 — 사이클마다 (긴급).
//   - low: 잔량 < 임계 — 보충 에피소드당 1회만 (경보 소음 억제).
func (rp *Reporter) hopperAlerts(ctx context.Context, row *storage.FeedCycle, discharge map[string]any) {
	est, err := EstimateHopper(ctx, rp.store, row.TankID)
	if err != nil || est == nil || est.RemainingG == nil {
		return
	}
	remaining := *est.RemainingG

	noCount := 0
	if discharge != nil {
		switch v := discharge["suspected_no_discharge"].(type) {
		case int:
			noCount = v
		case float64:
			noCount = int(v)
		}
	}
	cycleG := row.TotalAmountG
	if row.TargetAmountG > 0 {
		cycleG = row.TargetAmountG
	}

	if noCount >= 3 && remaining < cycleG {
		payload := map[string]any{
			"tank_id": row.TankID, "cycle_id": row.CycleID,
			"remaining_g_est": remaining, "no_count": noCount,
			"last_refill_at": est.LastRefillAt,
		}
		if _, err := rp.sink.AppendEvent(ctx, "feed_cycle", "", row.TankID,
			events.EventFeederHopperEmptySuspected, row.CycleID, payload); err == nil {
			rp.log.Warn("hopper empty suspected — 무배출 연쇄 + 잔량 부족",
				"tank_id", row.TankID, "remaining_g_est", remaining, "no_count", noCount)
		}
		return
	}

	if est.Low && !rp.hopperLowAlreadyRaised(ctx, row.TankID, est.LastRefillAt) {
		payload := map[string]any{
			"tank_id": row.TankID, "remaining_g_est": remaining,
			"threshold_g": est.ThresholdG, "last_refill_at": est.LastRefillAt,
		}
		if _, err := rp.sink.AppendEvent(ctx, "feed_cycle", "", row.TankID,
			events.EventFeederHopperLow, row.CycleID, payload); err == nil {
			rp.log.Warn("hopper low", "tank_id", row.TankID, "remaining_g_est", remaining)
		}
	}
}

// hopperLowAlreadyRaised — 마지막 보충 이후 low 경보가 이미 있으면 true.
func (rp *Reporter) hopperLowAlreadyRaised(ctx context.Context, tankID, lastRefillAt string) bool {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventFeederHopperLow, DeviceID: tankID, Limit: 1,
	})
	if err != nil || len(evs) == 0 {
		return false
	}
	t, err := time.Parse(time.RFC3339, lastRefillAt)
	if err != nil {
		return true // 보충 시각 불명이면 억제 쪽 (소음 방지)
	}
	return evs[0].RecordedAt.After(t)
}
