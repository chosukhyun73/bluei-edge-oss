package media

import (
	"errors"
	"sync"
	"time"
)

type Frame struct {
	CameraID   string    `json:"camera_id"`
	TankID     string    `json:"tank_id,omitempty"`
	Tier       string    `json:"tier"`
	MimeType   string    `json:"mime_type"`
	CapturedAt time.Time `json:"captured_at"`
	Data       []byte    `json:"-"`
}

type FrameStore struct {
	mu     sync.RWMutex
	latest map[string]Frame
}

func NewFrameStore() *FrameStore {
	return &FrameStore{latest: map[string]Frame{}}
}

func (s *FrameStore) Put(frame Frame) error {
	if frame.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if frame.Tier == "" {
		return errors.New("tier is required")
	}
	if len(frame.Data) == 0 {
		return errors.New("frame data is required")
	}
	if frame.CapturedAt.IsZero() {
		frame.CapturedAt = time.Now().UTC()
	}
	frame.Data = cloneBytes(frame.Data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest[streamKey(frame.CameraID, frame.Tier)] = frame
	return nil
}

func (s *FrameStore) Latest(cameraID, tier string) (Frame, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	frame, ok := s.latest[streamKey(cameraID, tier)]
	if !ok {
		return Frame{}, false
	}
	frame.Data = cloneBytes(frame.Data)
	return frame, true
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}
