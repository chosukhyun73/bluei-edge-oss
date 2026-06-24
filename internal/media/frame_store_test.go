package media

import (
	"bytes"
	"testing"
	"time"
)

func TestFrameStoreKeepsLatestFramePerStream(t *testing.T) {
	store := NewFrameStore()
	first := []byte("jpeg-frame-1")
	second := []byte("jpeg-frame-2")
	firstAt := time.Unix(100, 0).UTC()
	secondAt := time.Unix(101, 0).UTC()

	store.Put(Frame{
		CameraID:   "camera_1",
		TankID:     "tank_1",
		Tier:       "sub",
		MimeType:   "image/jpeg",
		CapturedAt: firstAt,
		Data:       first,
	})
	store.Put(Frame{
		CameraID:   "camera_1",
		TankID:     "tank_1",
		Tier:       "sub",
		MimeType:   "image/jpeg",
		CapturedAt: secondAt,
		Data:       second,
	})

	got, ok := store.Latest("camera_1", "sub")
	if !ok {
		t.Fatal("expected latest frame")
	}
	if !bytes.Equal(got.Data, second) {
		t.Fatalf("latest data = %q, want %q", got.Data, second)
	}
	if !got.CapturedAt.Equal(secondAt) {
		t.Fatalf("captured_at = %s, want %s", got.CapturedAt, secondAt)
	}
}

func TestFrameStoreCopiesFrameData(t *testing.T) {
	store := NewFrameStore()
	data := []byte("jpeg-frame")
	store.Put(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg", CapturedAt: time.Unix(100, 0).UTC(), Data: data})
	data[0] = 'X'

	got, ok := store.Latest("camera_1", "sub")
	if !ok {
		t.Fatal("expected latest frame")
	}
	if string(got.Data) != "jpeg-frame" {
		t.Fatalf("stored data mutated: %q", got.Data)
	}
	got.Data[0] = 'Y'
	again, _ := store.Latest("camera_1", "sub")
	if string(again.Data) != "jpeg-frame" {
		t.Fatalf("returned data mutated store: %q", again.Data)
	}
}

func TestFrameStoreRejectsInvalidFrame(t *testing.T) {
	store := NewFrameStore()
	if err := store.Put(Frame{Tier: "sub", MimeType: "image/jpeg", Data: []byte("x")}); err == nil {
		t.Fatal("expected missing camera_id error")
	}
	if err := store.Put(Frame{CameraID: "camera_1", MimeType: "image/jpeg", Data: []byte("x")}); err == nil {
		t.Fatal("expected missing tier error")
	}
	if err := store.Put(Frame{CameraID: "camera_1", Tier: "sub", MimeType: "image/jpeg"}); err == nil {
		t.Fatal("expected missing data error")
	}
}
