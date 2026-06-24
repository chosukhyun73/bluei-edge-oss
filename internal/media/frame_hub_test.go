package media

import (
	"bytes"
	"testing"
	"time"
)

func TestFrameHubFansOutFramesToSubscribers(t *testing.T) {
	hub := NewFrameHub(2)
	subA, cancelA := hub.Subscribe()
	defer cancelA()
	subB, cancelB := hub.Subscribe()
	defer cancelB()

	frame := Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", CapturedAt: time.Unix(100, 0).UTC(), Data: []byte("jpeg")}
	if err := hub.Publish(frame); err != nil {
		t.Fatalf("publish: %v", err)
	}

	for name, ch := range map[string]<-chan Frame{"a": subA, "b": subB} {
		got := <-ch
		if !bytes.Equal(got.Data, frame.Data) {
			t.Fatalf("subscriber %s got %q, want %q", name, got.Data, frame.Data)
		}
	}
}

func TestFrameHubKeepsLatestFrame(t *testing.T) {
	hub := NewFrameHub(1)
	if err := hub.Publish(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", Data: []byte("first")}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := hub.Publish(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", Data: []byte("second")}); err != nil {
		t.Fatalf("publish second: %v", err)
	}
	got, ok := hub.Latest()
	if !ok {
		t.Fatal("expected latest frame")
	}
	if string(got.Data) != "second" {
		t.Fatalf("latest = %q, want second", got.Data)
	}
}

func TestFrameHubUnsubscribe(t *testing.T) {
	hub := NewFrameHub(1)
	_, cancel := hub.Subscribe()
	if got := hub.SubscriberCount(); got != 1 {
		t.Fatalf("subscriber count = %d, want 1", got)
	}
	cancel()
	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count = %d, want 0", got)
	}
}

func TestFrameHubDoesNotBlockOnSlowSubscriber(t *testing.T) {
	hub := NewFrameHub(1)
	ch, cancel := hub.Subscribe()
	defer cancel()

	if err := hub.Publish(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", Data: []byte("first")}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := hub.Publish(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", Data: []byte("second")}); err != nil {
		t.Fatalf("publish second: %v", err)
	}

	got := <-ch
	if string(got.Data) != "second" {
		t.Fatalf("slow subscriber got %q, want newest second", got.Data)
	}
}
