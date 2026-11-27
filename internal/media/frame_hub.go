package media

import (
	"context"
	"sync"
	"time"
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

// latestFresh returns the latest frame only if captured within maxAge.
// 업스트림 단절 시 h.latest 는 마지막 프레임을 영구 보관하므로(pipeline 이 더는
// publish 안 함), 오래된 프레임은 "정지"로 간주해 반환하지 않는다.
func (h *FrameHub) latestFresh(maxAge time.Duration) (Frame, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.hasLatest {
		return Frame{}, false
	}
	if maxAge > 0 && !h.latest.CapturedAt.IsZero() && time.Since(h.latest.CapturedAt) > maxAge {
		return Frame{}, false
	}
	return cloneFrame(h.latest), true
}

// WaitLatestFresh returns a frame no older than maxAge. 최신 프레임이 stale 면
// 새 프레임을 기다린다(구독) — 라이브 카메라는 곧 publish 하므로 fresh 를 받고,
// 단절된 카메라는 ctx 만료로 (Frame{}, false) → 호출부가 stale 프레임을 LIVE 로
// 내보내지 않게 한다. maxAge<=0 이면 WaitLatest 와 동일.
func (h *FrameHub) WaitLatestFresh(ctx context.Context, maxAge time.Duration) (Frame, bool) {
	if maxAge <= 0 {
		return h.WaitLatest(ctx)
	}
	if frame, ok := h.latestFresh(maxAge); ok {
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
