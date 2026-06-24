package media

import (
	"context"
	"sync"
)

type FrameHub struct {
	mu          sync.RWMutex
	buffer      int
	latest      Frame
	hasLatest   bool
	subscribers map[chan Frame]struct{}
}

func NewFrameHub(buffer int) *FrameHub {
	if buffer < 1 {
		buffer = 1
	}
	return &FrameHub{buffer: buffer, subscribers: map[chan Frame]struct{}{}}
}

func (h *FrameHub) Publish(frame Frame) error {
	if frame.CameraID == "" || frame.Tier == "" || len(frame.Data) == 0 {
		store := NewFrameStore()
		return store.Put(frame)
	}
	frame.Data = cloneBytes(frame.Data)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = frame
	h.hasLatest = true
	for ch := range h.subscribers {
		select {
		case ch <- cloneFrame(frame):
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- cloneFrame(frame):
			default:
			}
		}
	}
	return nil
}

func (h *FrameHub) Subscribe() (<-chan Frame, func()) {
	ch := make(chan Frame, h.buffer)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, ch)
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

func (h *FrameHub) Latest() (Frame, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.hasLatest {
		return Frame{}, false
	}
	return cloneFrame(h.latest), true
}

func (h *FrameHub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

func (h *FrameHub) WaitLatest(ctx context.Context) (Frame, bool) {
	if frame, ok := h.Latest(); ok {
		return frame, true
	}
	frames, cancel := h.Subscribe()
	defer cancel()
	select {
	case frame, ok := <-frames:
		return frame, ok
	case <-ctx.Done():
		return Frame{}, false
	}
}

func cloneFrame(frame Frame) Frame {
	frame.Data = cloneBytes(frame.Data)
	return frame
}
