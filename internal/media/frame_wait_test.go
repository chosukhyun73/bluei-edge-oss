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

// WaitLatestFresh: 최신 프레임이 maxAge 보다 오래되면(업스트림 정지) 반환하지 않고
// 새 프레임을 기다린다. 새 프레임 없으면 ctx 만료로 false → 정지화면 방어.
func TestFrameHubWaitLatestFreshRejectsStale(t *testing.T) {
	hub := NewFrameHub(1)
	// 10초 전 프레임을 latest 로 주입(정지 상태 모사).
	stale := Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg",
		CapturedAt: time.Now().Add(-10 * time.Second).UTC(), Data: []byte("stale")}
	if err := hub.Publish(stale); err != nil {
		t.Fatal(err)
	}
	// maxAge=5s → stale 거부, 새 프레임도 없으므로 timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, ok := hub.WaitLatestFresh(ctx, 5*time.Second); ok {
		t.Fatal("stale frame should not be returned as fresh")
	}

	// 새 프레임 도착 → fresh 반환.
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = hub.Publish(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg",
			CapturedAt: time.Now().UTC(), Data: []byte("fresh")})
	}()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	frame, ok := hub.WaitLatestFresh(ctx2, 5*time.Second)
	if !ok || string(frame.Data) != "fresh" {
		t.Fatalf("expected fresh frame, got ok=%v data=%q", ok, frame.Data)
	}
}
