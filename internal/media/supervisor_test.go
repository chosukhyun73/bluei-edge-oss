package media

import (
	"testing"
	"time"
)

func TestSupervisorTracksStreamLifecycle(t *testing.T) {
	s := NewSupervisor()
	profile := StreamProfile{
		CameraID: "camera_tank_01_side",
		TankID:   "tank_01",
		Tier:     "sub",
		RTSPURL:  "rtsp://example.local/Streaming/Channels/102",
	}

	if err := s.Start(profile); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	status, ok := s.Status("camera_tank_01_side", "sub")
	if !ok {
		t.Fatal("expected status after start")
	}
	if status.State != StateStarting {
		t.Fatalf("state = %q, want %q", status.State, StateStarting)
	}
	if status.CameraID != profile.CameraID || status.TankID != profile.TankID || status.Tier != profile.Tier {
		t.Fatalf("status identity mismatch: %#v", status)
	}

	s.RecordFrame("camera_tank_01_side", "sub", time.Unix(100, 0).UTC())
	status, ok = s.Status("camera_tank_01_side", "sub")
	if !ok {
		t.Fatal("expected status after frame")
	}
	if status.State != StateStreaming {
		t.Fatalf("state = %q, want %q", status.State, StateStreaming)
	}
	if status.LastFrameAt.IsZero() {
		t.Fatal("expected last frame time")
	}
	if status.FrameCount != 1 {
		t.Fatalf("frame count = %d, want 1", status.FrameCount)
	}

	s.Stop("camera_tank_01_side", "sub", "operator_stop")
	status, ok = s.Status("camera_tank_01_side", "sub")
	if !ok {
		t.Fatal("expected status after stop")
	}
	if status.State != StateStopped {
		t.Fatalf("state = %q, want %q", status.State, StateStopped)
	}
	if status.LastError != "operator_stop" {
		t.Fatalf("last error/reason = %q", status.LastError)
	}
}

func TestSupervisorMarksReconnectAndOffline(t *testing.T) {
	s := NewSupervisor()
	profile := StreamProfile{CameraID: "camera_1", TankID: "tank_1", Tier: "sub", RTSPURL: "rtsp://example/stream"}
	if err := s.Start(profile); err != nil {
		t.Fatalf("start stream: %v", err)
	}

	s.MarkReconnect("camera_1", "sub", "rtsp timeout")
	status, _ := s.Status("camera_1", "sub")
	if status.State != StateReconnecting {
		t.Fatalf("state = %q, want %q", status.State, StateReconnecting)
	}
	if status.ReconnectCount != 1 {
		t.Fatalf("reconnect count = %d, want 1", status.ReconnectCount)
	}
	if status.LastError != "rtsp timeout" {
		t.Fatalf("last error = %q", status.LastError)
	}

	s.MarkOffline("camera_1", "sub", "max retries exceeded")
	status, _ = s.Status("camera_1", "sub")
	if status.State != StateOffline {
		t.Fatalf("state = %q, want %q", status.State, StateOffline)
	}
	if status.LastError != "max retries exceeded" {
		t.Fatalf("last error = %q", status.LastError)
	}
}

func TestSupervisorRejectsDuplicateStart(t *testing.T) {
	s := NewSupervisor()
	profile := StreamProfile{CameraID: "camera_1", Tier: "sub", RTSPURL: "rtsp://example/stream"}
	if err := s.Start(profile); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := s.Start(profile); err == nil {
		t.Fatal("expected duplicate start error")
	}
}
