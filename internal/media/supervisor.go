package media

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type StreamState string

const (
	StateStopped      StreamState = "stopped"
	StateStarting     StreamState = "starting"
	StateStreaming    StreamState = "streaming"
	StateReconnecting StreamState = "reconnecting"
	StateDegraded     StreamState = "degraded"
	StateOffline      StreamState = "offline"
)

type StreamProfile struct {
	CameraID string
	TankID   string
	Tier     string
	RTSPURL  string
}

type StreamStatus struct {
	CameraID       string      `json:"camera_id"`
	TankID         string      `json:"tank_id,omitempty"`
	Tier           string      `json:"tier"`
	State          StreamState `json:"state"`
	StartedAt      time.Time   `json:"started_at,omitempty"`
	LastFrameAt    time.Time   `json:"last_frame_at,omitempty"`
	FrameCount     int64       `json:"frame_count"`
	ReconnectCount int         `json:"reconnect_count"`
	DroppedFrames  int         `json:"dropped_frames"`
	LastError      string      `json:"last_error,omitempty"`
}

type Supervisor struct {
	mu      sync.RWMutex
	streams map[string]*StreamStatus
}

func NewSupervisor() *Supervisor {
	return &Supervisor{streams: map[string]*StreamStatus{}}
}

func (s *Supervisor) Start(profile StreamProfile) error {
	if profile.CameraID == "" {
		return errors.New("camera_id is required")
	}
	if profile.Tier == "" {
		return errors.New("tier is required")
	}
	if profile.RTSPURL == "" {
		return errors.New("rtsp_url is required")
	}
	key := streamKey(profile.CameraID, profile.Tier)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.streams[key]; existing != nil && existing.State != StateStopped && existing.State != StateOffline {
		return fmt.Errorf("stream already active: %s/%s", profile.CameraID, profile.Tier)
	}
	s.streams[key] = &StreamStatus{
		CameraID:  profile.CameraID,
		TankID:    profile.TankID,
		Tier:      profile.Tier,
		State:     StateStarting,
		StartedAt: time.Now().UTC(),
	}
	return nil
}

func (s *Supervisor) RecordFrame(cameraID, tier string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ensure(cameraID, tier)
	st.State = StateStreaming
	st.LastFrameAt = at
	st.FrameCount++
	st.LastError = ""
}

func (s *Supervisor) MarkReconnect(cameraID, tier, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ensure(cameraID, tier)
	st.State = StateReconnecting
	st.ReconnectCount++
	st.LastError = errMsg
}

func (s *Supervisor) MarkOffline(cameraID, tier, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ensure(cameraID, tier)
	st.State = StateOffline
	st.LastError = errMsg
}

func (s *Supervisor) Stop(cameraID, tier, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ensure(cameraID, tier)
	st.State = StateStopped
	st.LastError = reason
}

func (s *Supervisor) Status(cameraID, tier string) (StreamStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.streams[streamKey(cameraID, tier)]
	if st == nil {
		return StreamStatus{}, false
	}
	return *st, true
}

func (s *Supervisor) List() []StreamStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]StreamStatus, 0, len(s.streams))
	for _, st := range s.streams {
		out = append(out, *st)
	}
	return out
}

func (s *Supervisor) ensure(cameraID, tier string) *StreamStatus {
	key := streamKey(cameraID, tier)
	st := s.streams[key]
	if st == nil {
		st = &StreamStatus{CameraID: cameraID, Tier: tier}
		s.streams[key] = st
	}
	return st
}

func streamKey(cameraID, tier string) string {
	return cameraID + ":" + tier
}
