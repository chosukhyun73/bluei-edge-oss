package vision_pipeline

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"math"
	"sync"
	"testing"
	"time"

	"bluei.kr/edge/internal/media"
)

// encodeUniformJPEG — 단색 그레이 프레임 JPEG.
func encodeUniformJPEG(t *testing.T, w, h int, level uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestComputeAgitationMetricsParity(t *testing.T) {
	// agitation_curve.py 공식 수계산 대조:
	// series = [1,1,1,1,5,4,3,1,1,1,1,1] (n=12)
	// 3점 median smooth = [1,1,1,1,4,4,3,1,1,1,1,1]
	// bn = max(2, min(8, 12//4=3)) = 3 → baseline = 1
	// peak=4 (첫 발생 idx 4), last5 = mean([1,1,1,1,1]) = 1 → calm (1 <= 1.1)
	// auc = (4-1)+(4-1)+(3-1) = 8
	m := ComputeAgitationMetrics([]float64{1, 1, 1, 1, 5, 4, 3, 1, 1, 1, 1, 1}, 8)
	if m == nil {
		t.Fatal("metrics nil")
	}
	if m.NSeconds != 12 || m.Baseline != 1 || m.Peak != 4 || m.PeakAtS != 4 {
		t.Fatalf("unexpected metrics: %+v", m)
	}
	if !m.CalmReached || math.Abs(m.AUCAboveBaseline-8) > 1e-9 {
		t.Fatalf("calm/auc mismatch: %+v", m)
	}
	if ComputeAgitationMetrics([]float64{1, 2}, 8) != nil {
		t.Fatal("short series must return nil")
	}
}

func TestDecodeGrayROI(t *testing.T) {
	g, err := decodeGrayROI(encodeUniformJPEG(t, 100, 50, 128), liveROI)
	if err != nil {
		t.Fatal(err)
	}
	if g.w != 80 || g.h != 30 { // (0.90-0.10)*100, (0.75-0.15)*50
		t.Fatalf("roi dims: %dx%d", g.w, g.h)
	}
	for _, p := range g.pix {
		if int(p) < 120 || int(p) > 136 {
			t.Fatalf("uniform frame pixel out of range: %d", p)
		}
	}
}

func TestLiveSessionSamplingAndWindow(t *testing.T) {
	start := time.Now().UTC()
	s := &LiveSession{StartedAt: start, done: make(chan struct{})}
	// 정지 구간도 실영상처럼 미세 노이즈 기저(≈2)를 갖게 100/102 교대 —
	// 완전 균일이면 baseline=0 → PeakRiseFrac 이 0 을 반환한다.
	darkA := encodeUniformJPEG(t, 64, 48, 100)
	darkB := encodeUniformJPEG(t, 64, 48, 102)
	bright := encodeUniformJPEG(t, 64, 48, 160)

	// 10fps 흉내 — 같은 초 안의 프레임은 1장만 샘플링돼야 한다.
	// 0~5s 는 준정지(diff≈2), 6~11s 는 매초 명암 교대 → diff≈60.
	for sec := 0; sec < 12; sec++ {
		frame := darkA
		if sec%2 == 1 {
			frame = darkB
		}
		if sec >= 6 && sec%2 == 1 {
			frame = bright
		}
		for sub := 0; sub < 3; sub++ {
			s.ingest(media.Frame{
				Data:       frame,
				CapturedAt: start.Add(time.Duration(sec)*time.Second + time.Duration(sub)*100*time.Millisecond),
			})
		}
	}

	series := s.Series()
	if len(series) != 11 { // 12 샘플 → 11 diff
		t.Fatalf("series len = %d, want 11", len(series))
	}
	for i := 0; i < 5; i++ {
		if series[i] > 6 {
			t.Fatalf("still segment diff too high at %d: %f", i, series[i])
		}
	}
	if series[6] < 30 {
		t.Fatalf("moving segment diff too low: %f", series[6])
	}

	// 창 조회 — 요동 구간(6~12s)의 prior 가 정지 구간(0~5s)보다 커야 한다.
	priorMove, peakMove, _, nMove := s.WindowStats(start.Add(6*time.Second), start.Add(12*time.Second))
	priorStill, _, _, nStill := s.WindowStats(start, start.Add(5*time.Second))
	if nMove == 0 || nStill == 0 {
		t.Fatalf("window empty: move=%d still=%d", nMove, nStill)
	}
	if priorMove <= priorStill || peakMove < 30 {
		t.Fatalf("window stats: move prior=%f peak=%f, still prior=%f", priorMove, peakMove, priorStill)
	}
}

// Stop 이 Attach 보다 먼저 온 경우(짧은 사이클 + 카메라 준비 지연) — 늦게 도착한
// 구독은 즉시 cancel 돼야 한다 (구독 누수 방지).
func TestLiveAgitationStopBeforeAttach(t *testing.T) {
	l := NewLiveAgitation(nil)
	hub := media.NewFrameHub(2)
	l.StartCycle("fc_race", "tank_01")
	l.StopCycle("fc_race")

	frames, cancel := hub.Subscribe()
	l.Attach("fc_race", "cam_a", frames, cancel)
	if hub.SubscriberCount() != 0 {
		t.Fatalf("late attach must cancel subscription, subscribers=%d", hub.SubscriberCount())
	}
	if l.Session("fc_race") != nil {
		t.Fatal("stopped session must not linger")
	}
}

type fakeSink struct {
	mu     sync.Mutex
	events []string
	last   map[string]any
}

func (f *fakeSink) AppendEvent(_ context.Context, _, _, _, eventType, _ string, payload any) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, eventType)
	if m, ok := payload.(map[string]any); ok {
		f.last = m
	}
	return 1, nil
}

func TestLiveAgitationStartStop(t *testing.T) {
	sink := &fakeSink{}
	l := NewLiveAgitation(sink)
	hub := media.NewFrameHub(2)
	l.StartCycle("fc_test", "tank_01")
	if l.Session("fc_test") == nil {
		t.Fatal("session not registered")
	}
	frames, cancel := hub.Subscribe()
	l.Attach("fc_test", "cam_a", frames, cancel)

	base := time.Now().UTC()
	for sec := 0; sec < 5; sec++ {
		level := uint8(100)
		if sec%2 == 1 {
			level = 150
		}
		_ = hub.Publish(media.Frame{
			CameraID: "cam_a", Tier: "sub", MimeType: "image/jpeg",
			CapturedAt: base.Add(time.Duration(sec) * time.Second),
			Data:       encodeUniformJPEG(t, 64, 48, level),
		})
		time.Sleep(10 * time.Millisecond) // consume goroutine 처리 여유
	}

	l.StopCycle("fc_test")
	if l.Session("fc_test") != nil {
		t.Fatal("session not removed")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 1 || sink.events[0] != EventAgitationLive {
		t.Fatalf("events = %v", sink.events)
	}
	if sink.last["cycle_id"] != "fc_test" || sink.last["source"] != "live_frames" {
		t.Fatalf("payload = %v", sink.last)
	}
}

// 프레임이 늦게 와도 series 인덱스가 경과 초와 어긋나지 않아야 한다.
// 판정은 "경과 초 = 인덱스"로 창을 잡으므로, 누락된 초를 채우지 않으면 뒤로 갈수록
// 밀린다 (2026-08-12 실측: 924샘플/981초, 13번째 펄스에서 약 52초 어긋남).
func TestLiveSessionFillsMissingSeconds(t *testing.T) {
	start := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	a := encodeUniformJPEG(t, 64, 48, 100)
	b := encodeUniformJPEG(t, 64, 48, 130)

	t.Run("2초 지연 — 2샘플로 채우고 값은 절반", func(t *testing.T) {
		s := &LiveSession{CycleID: "c", TankID: "t", CameraID: "cam", StartedAt: start}
		s.ingest(media.Frame{Data: a, CapturedAt: start})
		s.ingest(media.Frame{Data: b, CapturedAt: start.Add(time.Second)}) // 정상 1초
		one := s.Series()
		if len(one) != 1 {
			t.Fatalf("1초 간격 후 len=%d, want 1", len(one))
		}
		s2 := &LiveSession{CycleID: "c", TankID: "t", CameraID: "cam", StartedAt: start}
		s2.ingest(media.Frame{Data: a, CapturedAt: start})
		s2.ingest(media.Frame{Data: b, CapturedAt: start.Add(2 * time.Second)}) // 2초 지연
		two := s2.Series()
		if len(two) != 2 {
			t.Fatalf("2초 지연 후 len=%d, want 2 (누락 초를 채워야 한다)", len(two))
		}
		// 같은 장면 변화를 2초에 걸쳐 본 것이므로 초당 값은 절반이어야 한다.
		if diff := two[0]*2 - one[0]; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("초당 평균이 아님: 1초 %v vs 2초 지연 %v", one[0], two[0])
		}
		if two[0] != two[1] {
			t.Errorf("채운 값이 다르다: %v %v", two[0], two[1])
		}
	})

	t.Run("긴 단절 — 채우지 않고 건너뛴다", func(t *testing.T) {
		s := &LiveSession{CycleID: "c", TankID: "t", CameraID: "cam", StartedAt: start}
		s.ingest(media.Frame{Data: a, CapturedAt: start})
		s.ingest(media.Frame{Data: b, CapturedAt: start.Add(time.Second)})
		before := len(s.Series())
		// liveMaxFillSec 초과 — 없는 데이터를 지어내지 않는다.
		s.ingest(media.Frame{Data: a, CapturedAt: start.Add(time.Duration(liveMaxFillSec+5) * time.Second)})
		if got := len(s.Series()); got != before {
			t.Errorf("긴 단절인데 %d → %d 로 채웠다", before, got)
		}
		if s.gapsSkipped != 1 {
			t.Errorf("gapsSkipped = %d, want 1", s.gapsSkipped)
		}
		// 단절 이후에도 정상 샘플링이 이어져야 한다 (기준 프레임을 새로 잡았으므로).
		s.ingest(media.Frame{Data: b, CapturedAt: start.Add(time.Duration(liveMaxFillSec+6) * time.Second)})
		if got := len(s.Series()); got != before+1 {
			t.Errorf("단절 복귀 후 샘플링 안 됨: %d", got)
		}
	})

	t.Run("경과 초와 샘플 수가 일치한다", func(t *testing.T) {
		s := &LiveSession{CycleID: "c", TankID: "t", CameraID: "cam", StartedAt: start}
		// 1초·2초·3초 간격이 섞여도 30초 경과면 30샘플이어야 한다.
		at := start
		s.ingest(media.Frame{Data: a, CapturedAt: at})
		for _, gapSec := range []int{1, 2, 1, 3, 1, 2, 1, 1, 3, 2, 1, 1, 2, 1, 1, 1, 2, 1, 1, 2} {
			at = at.Add(time.Duration(gapSec) * time.Second)
			s.ingest(media.Frame{Data: b, CapturedAt: at})
			b, a = a, b // 매번 장면 교대
		}
		elapsed := int(at.Sub(start).Seconds())
		if got := len(s.Series()); got != elapsed {
			t.Errorf("경과 %d초인데 %d샘플 (어긋남 %d초)", elapsed, got, elapsed-got)
		}
	})
}

// 프레임 구독이 세션 시작보다 늦게 붙어도 창이 밀리지 않아야 한다.
// 판정은 (펄스시각 − 기준시각)으로 인덱스를 구하는데, 기준이 StartedAt 이면
// 늦게 붙은 만큼 앞선 구간을 본다 (2026-08-12 실측 6초 — +10~20s 창이 +4~14s 를 봤다).
func TestLiveSessionSeriesStartOffset(t *testing.T) {
	start := time.Date(2026, 8, 12, 9, 0, 0, 0, time.UTC)
	still := encodeUniformJPEG(t, 64, 48, 100)
	move := encodeUniformJPEG(t, 64, 48, 170)
	const lateSec = 6

	s := &LiveSession{CycleID: "c", TankID: "t", CameraID: "cam", StartedAt: start}
	if got := s.SeriesStart(); !got.Equal(start) {
		t.Errorf("표본 전에는 StartedAt 을 돌려줘야 한다: %v", got)
	}
	// 프레임이 6초 늦게 붙는다. 이후 30초는 정상 1초 간격.
	at := start.Add(lateSec * time.Second)
	s.ingest(media.Frame{Data: still, CapturedAt: at})
	for i := 1; i <= 30; i++ {
		// 20~25초 구간(절대 26~31s)만 크게 움직인다.
		f := still
		if 20 <= i && i <= 25 && i%2 == 1 {
			f = move
		}
		s.ingest(media.Frame{Data: f, CapturedAt: at.Add(time.Duration(i) * time.Second)})
	}

	if got := s.SeriesStart(); !got.Equal(at) {
		t.Errorf("SeriesStart = %v, want %v (첫 표본 시각)", got, at)
	}
	// 움직임 구간을 절대시각으로 조회 — SeriesStart 기준이면 정확히 잡혀야 한다.
	_, peakMove, _, nMove := s.WindowStats(at.Add(20*time.Second), at.Add(26*time.Second))
	_, peakStill, _, nStill := s.WindowStats(at.Add(2*time.Second), at.Add(8*time.Second))
	if nMove == 0 || nStill == 0 {
		t.Fatalf("창이 비었다: move=%d still=%d", nMove, nStill)
	}
	if peakMove <= peakStill {
		t.Errorf("움직임 구간이 더 높아야 한다: move=%.1f still=%.1f", peakMove, peakStill)
	}
	// StartedAt 기준으로 잡으면 6초 밀려 움직임 구간을 놓친다 — 회귀 방지용 대조.
	_, peakShifted, _, _ := s.WindowStats(start.Add(20*time.Second), start.Add(26*time.Second))
	if peakShifted >= peakMove {
		t.Errorf("6초 밀린 창이 더 높다? shifted=%.1f move=%.1f", peakShifted, peakMove)
	}
}

// 10fps 프레임이 꾸준히 들어오면 series 길이가 실제 경과 초와 같아야 한다.
// 기준 시각을 프레임 시각으로 갱신하면 1초 초과분(0~0.1초)이 매 샘플 누적돼
// series 가 짧아진다 — 2026-08-13 실측: 455샘플 / 481초(5.4%), 간격 평균 1.057초.
func TestLiveSessionNoSamplingDrift(t *testing.T) {
	start := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	a := encodeUniformJPEG(t, 64, 48, 100)
	b := encodeUniformJPEG(t, 64, 48, 120)
	s := &LiveSession{CycleID: "c", TankID: "t", CameraID: "cam", StartedAt: start}

	// 실환경 지터 — 프레임이 정확히 100ms 격자로 오지 않는다(107ms). 1초 경계를
	// 넘긴 시점에 샘플링되므로 gap 이 1.07초가 되고, 기준을 프레임 시각으로 갱신하면
	// 그 초과분이 매번 누적된다.
	const totalSec = 300
	const frameGap = 107 * time.Millisecond
	at := start
	for at.Sub(start) <= time.Duration(totalSec)*time.Second {
		f := a
		if int(at.Sub(start)/frameGap)%2 == 1 {
			f = b
		}
		s.ingest(media.Frame{Data: f, CapturedAt: at})
		at = at.Add(frameGap)
	}
	got := len(s.Series())
	// 첫 프레임은 diff 를 못 만들므로 1샘플 적다. ±1 허용.
	if got < totalSec-1 || got > totalSec+1 {
		t.Errorf("%d초 동안 %d샘플 (드리프트 %d초). want ≈%d",
			totalSec, got, totalSec-got, totalSec)
	}
}
