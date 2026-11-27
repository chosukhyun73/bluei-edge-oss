package vision_pipeline

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"bluei.kr/edge/internal/media"
)

// largestBlob — 4-이웃 클러스터가 가장 큰 것의 무게중심/크기를 반환해야 한다.
func TestLargestBlob(t *testing.T) {
	w, h := 8, 6
	mask := make([]bool, w*h)
	set := func(y, x int) { mask[y*w+x] = true }
	// 블롭 A: 3셀 가로줄 (y=1, x=1..3)
	set(1, 1)
	set(1, 2)
	set(1, 3)
	// 블롭 B: 5셀 세로줄 (y=0..4, x=6) — 최대
	for y := 0; y < 5; y++ {
		set(y, 6)
	}
	// 고립 1셀
	set(5, 0)

	y, x, size, cells := largestBlob(mask, w, h)
	if size != 5 {
		t.Fatalf("expected largest blob size 5, got %d", size)
	}
	if x != 6 || y != 2 {
		t.Fatalf("expected centroid (y=2,x=6), got (y=%d,x=%d)", y, x)
	}
	if len(cells) != 5 {
		t.Fatalf("expected 5 blob cells, got %d", len(cells))
	}
}

func TestLargestBlob_Empty(t *testing.T) {
	_, _, size, cells := largestBlob(make([]bool, 12), 4, 3)
	if size != 0 || cells != nil {
		t.Fatalf("expected 0 for empty mask, got %d (%v)", size, cells)
	}
}

// ── checkCamera 합성 프레임 시나리오 (2026-07-08 오탐 개정 검증) ──────────────
//
// 셀 그리드로 장면을 합성한다: 물(청록·어두움) 배경 + 우측 데크(갈색) +
// 파이프 세로줄(밝은 회색, 상시 존재). "밤" 과 "낮"(전 셀 +40 조도)을 오가며
// 상대 밝기·참조 합집합·물 링 게이트를 검증한다.

const (
	tw, th = 40, 30 // 셀 수 → 320x240 px
)

type synthScene struct {
	dayOffset float64
	blobAt    *[2]int // 셀 (x,y) — 2x2 옅은 블롭 (nil=없음)
}

func (s synthScene) render(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, tw*mortalityCellPx, th*mortalityCellPx))
	cellRGB := func(cx, cy int) (float64, float64, float64) {
		// 물 배경 (B>R, 어두움)
		r, g, b := 60.0, 90.0, 100.0
		if cx >= tw-8 { // 우측 데크 (R>B) — 밤엔 옅음 미달, 낮엔 중앙값 이상 (실측 L 구조물 패턴)
			r, g, b = 105, 90, 70
		}
		if cx == 10 { // 파이프 세로줄 (밝은 회색, 상시)
			r, g, b = 200, 200, 200
		}
		if s.blobAt != nil {
			dx, dy := cx-s.blobAt[0], cy-s.blobAt[1]
			if dx >= 0 && dx < 2 && dy >= 0 && dy < 2 { // 2x2 옅은 블롭
				r, g, b = 195, 195, 185
			}
		}
		return r + s.dayOffset, g + s.dayOffset, b + s.dayOffset
	}
	for cy := 0; cy < th; cy++ {
		for cx := 0; cx < tw; cx++ {
			r, g, b := cellRGB(cx, cy)
			col := color.RGBA{uint8(min255(r)), uint8(min255(g)), uint8(min255(b)), 255}
			for y := cy * mortalityCellPx; y < (cy+1)*mortalityCellPx; y++ {
				for x := cx * mortalityCellPx; x < (cx+1)*mortalityCellPx; x++ {
					img.Set(x, y, col)
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func min255(v float64) float64 {
	if v > 255 {
		return 255
	}
	return v
}

type mortalityTestRig struct {
	m      *MortalityWatcher
	cam    MortalityCamera
	frame  []byte
	mu     sync.Mutex
	alerts []map[string]any
}

func newMortalityTestRig(t *testing.T) *mortalityTestRig {
	t.Helper()
	old := mortalitySampleGap
	mortalitySampleGap = time.Millisecond
	t.Cleanup(func() { mortalitySampleGap = old })

	rig := &mortalityTestRig{cam: MortalityCamera{TankID: "tank_01", CameraID: "cam_top_t"}}
	rig.m = NewMortalityWatcher(
		"", // 증거 저장 생략
		func(context.Context) ([]MortalityCamera, error) { return []MortalityCamera{rig.cam}, nil },
		func(_ context.Context, _ string) (media.Frame, error) {
			rig.mu.Lock()
			defer rig.mu.Unlock()
			return media.Frame{Data: rig.frame, CapturedAt: time.Now()}, nil
		},
		func(_ context.Context, _, _, _ string, evidence map[string]any) {
			rig.mu.Lock()
			defer rig.mu.Unlock()
			rig.alerts = append(rig.alerts, evidence)
		},
	)
	return rig
}

// warmupNight — 밤 장면으로 1회 기록 후, 그 마스크를 refAge 이전으로 옮겨 참조화.
func (rig *mortalityTestRig) warmupNight(t *testing.T) {
	t.Helper()
	rig.frame = synthScene{dayOffset: 0}.render(t)
	rig.m.checkCamera(context.Background(), rig.cam)
	hist := rig.m.history[rig.cam.CameraID]
	if len(hist) != 1 {
		t.Fatalf("warmup history = %d", len(hist))
	}
	hist[0].at = time.Now().Add(-time.Hour)
	if len(rig.alerts) != 0 {
		t.Fatalf("warmup must not alert: %v", rig.alerts)
	}
}

// 파이프는 밤 참조에 존재 → 낮(전 셀 +40 조도)이 되어도 오탐하지 않는다.
// (구 절대 임계(150)에서는 파이프가 밤 참조에서 빠지고 낮에만 등장해 오탐 — 08:02/08:06 실사례.)
func TestMortality_LightingShiftNoFalseAlert(t *testing.T) {
	rig := newMortalityTestRig(t)
	rig.warmupNight(t)

	rig.frame = synthScene{dayOffset: 40}.render(t)
	rig.m.checkCamera(context.Background(), rig.cam)
	if len(rig.alerts) != 0 {
		t.Fatalf("lighting shift must not alert: %v", rig.alerts)
	}
}

// 수면 위에 새로 나타난 옅은 정지 블롭(사체)은 잡는다 — 물 링 통과.
func TestMortality_CarcassOnWaterAlerts(t *testing.T) {
	rig := newMortalityTestRig(t)
	rig.warmupNight(t)

	rig.frame = synthScene{dayOffset: 40, blobAt: &[2]int{20, 15}}.render(t)
	rig.m.checkCamera(context.Background(), rig.cam)
	if len(rig.alerts) != 1 {
		t.Fatalf("carcass must alert once, got %d", len(rig.alerts))
	}
	if ring, ok := rig.alerts[0]["water_ring"].(float64); !ok || ring < mortalityWaterRingMin {
		t.Fatalf("water_ring = %v", rig.alerts[0]["water_ring"])
	}
}

// 데크(수조 밖) 위 옅은 블롭은 물 링 게이트가 기각한다.
// 데크는 밤 참조에 없으므로(옅음 미달) 후보까지는 오르고, 링에서 떨어져야 한다.
func TestMortality_DeckBlobRejected(t *testing.T) {
	rig := newMortalityTestRig(t)
	rig.warmupNight(t)

	rig.frame = synthScene{dayOffset: 40, blobAt: &[2]int{tw - 5, 15}}.render(t)
	rig.m.checkCamera(context.Background(), rig.cam)
	if len(rig.alerts) != 0 {
		t.Fatalf("deck blob must be rejected by water ring: %v", rig.alerts)
	}

	// 동일 블롭이 수면 위라면 잡혀야 한다 — 기각이 링 게이트 때문임을 교차 확인.
	rig.frame = synthScene{dayOffset: 40, blobAt: &[2]int{20, 15}}.render(t)
	rig.m.checkCamera(context.Background(), rig.cam)
	if len(rig.alerts) != 1 {
		t.Fatalf("same blob on water must alert (ring-gate cross check), got %d", len(rig.alerts))
	}
}
