package vision_pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/inference"
)

type mockLRCN struct {
	resp    inference.LRCNResponse
	err     error
	calls   int
	lastReq inference.LRCNRequest
}

func (m *mockLRCN) Score(_ context.Context, req inference.LRCNRequest) (inference.LRCNResponse, error) {
	m.calls++
	m.lastReq = req
	return m.resp, m.err
}

type mockSink struct {
	calls         int
	lastModule    string
	lastAdapter   string
	lastDeviceID  string
	lastEventType string
	lastCorrID    string
	lastPayload   any
	err           error
}

func (m *mockSink) AppendEvent(_ context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error) {
	m.calls++
	m.lastModule = module
	m.lastAdapter = adapter
	m.lastDeviceID = deviceID
	m.lastEventType = eventType
	m.lastCorrID = corrID
	m.lastPayload = payload
	if m.err != nil {
		return 0, m.err
	}
	return 42, nil
}

func sampleResult() *capture.Result {
	return &capture.Result{
		CycleID:    "cycle_abc",
		TankID:     "tank_01",
		CameraID:   "cam_tank_01_side",
		MP4Path:    "/tmp/captures/cycle_abc.mp4",
		DurationS:  7,
		CapturedAt: time.Now().UTC(),
	}
}

func TestPipeline_HappyPath(t *testing.T) {
	lrcn := &mockLRCN{resp: inference.LRCNResponse{
		FeedingActivityScore: 0.73,
		ModelVersion:         "bootstrap-2026-05-21-blueocean-final",
		InferenceMs:          1170,
		FrameCount:           27,
		TankID:               "tank_01",
	}}
	sink := &mockSink{}
	p := New(lrcn, sink)

	p.OnCaptureResult(context.Background(), sampleResult())

	if lrcn.calls != 1 {
		t.Fatalf("LRCN.Score call count: want 1, got %d", lrcn.calls)
	}
	if lrcn.lastReq.ClipPath != "/tmp/captures/cycle_abc.mp4" || lrcn.lastReq.TankID != "tank_01" {
		t.Fatalf("LRCN request fields: %+v", lrcn.lastReq)
	}
	if sink.calls != 1 {
		t.Fatalf("sink.AppendEvent call count: want 1, got %d", sink.calls)
	}
	if sink.lastModule != "g3_pipeline" || sink.lastAdapter != "vision" || sink.lastDeviceID != "cam_tank_01_side" {
		t.Fatalf("sink fields: module=%s adapter=%s deviceID=%s", sink.lastModule, sink.lastAdapter, sink.lastDeviceID)
	}
	if sink.lastEventType != events.EventVisionObservationRecorded {
		t.Fatalf("event type: got %s", sink.lastEventType)
	}

	payload, ok := sink.lastPayload.(events.VisionObservationPayload)
	if !ok {
		t.Fatalf("payload type: %T", sink.lastPayload)
	}
	if payload.Mode != "enhanced" || payload.Phase != "feeding_start" {
		t.Fatalf("mode/phase: %s / %s", payload.Mode, payload.Phase)
	}
	if payload.ModelVersion != "bootstrap-2026-05-21-blueocean-final" {
		t.Fatalf("model_version: %s", payload.ModelVersion)
	}
	if payload.Confidence == nil || *payload.Confidence != 0.73 {
		t.Fatalf("confidence: %+v", payload.Confidence)
	}
	if payload.Scores["feeding_activity"] != 0.73 {
		t.Fatalf("feeding_activity score: %v", payload.Scores)
	}
	if payload.Quality != events.QualityOK {
		t.Fatalf("quality: %s", payload.Quality)
	}
	if payload.Evidence["cycle_id"] != "cycle_abc" {
		t.Fatalf("evidence.cycle_id: %v", payload.Evidence["cycle_id"])
	}
	if payload.Evidence["inference_ms"] != 1170 {
		t.Fatalf("evidence.inference_ms: %v", payload.Evidence["inference_ms"])
	}
	if payload.Evidence["frame_count"] != 27 {
		t.Fatalf("evidence.frame_count: %v", payload.Evidence["frame_count"])
	}
	if payload.Evidence["source"] != "lrcn" {
		t.Fatalf("evidence.source: %v", payload.Evidence["source"])
	}
}

func TestPipeline_LRCNNotReady_Silent(t *testing.T) {
	lrcn := &mockLRCN{err: inference.ErrLRCNModelNotReady}
	sink := &mockSink{}
	p := New(lrcn, sink)

	p.OnCaptureResult(context.Background(), sampleResult())

	if lrcn.calls != 1 {
		t.Fatalf("LRCN call: want 1, got %d", lrcn.calls)
	}
	if sink.calls != 0 {
		t.Fatalf("sink must NOT be called when LRCN not ready, got %d", sink.calls)
	}
}

func TestPipeline_LRCNError_Silent(t *testing.T) {
	lrcn := &mockLRCN{err: errors.New("network unreachable")}
	sink := &mockSink{}
	p := New(lrcn, sink)
	p.OnCaptureResult(context.Background(), sampleResult())
	if sink.calls != 0 {
		t.Fatalf("sink must NOT be called on LRCN generic error, got %d", sink.calls)
	}
}

func TestPipeline_PayloadInvalid_MissingModelVersion(t *testing.T) {
	// Quality=ok 인데 model_version 빈 문자열 → Validate 실패.
	lrcn := &mockLRCN{resp: inference.LRCNResponse{
		FeedingActivityScore: 0.5,
		ModelVersion:         "", // 일부러 빈값
	}}
	sink := &mockSink{}
	p := New(lrcn, sink)
	p.OnCaptureResult(context.Background(), sampleResult())
	if sink.calls != 0 {
		t.Fatalf("sink must NOT be called when payload Validate fails, got %d", sink.calls)
	}
}

func TestPipeline_SinkError_Silent(t *testing.T) {
	lrcn := &mockLRCN{resp: inference.LRCNResponse{
		FeedingActivityScore: 0.3,
		ModelVersion:         "v1",
	}}
	sink := &mockSink{err: errors.New("disk full")}
	p := New(lrcn, sink)
	// 패닉/에러 없이 silent return 검증 (signature 가 error 안 돌려줌)
	p.OnCaptureResult(context.Background(), sampleResult())
	if sink.calls != 1 {
		t.Fatalf("sink should be invoked once even if it errors: %d", sink.calls)
	}
}

func TestPipeline_NilLRCN_Noop(t *testing.T) {
	sink := &mockSink{}
	p := New(nil, sink)
	p.OnCaptureResult(context.Background(), sampleResult())
	if sink.calls != 0 {
		t.Fatalf("nil lrcn must not trigger sink: %d", sink.calls)
	}
}

func TestPipeline_NilSink_Noop(t *testing.T) {
	lrcn := &mockLRCN{}
	p := New(lrcn, nil)
	p.OnCaptureResult(context.Background(), sampleResult())
	if lrcn.calls != 0 {
		t.Fatalf("nil sink must not trigger lrcn: %d", lrcn.calls)
	}
}

func TestPipeline_NilResult_Noop(t *testing.T) {
	lrcn := &mockLRCN{}
	sink := &mockSink{}
	p := New(lrcn, sink)
	p.OnCaptureResult(context.Background(), nil)
	if lrcn.calls != 0 || sink.calls != 0 {
		t.Fatal("nil result must be no-op")
	}
}

func TestPipeline_NilPipeline_Noop(t *testing.T) {
	var p *Pipeline // nil
	// 패닉 안 함
	p.OnCaptureResult(context.Background(), sampleResult())
}
