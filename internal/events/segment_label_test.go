package events

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestVisionSegmentLabelValidate(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	prior := 0.49
	good := VisionSegmentLabelPayload{
		LabelID: "l1", CycleID: "c1", ClipID: "clip1", CameraID: "cam1",
		OperatorID: "op1", LabeledAt: now,
		Segments: []SegmentLabel{
			{PulseNo: 1, Score: 0.55, Prior: &prior, Source: "operator"},
			{PulseNo: 2, Score: 0.49, Prior: &prior, Source: "prior"},
		},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}

	bad := good
	bad.Segments = nil
	if bad.Validate() == nil {
		t.Error("empty segments should fail")
	}

	bad = good
	bad.Segments = []SegmentLabel{{PulseNo: 1, Score: 1.5, Source: "operator"}}
	if bad.Validate() == nil {
		t.Error("score > 1 should fail")
	}

	bad = good
	bad.Segments = []SegmentLabel{{PulseNo: 1, Score: 0.5, Source: "bogus"}}
	if bad.Validate() == nil {
		t.Error("invalid source should fail")
	}

	bad = good
	bad.CycleID = ""
	if bad.Validate() == nil {
		t.Error("missing cycle_id should fail")
	}

	// 격리 세그먼트: 사유 있으면 통과(score/source 검사 생략).
	excl := good
	excl.Segments = []SegmentLabel{{PulseNo: 1, Excluded: true, ExcludeReason: "feed_not_delivered"}}
	if err := excl.Validate(); err != nil {
		t.Errorf("excluded segment with reason rejected: %v", err)
	}

	// 격리인데 사유 없으면 실패.
	exclNoReason := good
	exclNoReason.Segments = []SegmentLabel{{PulseNo: 1, Excluded: true}}
	if exclNoReason.Validate() == nil {
		t.Error("excluded segment without reason should fail")
	}
}

// discharge_modified — 정답지 판별 플래그. 미전송(nil)과 false 는 다른 뜻이라
// (미상 vs 프리필 수락) omitempty 로 구분되어야 한다.
func TestVisionSegmentLabelDischargeModified(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	base := VisionSegmentLabelPayload{
		LabelID: "l1", CycleID: "c1", ClipID: "clip1", CameraID: "cam1",
		OperatorID: "op1", LabeledAt: now,
		Segments: []SegmentLabel{{PulseNo: 1, Score: 0.55, Source: "operator"}},
	}
	// 세 상태 모두 Validate 를 막지 않는다 (기존 라벨 호환).
	yes, no := true, false
	for name, v := range map[string]*bool{"미상(nil)": nil, "수정함": &yes, "프리필 수락": &no} {
		p := base
		p.DischargeModified = v
		if err := p.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// nil 은 JSON 에서 아예 빠져야 한다 — false 로 굳으면 "프리필 수락"으로 오독된다.
	p := base
	if b, err := json.Marshal(p); err != nil {
		t.Fatal(err)
	} else if bytes.Contains(b, []byte("discharge_modified")) {
		t.Errorf("nil 인데 필드가 직렬화됨: %s", b)
	}
	p.DischargeModified = &no
	if b, err := json.Marshal(p); err != nil {
		t.Fatal(err)
	} else if !bytes.Contains(b, []byte(`"discharge_modified":false`)) {
		t.Errorf("false 가 직렬화되지 않음: %s", b)
	}
}
