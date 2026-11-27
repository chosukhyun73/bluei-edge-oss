package vision_pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bluei.kr/edge/internal/media"
)

func TestFloorStillCollectTank(t *testing.T) {
	dir := t.TempDir()
	sink := &fakeSink{}
	cams := func(context.Context) ([]FloorCamera, error) {
		return []FloorCamera{
			{TankID: "tank_01", CameraID: "cam_underwater_7_1"},
			{TankID: "tank_02", CameraID: "cam_top_7_1_2_R"},
		}, nil
	}
	grab := func(_ context.Context, cameraID string) (media.Frame, error) {
		return media.Frame{
			CameraID: cameraID, Tier: "sub", MimeType: "image/jpeg",
			CapturedAt: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
			Data:       []byte("jpegdata"),
		}, nil
	}
	c := NewFloorStillCollector(sink, dir, cams, grab)

	c.CollectTank(context.Background(), "tank_01", "pre_cycle", "fc_x")

	path := filepath.Join(dir, "tank_01", "20260706T120000_pre_cycle.jpg")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("still not written: %v", err)
	}
	if string(data) != "jpegdata" {
		t.Fatalf("still content = %q", data)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 1 || sink.events[0] != EventFloorStillStored {
		t.Fatalf("events = %v", sink.events)
	}
	if sink.last["tank_id"] != "tank_01" || sink.last["trigger"] != "pre_cycle" ||
		sink.last["cycle_id"] != "fc_x" || sink.last["path"] != path {
		t.Fatalf("payload = %v", sink.last)
	}
	// tank_02 카메라(수중캠 아님 — 목록에 있어도 tank 불일치)는 수집 안 됨.
	if _, err := os.Stat(filepath.Join(dir, "tank_02")); !os.IsNotExist(err) {
		t.Fatal("tank_02 must not be collected")
	}
}

// 조도 게이트 — 소등(어두운) 프레임은 저장하지 않고, 주간 프레임은 저장한다.
// (encodeUniformJPEG 는 agitation_live_test.go 의 helper 재사용.)
func TestFloorStillLumaGate(t *testing.T) {
	dir := t.TempDir()
	sink := &fakeSink{}
	cams := func(context.Context) ([]FloorCamera, error) {
		return []FloorCamera{{TankID: "tank_01", CameraID: "cam_underwater_7_1"}}, nil
	}
	frameData := encodeUniformJPEG(t, 32, 32, 5) // 평균 휘도 ~5 < 40
	grab := func(_ context.Context, cameraID string) (media.Frame, error) {
		return media.Frame{
			CameraID: cameraID, MimeType: "image/jpeg",
			CapturedAt: time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC),
			Data:       frameData,
		}, nil
	}
	c := NewFloorStillCollector(sink, dir, cams, grab)

	c.CollectTank(context.Background(), "tank_01", "periodic", "")
	if _, err := os.Stat(filepath.Join(dir, "tank_01")); !os.IsNotExist(err) {
		t.Fatal("dark frame must be skipped")
	}
	sink.mu.Lock()
	if len(sink.events) != 0 {
		t.Fatalf("dark frame must not append events: %v", sink.events)
	}
	sink.mu.Unlock()

	frameData = encodeUniformJPEG(t, 32, 32, 128) // 평균 휘도 ~128
	c.CollectTank(context.Background(), "tank_01", "periodic", "")
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 1 || sink.events[0] != EventFloorStillStored {
		t.Fatalf("bright frame must be stored: %v", sink.events)
	}
}
