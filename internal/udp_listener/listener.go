// Package udp_listener consumes 1Hz weight stream packets from ESP32
// feeder controllers and forwards them to feed_cycle.Worker for live
// silo-threshold evaluation.
//
// Why UDP: HTTP polling round-trip + JSON parse cost is too high for
// 1Hz continuous data. UDP fire-and-forget keeps ESP32 CPU/Wi-Fi
// budget low and backend recv cost negligible.
//
// Wire format (JSON, ≤256 bytes):
//
//	{
//	  "id": "feeder_tank_02",
//	  "ts_ms": 12345678,
//	  "raw": -11500,
//	  "grams": 38.5,
//	  "mode": "linear",
//	  "rssi": -42,
//	  "uptime_ms": 999999,
//	  "cycle_active": false
//	}
//
// Backend listens on :9998 (configurable). No auth — same trust
// boundary as HTTP polling on LAN.
package udp_listener

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
)

// Packet is the wire format an ESP32 sends to UDP :9998.
type Packet struct {
	ControllerID string  `json:"id"`
	TsMs         int64   `json:"ts_ms"`
	Raw          int64   `json:"raw"`
	Grams        float64 `json:"grams"`
	Mode         string  `json:"mode"`
	RSSI         int     `json:"rssi"`
	UptimeMs     int64   `json:"uptime_ms"`
	CycleActive  bool    `json:"cycle_active"`
}

// trackEntry holds per-controller state for 10s downsample tracing.
type trackEntry struct {
	lastReportAt time.Time
	lastGrams    float64
	prevGrams    float64 // grams reported at previous 10s tick
	hasPrev      bool
}

// LiveWeight is the most recent UDP weight sample for a controller.
// Exposed via GET /v1/controllers/{id}/live-weight.
//
// Grams 는 표시용 안정값 (EMA + dead band 적용 후). silo_threshold 같은
// 실시간 안전 판단은 HandleStreamWeight 에 들어가는 raw grams 를 그대로 사용.
type LiveWeight struct {
	ControllerID string    `json:"controller_id"`
	Grams        float64   `json:"grams"`     // 안정화 후 (EMA + dead band)
	RawGrams     float64   `json:"raw_grams"` // 필터 전 (디버그/대조용)
	Raw          int64     `json:"raw"`
	Mode         string    `json:"mode"`
	RSSI         int       `json:"rssi"`
	ReceivedAt   time.Time `json:"received_at"`
	AgeMs        int64     `json:"age_ms"`
}

// 신호 안정화 파라미터 — 상용 저울 LCD 와 유사한 거동.
// 3단계 처리 (상용 저울 표준):
//   - |Δ| ≥ motionThresholdG : "motion" 으로 간주, 즉시 새 값 추적 (응답 지연 없음)
//   - deadBandG < |Δ| < motion : EMA α 적용 (점진 평활)
//   - |Δ| ≤ deadBandG         : dead band — 이전 평활값 유지 (LCD 동결)
//
// silo_threshold 는 raw 값을 사용하므로 어차피 즉시 반응.
const (
	emaAlpha         = 0.1
	deadBandG        = 3.0
	motionThresholdG = 30.0 // ≥30g 변화 = 사용자가 무게 올리고/내리고 → 즉시 추적
)

// Listener binds a UDP socket and forwards weight packets to the
// feed_cycle worker. Lifecycle: New → Start → (Stop or ctx cancel).
type Listener struct {
	addr    string
	worker  *feed_cycle.Worker
	store   storage.Store // optional; if non-nil, last_seen_at is touched
	log     *slog.Logger
	conn    net.PacketConn
	done    chan struct{}
	stopped bool

	// 10s downsample trace (per-controller) — appended to traceFile if set.
	traceMu     sync.Mutex
	tracks      map[string]*trackEntry
	tracePeriod time.Duration
	traceFile   *os.File

	// live cache — most recent UDP weight per controller.
	liveMu sync.RWMutex
	live   map[string]LiveWeight

	// 운영자 영점 — 현재 안정값을 0 으로 만드는 software offset.
	// piecewise cal 의 0점 raw drift 보정 (운영자가 빈 통일 때 버튼 누름).
	// backend restart 시 잃음 (memory only). 영구 저장은 본선 후.
	zeroMu     sync.RWMutex
	zeroOffset map[string]float64
}

// New constructs a Listener. addr is e.g. ":9998".
// store may be nil — last_seen_at touch is skipped in that case.
func New(addr string, worker *feed_cycle.Worker, store storage.Store, log *slog.Logger) *Listener {
	if log == nil {
		log = slog.Default()
	}
	return &Listener{
		addr:        addr,
		worker:      worker,
		store:       store,
		log:         log,
		done:        make(chan struct{}),
		tracks:      make(map[string]*trackEntry),
		tracePeriod: 10 * time.Second,
		live:        make(map[string]LiveWeight),
		zeroOffset:  make(map[string]float64),
	}
}

// SetZero 는 운영자가 영점 버튼을 누른 시점의 현재 안정값을 zero offset 으로 저장한다.
// 이후 GetLiveWeight 의 grams 는 (stable - offset) 으로 반환됨.
// 빈 통 미수신 시 false.
func (l *Listener) SetZero(controllerID string) (offset float64, ok bool) {
	l.liveMu.RLock()
	w, found := l.live[controllerID]
	l.liveMu.RUnlock()
	if !found {
		return 0, false
	}
	l.zeroMu.Lock()
	l.zeroOffset[controllerID] = w.Grams
	l.zeroMu.Unlock()
	return w.Grams, true
}

// ClearZero 는 controller 의 영점을 해제한다 (offset = 0).
func (l *Listener) ClearZero(controllerID string) {
	l.zeroMu.Lock()
	delete(l.zeroOffset, controllerID)
	l.zeroMu.Unlock()
}

func (l *Listener) getZeroOffset(controllerID string) float64 {
	l.zeroMu.RLock()
	defer l.zeroMu.RUnlock()
	return l.zeroOffset[controllerID]
}

// GetLiveWeight returns the most recent UDP weight sample for the controller.
// Returns ok=false if no packet has arrived yet.
// Signature matches api.LiveWeightProvider (avoids circular import on a wrapper type).
// grams 는 EMA + dead band 적용 후 안정값에 운영자 영점 offset 적용한 값.
// rawGrams 는 필터/영점 전 값.
func (l *Listener) GetLiveWeight(controllerID string) (grams, rawGrams float64, raw int64, mode string, rssi int, ageMs int64, ok bool) {
	l.liveMu.RLock()
	w, found := l.live[controllerID]
	l.liveMu.RUnlock()
	if !found {
		return 0, 0, 0, "", 0, 0, false
	}
	offset := l.getZeroOffset(controllerID)
	return w.Grams - offset, w.RawGrams, w.Raw, w.Mode, w.RSSI, time.Since(w.ReceivedAt).Milliseconds(), true
}

// ListLive returns a snapshot of all live weights.
func (l *Listener) ListLive() []LiveWeight {
	l.liveMu.RLock()
	defer l.liveMu.RUnlock()
	now := time.Now()
	out := make([]LiveWeight, 0, len(l.live))
	for _, w := range l.live {
		w.AgeMs = now.Sub(w.ReceivedAt).Milliseconds()
		out = append(out, w)
	}
	return out
}

// EnableTraceFile opens path for append; subsequent UDP weight packets
// will produce one 10s-downsampled line per controller. Call before Start.
func (l *Listener) EnableTraceFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.traceFile = f
	fmt.Fprintf(f, "# time\tcontroller_id\tgrams\tdelta_g\tinterval_s\n")
	return nil
}

// Start binds the UDP socket and spawns the receive goroutine.
// Returns an error if the bind fails. Cancelling ctx stops the listener.
func (l *Listener) Start(ctx context.Context) error {
	if l.worker == nil {
		return errors.New("udp_listener: nil worker")
	}
	c, err := net.ListenPacket("udp", l.addr)
	if err != nil {
		return err
	}
	l.conn = c
	l.log.Info("udp weight listener started", "addr", l.addr)
	go l.recvLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = l.Stop()
	}()
	return nil
}

// Stop closes the socket. Safe to call multiple times.
func (l *Listener) Stop() error {
	if l.stopped {
		return nil
	}
	l.stopped = true
	if l.conn != nil {
		_ = l.conn.Close()
	}
	<-l.done
	if l.traceFile != nil {
		_ = l.traceFile.Close()
	}
	l.log.Info("udp weight listener stopped")
	return nil
}

func (l *Listener) recvLoop(ctx context.Context) {
	defer close(l.done)
	buf := make([]byte, 1024)
	for {
		_ = l.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, src, err := l.conn.ReadFrom(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if ctx.Err() != nil {
					return
				}
				continue
			}
			if l.stopped || ctx.Err() != nil {
				return
			}
			l.log.Warn("udp recv error", "error", err)
			continue
		}
		l.handle(ctx, buf[:n], src)
	}
}

func (l *Listener) handle(ctx context.Context, raw []byte, src net.Addr) {
	var p Packet
	if err := json.Unmarshal(raw, &p); err != nil {
		l.log.Debug("udp decode failed", "src", src, "bytes", len(raw))
		return
	}
	if p.ControllerID == "" {
		return
	}
	now := time.Now().UTC()

	// 0) live cache — UI live-weight 표기용 (motion / EMA / dead band 3단계).
	l.liveMu.Lock()
	prev, exists := l.live[p.ControllerID]
	var stable float64
	delta := math.Abs(p.Grams - prev.Grams)
	switch {
	case !exists:
		stable = p.Grams // 첫 패킷 — 초기 평활값
	case delta >= motionThresholdG:
		stable = p.Grams // motion — 즉시 새 값 (저울 LCD 의 unstable 표시 후 갱신과 동등)
	case delta <= deadBandG:
		stable = prev.Grams // dead band — 이전 안정값 유지 (LCD 정지)
	default:
		stable = emaAlpha*p.Grams + (1-emaAlpha)*prev.Grams // 중간 변화 — EMA
	}
	l.live[p.ControllerID] = LiveWeight{
		ControllerID: p.ControllerID,
		Grams:        stable,
		RawGrams:     p.Grams,
		Raw:          p.Raw,
		Mode:         p.Mode,
		RSSI:         p.RSSI,
		ReceivedAt:   now,
	}
	l.liveMu.Unlock()

	// 1) Stream weight → feed_cycle (silo threshold + lastWeightAfter).
	matched := l.worker.HandleStreamWeight(ctx, p.ControllerID, p.Grams, now)

	// 2) heartbeat — last_seen_at 갱신 (store 가 nil 이면 skip).
	if l.store != nil {
		l.touchLastSeen(ctx, p.ControllerID, src)
	}

	if matched != nil {
		l.log.Debug("udp weight matched cycle",
			"controller_id", p.ControllerID,
			"cycle_id", matched.CycleID,
			"grams", p.Grams,
			"mode", p.Mode)
	}

	// 10s downsample trace — 운영자 화면 표기용.
	l.recordTrace(now, p.ControllerID, p.Grams)
}

// recordTrace emits one line every tracePeriod per controller (default 10s).
// 출력 채널: slog INFO (msg="weight10s") + 선택적 traceFile.
func (l *Listener) recordTrace(now time.Time, controllerID string, grams float64) {
	l.traceMu.Lock()
	t, ok := l.tracks[controllerID]
	if !ok {
		t = &trackEntry{lastReportAt: now, lastGrams: grams, prevGrams: grams}
		l.tracks[controllerID] = t
	}
	t.lastGrams = grams
	elapsed := now.Sub(t.lastReportAt)
	report := elapsed >= l.tracePeriod
	var (
		delta    float64
		interval float64
		prev     = t.prevGrams
		hasPrev  = t.hasPrev
	)
	if report {
		delta = grams - t.prevGrams
		interval = elapsed.Seconds()
		t.prevGrams = grams
		t.hasPrev = true
		t.lastReportAt = now
	}
	l.traceMu.Unlock()

	if !report {
		return
	}
	if !hasPrev {
		delta = 0 // 첫 보고에는 delta 의미 없음
		_ = prev
	}
	l.log.Info("weight10s",
		"controller_id", controllerID,
		"grams", grams,
		"delta_g", delta,
		"interval_s", interval)
	if l.traceFile != nil {
		fmt.Fprintf(l.traceFile, "%s\t%s\t%.2f\t%+.2f\t%.1f\n",
			now.Format(time.RFC3339), controllerID, grams, delta, interval)
	}
}

func (l *Listener) touchLastSeen(ctx context.Context, controllerID string, src net.Addr) {
	c, err := l.store.GetController(ctx, controllerID)
	if err != nil || c == nil {
		return
	}
	c.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	if udpAddr, ok := src.(*net.UDPAddr); ok && udpAddr != nil {
		ip := udpAddr.IP.String()
		if ip != "" && c.IPAddress != ip {
			c.IPAddress = ip
		}
	}
	_ = l.store.UpsertController(ctx, c)
}
