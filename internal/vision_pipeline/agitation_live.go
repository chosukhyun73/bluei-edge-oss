package vision_pipeline

// 라이브 수면 요동 계산 (축1 1a — docs/wip/autonomous-feeding-roadmap-2026-07-06.md).
//
// 클립 사후 계산(agitation.go + scripts/agitation_curve.py)과 달리, 사이클 진행 중
// 카메라 프레임허브(api JPEG 워커, 640w 10fps)에서 초당 1샘플로 요동 시계열을
// 직접 만든다 — gap(15s) 동안 직전 펄스의 반응을 조회할 수 있게 하는 기반.
//
// 세션 수명은 신호게이팅 녹화(capture.StartCycleRecording/StopCycleRecording)와
// 동일 창 — 클립과 같은 구간을 보므로 종료 시 적재하는 vision.agitation.live 를
// 클립의 vision.agitation.recorded 와 cycle_id+camera_id 로 대조해 파리티를 검증한다.
// 공식(1fps·ROI·median smoothing·baseline)은 agitation_curve.py 와 동일하게 이식.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/media"
)

// EventAgitationLive — 라이브 요동 곡선 이벤트. 클립 사후 이벤트와 파리티 대조용.
const EventAgitationLive = "vision.agitation.live"

// liveSampleInterval — 클립 경로(1fps 디코드)와 동일한 초당 1샘플.
const liveSampleInterval = time.Second

// liveSeriesCap — series 백스톱. 녹화 백스톱(maxCycleRecordingSeconds)보다 훨씬 큼.
const liveSeriesCap = 2 * 3600

// liveBaselineSec — agitation_curve.py --baseline-sec 기본값과 동일.
const liveBaselineSec = 8

// liveMaxFillSec — 프레임 지연을 초당 평균으로 채우는 상한(초).
//
// 이 이상 끊기면 그 사이 움직임을 추정할 근거가 없고, 오래된 prev 와의 차분도
// 신뢰할 수 없다(재연결 직후 장면이 통째로 바뀌면 거대한 가짜 피크가 된다).
// 그런 구간은 채우지 않고 기준 프레임만 새로 잡는다 — 곡선은 그만큼 짧아지지만
// 없는 데이터를 지어내는 것보다 낫다.
//
// 실측(2026-08-12): 정상 사이클의 결손은 981초 중 57초(5.8%)로 잔잔히 흩어져 있다.
// 10초를 넘는 결손은 카메라 순단이며, 그 사이클은 판정 자체를 신뢰할 수 없다.
const liveMaxFillSec = 10

// liveResyncSlack — 1초 격자가 실제 시각보다 이만큼 뒤처지면 격자를 현재 시각으로
// 되맞춘다. 격자 전진만으로는 긴 단절 뒤에 계속 뒤처진 채 남는다.
const liveResyncSlack = 3 * time.Second

// liveROI — agitation_curve.py --roi 기본값과 동일 (수면 중심부, l,t,r,b 비율).
var liveROI = [4]float64{0.10, 0.15, 0.90, 0.75}

// grayFrame — ROI 크롭된 그레이스케일 평면.
type grayFrame struct {
	pix  []byte
	w, h int
}

// LiveSession — 한 사이클·한 카메라의 라이브 요동 세션. consume goroutine 만이
// prev/lastSampleAt 을 만지고, series 는 mu 로 보호되어 gap 중 조회와 동시 접근 안전.
type LiveSession struct {
	CycleID   string
	TankID    string
	CameraID  string
	StartedAt time.Time

	mu     sync.Mutex
	series []float64
	// secondsFilled — 프레임이 늦게 와 초당 평균으로 채운 초 수.
	// gapsSkipped — liveMaxFillSec 을 넘어 채우지 않고 건너뛴 횟수(카메라 단절).
	// 둘 다 곡선의 신뢰도 지표다 — 적재 payload 로 나가 사후 대조에 쓴다.
	secondsFilled int
	gapsSkipped   int
	// firstSampleAt — series[0] 에 해당하는 실제 시각.
	//
	// 세션은 사이클 시작(첫 급이기 ON)에 만들어지지만 프레임 구독은 그보다 늦게 붙는다.
	// 그 사이 초는 series 에 없으므로, 판정이 StartedAt 을 기준으로 인덱스를 구하면
	// 그 지연만큼 앞선 구간을 본다 — 2026-08-12 실측 6초(반응창 +10~20s 가 실제로는
	// +4~14s 를 봤다). 판정에는 이 값을 기준으로 줘야 한다.
	firstSampleAt time.Time

	// consume goroutine 전용 (락 불필요).
	prev         *grayFrame
	lastSampleAt time.Time

	// attach 이후에만 설정 (mu 보호). attached=false 세션은 done 대기 없이 정리.
	attached bool
	cancel   func()
	done     chan struct{}
}

func (s *LiveSession) consume(frames <-chan media.Frame) {
	defer close(s.done)
	for f := range frames {
		s.ingest(f)
	}
}

// ingest — 프레임 1장 처리. 1초 버킷당 첫 프레임만 샘플링해 직전 샘플과의
// 평균 절대차를 series 에 더한다 (agitation_curve.py 의 1fps 디코드와 등가).
func (s *LiveSession) ingest(f media.Frame) {
	at := f.CapturedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	gap := time.Duration(0)
	if !s.lastSampleAt.IsZero() {
		gap = at.Sub(s.lastSampleAt)
		if gap < liveSampleInterval {
			return
		}
	}
	g, err := decodeGrayROI(f.Data, liveROI)
	if err != nil {
		return
	}
	// 이번 프레임이 몇 초분인가. 늦게 오면 그 간격만큼 초를 채운다 — 채우지 않으면
	// series 인덱스가 경과 초보다 작아지고, 판정은 "경과 초 = 인덱스"로 조회하므로
	// 창이 밀린다.
	//
	// 값은 차분을 간격으로 나눈 **초당 평균**을 반복한다. 2초 만에 온 프레임의
	// meanAbsDiff 는 2초치 움직임이므로 그대로 1샘플로 넣으면 진폭도 과대해진다.
	n := 1
	if gap > 0 {
		n = int(gap.Seconds() + 0.5)
		if n < 1 {
			n = 1
		}
	}
	// 기준 시각은 프레임 시각이 아니라 **1초 격자**로 전진시킨다.
	//
	// at 으로 갱신하면 매 샘플마다 1초 초과분(10fps 면 0~0.1초)이 누적돼 series 가
	// 실제 경과보다 짧아진다 — 2026-08-13 실측: 455샘플 / 481초, 간격 평균 1.057초,
	// 누적 26초(5.4%). 프레임이 오래 끊겨 격자가 크게 뒤처지면 현재 시각으로 재동기화한다.
	if s.lastSampleAt.IsZero() {
		s.lastSampleAt = at
	} else {
		next := s.lastSampleAt.Add(time.Duration(n) * liveSampleInterval)
		if at.Sub(next) > liveResyncSlack {
			next = at
		}
		s.lastSampleAt = next
	}
	if n > liveMaxFillSec {
		// 긴 단절 — 그 사이 움직임을 추정할 근거가 없고, 오래된 prev 와의 차분도
		// 신뢰할 수 없다. 채우지 않고 기준 프레임만 새로 잡는다(곡선은 그만큼 짧아진다).
		s.prev = g
		s.mu.Lock()
		s.gapsSkipped++
		s.mu.Unlock()
		return
	}
	if s.prev != nil && s.prev.w == g.w && s.prev.h == g.h {
		perSec := meanAbsDiff(s.prev.pix, g.pix) / float64(n)
		s.mu.Lock()
		if len(s.series) == 0 {
			// series[0] 은 prev(직전 프레임)와 이번 프레임의 차분이므로 그 구간의
			// 시작, 즉 prev 프레임 시각에 해당한다.
			s.firstSampleAt = at.Add(-time.Duration(n) * liveSampleInterval)
		}
		for i := 0; i < n && len(s.series) < liveSeriesCap; i++ {
			s.series = append(s.series, perSec)
		}
		if n > 1 {
			s.secondsFilled += n - 1
		}
		s.mu.Unlock()
	}
	s.prev = g
}

// SeriesStart — series[0] 에 해당하는 시각. 프레임 구독이 늦게 붙으면 세션 시작보다
// 늦다. 판정은 반드시 이 값을 기준으로 인덱스를 구해야 한다(StartedAt 아님).
// 아직 표본이 없으면 StartedAt 을 돌려준다.
func (s *LiveSession) SeriesStart() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstSampleAt.IsZero() {
		return s.StartedAt
	}
	return s.firstSampleAt
}

// Series — 현재까지의 raw series 복사본.
func (s *LiveSession) Series() []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]float64, len(s.series))
	copy(out, s.series)
	return out
}

// Metrics — 현재까지의 series 로 클립 경로와 동일 공식의 메트릭 스냅샷.
// 표본 3개 미만이면 nil.
func (s *LiveSession) Metrics() *AgitationMetrics {
	return ComputeAgitationMetrics(s.Series(), liveBaselineSec)
}

// WindowStats — [from,to) 절대시각 창(펄스 세그먼트)의 반응 통계. series[0] 시각 정렬
// 초 인덱스로 변환해 SegmentPrior 를 재사용한다 (샘플링 정렬상 ±1s 오차 허용).
// baseline 은 세션 전체 기저 — 클립 SegmentPrior 와 동일한 비교 기준.
//
// ⚠ 기준은 StartedAt 이 아니라 SeriesStart() 다. 프레임 구독이 늦게 붙으면 그 차이만큼
// 창이 앞으로 밀린다 (2026-08-12 실측 6초 — 반응창 +10~20s 가 +4~14s 를 봤다).
func (s *LiveSession) WindowStats(from, to time.Time) (prior, segPeak, segMean float64, n int) {
	m := s.Metrics()
	if m == nil {
		return 0, 0, 0, 0
	}
	base := s.SeriesStart()
	startSec := int(from.Sub(base).Seconds())
	endSec := int(to.Sub(base).Seconds())
	return SegmentPrior(m.Series, m.Baseline, startSec, endSec)
}

// ComputeAgitationMetrics — agitation_curve.py 의 후처리(3점 median smoothing,
// baseline=앞 bn초 평균, calm=last5≤baseline×1.10, AUC)를 Go 로 이식.
// 라이브 series 전용 — 클립 경로는 여전히 파이썬 출력을 파싱한다.
func ComputeAgitationMetrics(series []float64, baselineSec int) *AgitationMetrics {
	if len(series) < 3 {
		return nil
	}
	smooth := make([]float64, len(series))
	for i := range series {
		lo := i - 1
		if lo < 0 {
			lo = 0
		}
		hi := i + 2
		if hi > len(series) {
			hi = len(series)
		}
		smooth[i] = median(series[lo:hi])
	}
	bn := len(smooth) / 4
	if bn > baselineSec {
		bn = baselineSec
	}
	if bn < 2 {
		bn = 2
	}
	baseline := mean(smooth[:bn])
	peak, peakAt := smooth[0], 0
	auc := 0.0
	for i, v := range smooth {
		if v > peak {
			peak, peakAt = v, i
		}
	}
	for _, v := range smooth {
		if v > baseline {
			auc += v - baseline
		}
	}
	last5 := smooth[len(smooth)-1]
	if len(smooth) >= 5 {
		last5 = mean(smooth[len(smooth)-5:])
	}
	return &AgitationMetrics{
		NSeconds:         len(smooth),
		Baseline:         baseline,
		Peak:             peak,
		PeakAtS:          peakAt,
		Last5Mean:        last5,
		CalmReached:      last5 <= baseline*1.10,
		AUCAboveBaseline: auc,
		Series:           smooth,
	}
}

func mean(v []float64) float64 {
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func median(v []float64) float64 {
	switch len(v) {
	case 1:
		return v[0]
	case 2:
		return (v[0] + v[1]) / 2
	default: // 3점 (smoothing 창은 최대 3)
		a, b, c := v[0], v[1], v[2]
		if a > b {
			a, b = b, a
		}
		if b > c {
			b = c
		}
		if a > b {
			b = a
		}
		return b
	}
}

// decodeGrayROI — JPEG → 그레이스케일 → ROI 크롭. JPEG 디코드는 통상 YCbCr 이므로
// Y 평면을 그대로 그레이스케일로 쓴다 (agitation_curve.py 의 GRAY8 변환과 등가).
func decodeGrayROI(data []byte, roi [4]float64) (*grayFrame, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	x0, y0 := int(float64(w)*roi[0]), int(float64(h)*roi[1])
	x1, y1 := int(float64(w)*roi[2]), int(float64(h)*roi[3])
	if x1 <= x0 || y1 <= y0 {
		return nil, fmt.Errorf("empty roi for %dx%d frame", w, h)
	}
	out := &grayFrame{pix: make([]byte, (x1-x0)*(y1-y0)), w: x1 - x0, h: y1 - y0}
	switch im := img.(type) {
	case *image.YCbCr:
		for y := y0; y < y1; y++ {
			row := im.Y[(y+b.Min.Y-im.Rect.Min.Y)*im.YStride:]
			copy(out.pix[(y-y0)*out.w:(y-y0+1)*out.w], row[x0+b.Min.X-im.Rect.Min.X:])
		}
	case *image.Gray:
		for y := y0; y < y1; y++ {
			row := im.Pix[(y+b.Min.Y-im.Rect.Min.Y)*im.Stride:]
			copy(out.pix[(y-y0)*out.w:(y-y0+1)*out.w], row[x0+b.Min.X-im.Rect.Min.X:])
		}
	default:
		gray := image.NewGray(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				gray.Set(x, y, img.At(x, y))
			}
		}
		for y := y0; y < y1; y++ {
			row := gray.Pix[y*gray.Stride:]
			copy(out.pix[(y-y0)*out.w:(y-y0+1)*out.w], row[x0:])
		}
	}
	return out, nil
}

func meanAbsDiff(a, b []byte) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	sum := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return float64(sum) / float64(len(a))
}

// LiveAgitation — cycleID → 라이브 세션 레지스트리. capture 의 사이클 녹화 훅이
// Start/Stop 을 호출하고, (1b) feed_cycle 판정기가 Session 으로 gap 중 조회한다.
type LiveAgitation struct {
	sink EventSink
	log  *slog.Logger

	mu       sync.Mutex
	sessions map[string]*LiveSession
}

// NewLiveAgitation — sink 는 종료 시 vision.agitation.live 적재용. nil 이면 적재 skip.
func NewLiveAgitation(sink EventSink) *LiveAgitation {
	return &LiveAgitation{
		sink:     sink,
		log:      slog.With("service", "vision_live_agitation"),
		sessions: map[string]*LiveSession{},
	}
}

// StartCycle — 세션 등록 (값싼 동기 호출 — 사이클 훅 경로를 블로킹하지 않는다).
// 카메라 준비(허브 ensure, 수 초)는 호출부가 goroutine 에서 마친 뒤 Attach 로 연결한다.
func (l *LiveAgitation) StartCycle(cycleID, tankID string) {
	s := &LiveSession{
		CycleID:   cycleID,
		TankID:    tankID,
		StartedAt: time.Now().UTC(),
		done:      make(chan struct{}),
	}
	l.mu.Lock()
	if old := l.sessions[cycleID]; old != nil && old.cancel != nil {
		old.cancel() // 방어 — 같은 cycleID 잔존 세션 정리
	}
	l.sessions[cycleID] = s
	l.mu.Unlock()
	l.log.Info("live agitation session started", "cycle_id", cycleID, "tank_id", tankID)
}

// LiveAgitationSnapshot — 진행 중 라이브 요동 세션의 현재 상태(대시보드 실시간 표기용).
// series 는 raw(정규화 전) — 프론트가 클립 경로와 동일한 P90 정규화를 적용한다.
type LiveAgitationSnapshot struct {
	CycleID   string    `json:"cycle_id"`
	TankID    string    `json:"tank_id"`
	CameraID  string    `json:"camera_id"`
	Baseline  float64   `json:"baseline"`
	StartedAt time.Time `json:"started_at"`
	Series    []float64 `json:"series"`
}

// ActiveSnapshots — 프레임 연결된 진행 중 세션들의 현재 series 스냅샷.
// 급이 중 실시간 요동 곡선을 대시보드가 폴링해 그린다 (사이클 종료 후에야 나오던
// vision.agitation.recorded 를 기다리지 않고). 표본 3개 미만 세션은 제외.
func (l *LiveAgitation) ActiveSnapshots() []LiveAgitationSnapshot {
	type ref struct {
		s        *LiveSession
		cam      string
		attached bool
	}
	l.mu.Lock()
	refs := make([]ref, 0, len(l.sessions))
	for _, s := range l.sessions {
		refs = append(refs, ref{s, s.CameraID, s.attached})
	}
	l.mu.Unlock()

	out := make([]LiveAgitationSnapshot, 0, len(refs))
	for _, r := range refs {
		if !r.attached {
			continue
		}
		series := r.s.Series()
		if len(series) < 3 {
			continue
		}
		var baseline float64
		if m := r.s.Metrics(); m != nil {
			baseline = m.Baseline
		}
		out = append(out, LiveAgitationSnapshot{
			CycleID: r.s.CycleID, TankID: r.s.TankID, CameraID: r.cam,
			Baseline: baseline, StartedAt: r.s.StartedAt, Series: series,
		})
	}
	return out
}

// Attach — 준비된 프레임 구독을 세션에 연결. 세션이 이미 종료(StopCycle)됐거나
// 이미 연결돼 있으면 들어온 구독을 즉시 cancel 해 누수를 막는다.
func (l *LiveAgitation) Attach(cycleID, cameraID string, frames <-chan media.Frame, cancel func()) {
	l.mu.Lock()
	s := l.sessions[cycleID]
	if s == nil || s.attached {
		l.mu.Unlock()
		cancel()
		return
	}
	s.attached = true
	s.CameraID = cameraID
	s.cancel = cancel
	l.mu.Unlock()
	go s.consume(frames)
	l.log.Info("live agitation frames attached", "cycle_id", cycleID, "camera_id", cameraID)
}

// Session — 진행 중 세션 조회 (1b gap 판정용). 없으면 nil.
func (l *LiveAgitation) Session(cycleID string) *LiveSession {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sessions[cycleID]
}

// StopCycle — 세션 종료 + vision.agitation.live 적재. 표본 3개 미만이면 적재 skip.
func (l *LiveAgitation) StopCycle(cycleID string) {
	l.mu.Lock()
	s := l.sessions[cycleID]
	delete(l.sessions, cycleID)
	var attached bool
	if s != nil {
		attached = s.attached
	}
	l.mu.Unlock()
	if s == nil {
		return
	}
	if !attached {
		return // 프레임 연결 전 종료 — 이후 Attach 는 세션 부재로 구독을 즉시 cancel.
	}
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
	m := s.Metrics()
	if m == nil {
		l.log.Info("live agitation session too short — skip event", "cycle_id", cycleID)
		return
	}
	s.mu.Lock()
	filled, skipped := s.secondsFilled, s.gapsSkipped
	s.mu.Unlock()
	if l.sink == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload := map[string]any{
		"cycle_id":           s.CycleID,
		"tank_id":            s.TankID,
		"camera_id":          s.CameraID,
		"started_at":         common.FormatTime(s.StartedAt),
		"n_seconds":          m.NSeconds,
		"baseline":           m.Baseline,
		"peak":               m.Peak,
		"peak_at_s":          m.PeakAtS,
		"last5_mean":         m.Last5Mean,
		"calm_reached":       m.CalmReached,
		"auc_above_baseline": m.AUCAboveBaseline,
		"series":             m.Series,
		"source":             "live_frames",
		// 곡선 신뢰도 — 채운 초가 많거나 건너뛴 단절이 있으면 판정을 의심할 것.
		"seconds_filled": filled,
		"gaps_skipped":   skipped,
		// series[0] 의 실제 시각. started_at 과 다르면 그만큼 앞이 잘린 것이다.
		"first_sample_at": common.FormatTime(s.SeriesStart()),
	}
	if _, err := l.sink.AppendEvent(ctx, "g3_pipeline", "vision", s.CameraID,
		EventAgitationLive, s.CycleID, payload); err != nil {
		l.log.Warn("append live agitation event failed", "cycle_id", cycleID, "error", err)
		return
	}
	l.log.Info("live agitation recorded",
		"cycle_id", cycleID, "n_seconds", m.NSeconds,
		"baseline", m.Baseline, "peak", m.Peak, "peak_at_s", m.PeakAtS,
		"calm_reached", m.CalmReached)
}
