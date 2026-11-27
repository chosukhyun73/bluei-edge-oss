package feed_cycle

import (
	"context"
	"testing"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

// 섀도 판정 — 라이브 요동 + 펄스 이벤트로 "AI였다면 pN 중단"이 보고에 실리는지.
func TestReporterShadowJudge(t *testing.T) {
	row := completedCycleRow() // 7펄스, started 00:57:00Z
	started := row.StartedAt

	// 펄스 7개 — 18s 간격, ON 3s. ON시작_i = started + (i-1)*18s.
	var pulseEvents []*storage.Event
	for i := 1; i <= 7; i++ {
		on := started.Add(time.Duration(i-1) * 18 * time.Second)
		pulseEvents = append(pulseEvents, mkEvent(t, map[string]any{
			"cycle_id": "cycle_test_01", "pulse_no": i,
			"actual_duration_ms": 3000,
			"completed_at":       on.Add(3 * time.Second).Format(time.RFC3339Nano),
		}))
	}

	// 라이브 요동 — p1~p3(0~54s) 강반응(15.0), p4~(54s~) 무반응(5.2). baseline 5.
	series := make([]float64, 130)
	for i := range series {
		if i < 54 {
			series[i] = 15.0
		} else {
			series[i] = 5.2
		}
	}
	liveEvent := mkEvent(t, map[string]any{
		"cycle_id": "cycle_test_01", "baseline": 5.0, "series": series,
		"started_at": started.Format(time.RFC3339Nano),
	})

	store := &reporterFakeStore{
		cycle: row,
		events: map[string][]*storage.Event{
			events.EventFeedPulseCompleted:     pulseEvents,
			vision_pipeline.EventAgitationLive: {liveEvent},
		},
		cameras: []*storage.CameraProfile{{CameraID: "cam_uw", TankID: "tank_01", Position: "underwater"}},
	}
	sink := &reporterFakeSink{}
	testReporter(store, sink).OnCycleComplete(context.Background(), "cycle_test_01")

	// 보고 외에 경보 이벤트가 함께 나갈 수 있으므로 타입으로 골라낸다.
	var report map[string]any
	for _, e := range sink.entries {
		if e.EventType == events.EventFeedCycleReport {
			if report != nil {
				t.Fatalf("expected 1 report, got more")
			}
			report = e.Payload
		}
	}
	if report == nil {
		t.Fatalf("report missing: %v", sink.entries)
	}
	sj, _ := report["shadow_judge"].(map[string]any)
	if sj == nil {
		t.Fatalf("shadow_judge missing: %v", report)
	}
	// 트랙 U: 무반응 2연속 → p5 에서 중단 판정. 7펄스 실행 → 2펄스 절약.
	if sj["track"] != "U" || sj["source"] != "live" || sj["shadow"] != true {
		t.Errorf("shadow_judge meta = %v", sj)
	}
	if sj["stop_at_pulse"].(float64) != 5 || sj["pulses_saved"].(float64) != 2 {
		t.Errorf("stop_at=%v saved=%v, want 5/2 (priors=%v)",
			sj["stop_at_pulse"], sj["pulses_saved"], sj["priors"])
	}
	// gap끝 복귀도 병기 필드 — 활동이 15.0 인 p1~p3 gap끝은 미복귀(비 3.0),
	// 5.2 로 내려간 p4~ 는 복귀(비 1.04 ≤ 1.2). (JSON 왕복 후라 []any.)
	recovered, _ := sj["gap_end_recovery"].([]any)
	ratios, _ := sj["gap_end_ratio"].([]any)
	if len(recovered) != 7 || len(ratios) != 7 {
		t.Fatalf("gap_end fields missing: recovery=%v ratio=%v", sj["gap_end_recovery"], sj["gap_end_ratio"])
	}
	wantRec := []bool{false, false, false, true, true, true, true}
	for i, w := range wantRec {
		if recovered[i] != w {
			t.Errorf("gap_end_recovery[%d] = %v, want %v (ratio=%v)", i, recovered[i], w, ratios[i])
		}
	}
	if ratios[0].(float64) != 3.0 || ratios[3].(float64) != 1.04 {
		t.Errorf("gap_end_ratio = %v, want [3 3 3 1.04 ...]", ratios)
	}

	// 수중캠 없으면 트랙 T.
	store.cameras = nil
	sink2 := &reporterFakeSink{}
	testReporter(store, sink2).OnCycleComplete(context.Background(), "cycle_test_01")
	sj2, _ := sink2.entries[0].Payload["shadow_judge"].(map[string]any)
	if sj2 == nil || sj2["track"] != "T" {
		t.Errorf("track = %v, want T", sj2)
	}
}

// 요동 데이터가 없으면 shadow_judge 는 null — 보고는 그대로 진행.
func TestReporterShadowJudgeAbsent(t *testing.T) {
	store := &reporterFakeStore{cycle: completedCycleRow(), events: map[string][]*storage.Event{}}
	sink := &reporterFakeSink{}
	testReporter(store, sink).OnCycleComplete(context.Background(), "cycle_test_01")
	if len(sink.entries) != 1 {
		t.Fatalf("expected 1 report, got %d", len(sink.entries))
	}
	if sink.entries[0].Payload["shadow_judge"] != nil {
		t.Errorf("shadow_judge = %v, want null", sink.entries[0].Payload["shadow_judge"])
	}
}
