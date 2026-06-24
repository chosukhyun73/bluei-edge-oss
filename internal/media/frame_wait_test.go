package media

import (
	"context"
	"testing"
	"time"
)

func TestFrameHubWaitLatestReturnsPublishedFrame(t *testing.T) {
	hub := NewFrameHub(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = hub.Publish(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", Data: []byte("jpeg")})
	}()
	frame, ok := hub.WaitLatest(ctx)
	if !ok {
		t.Fatal("expected frame")
	}
	if string(frame.Data) != "jpeg" {
		t.Fatalf("data = %q", frame.Data)
	}
}

func TestFrameHubWaitLatestTimesOut(t *testing.T) {
	hub := NewFrameHub(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, ok := hub.WaitLatest(ctx)
	if ok {
		t.Fatal("expected timeout without frame")
	}
}
