package vision_pipeline

// 폐사 의심 감시 워커 (2026-07-07 운영자 지시 — tank_02 실사체 실측으로 방법 검증).
//
// 학습 모델 없이 고전 CV 로 "어체 크기의 옅은(흰빛) 덩어리가 수 분간 정지"를 잡는다:
//   1. 주기마다 탑뷰 카메라에서 프레임 3장(간격 sampleGap)을 뜨고 8px 셀 그리드 평균 계산
//   2. 세 장 모두에서 옅고(장면 중앙값 대비 상대 밝기 — 아래 개정) 셀 변화가 작은 셀 = "정지 옅음"
//   3. 기준(참조) 마스크(≥refAge 이전의 정지 옅음 = 수조 테두리·구조물·파이프)에 없던
//      새 정지 옅음 셀만 후보로 남긴다 (사체 = *새로* 정지한 것)
//   4. 후보 셀을 4-이웃 클러스터링 → 어체 크기 범위 블롭 + 주변이 수면(물 링)이면 알림 1건
//
// 개정 2026-07-08 (첫날 오탐 81건 실측 — 구조물·조명 오탐):
//   - 옅음 판정을 절대 밝기(>150) → 장면 중앙값 대비 상대(+relBright)로 변경.
//     구조물/파이프는 어느 조명에서나 "장면 대비 밝음"이라 참조에 항상 존재해 제외되고,
//     점등/일출로 절대 밝기가 임계를 넘나들며 생기던 "새로 나타난 옅음" 오탐이 사라진다.
//     (실측: R 파이프 셀 상대 +24~+39 전 조명 일정 / 절대는 114→159 로 150 을 교차)
//   - 참조 = refAge 이전 마스크들의 합집합 — 어군이 일시적으로 가려도 구조물이 참조에서
//     빠지지 않는다.
//   - 물 링 게이트: 블롭 주변 링(체비셰프 거리 1~2)의 물성(B≥R 또는 장면보다 어두움) 셀
//     비율 ≥ waterRingMin. 사체는 수면 위에 떠 링이 물(실측 100%), 수조 밖 구조물은
//     데크/바닥(실측 12~50%). 수조 내부 ROI 설정 없이 "수조 안" 을 자동 판별한다.
//
// 알림은 raiseAlert 콜백(main 에서 open-alert dedupe upsert 로 배선) — 같은 수조에
// open 알림이 있으면 중복 발화하지 않는다. 판정 프레임은 dir 에 증거로 저장.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // 프레임 디코드
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"bluei.kr/edge/internal/media"
)

const (
	mortalityCheckInterval = 5 * time.Minute
	mortalitySamples       = 3
	mortalityCellPx        = 8
	// 참조 마스크 최소 나이 — 이보다 오래 전부터 정지 옅음이던 셀은 구조물로 간주.
	mortalityRefAge = 45 * time.Minute
	// 어체 크기 블롭 범위 (8px 셀 수). sub 프레임(~320px 폭)에서 어체 ≈ 5~10 셀.
	mortalityBlobMin = 3
	mortalityBlobMax = 150
	// 옅음/정지 임계 (2026-07-08 오탐 증거 프레임 실측 재튜닝 — 상단 개정 주석).
	mortalityRelBright   = 20.0 // 셀 밝기 − 장면 중앙값 ≥ 이 값 → "옅음"
	mortalityBrightFloor = 90.0 // 극저조도 노이즈 방어 절대 하한
	mortalityStillMax    = 12
	// 물 링 게이트 — 블롭 주변 링의 물성 셀 비율 하한 (실측: 수면 100% / 구조물 12~50%).
	mortalityWaterRingMin = 0.75
	// OSD 자막 대역 제외 — 카메라 타임스탬프(흰 글자)가 "정지한 옅은 형체"로 오탐됨
	// (2026-07-09 00:41 야간 IR·10:46 주간, cam_top (156,28) 실사례 — 운영자 육안 폐사 없음).
	// 상단 5셀행 = 40px (320px sub 프레임 기준 자막 높이 + 여유).
	mortalityOSDTopRows = 5
)

// mortalitySampleGap — 샘플 간격. 테스트에서 단축 (그 외 변경 금지).
var mortalitySampleGap = 25 * time.Second

// MortalityCamera — 감시 대상 (입식 수조의 탑뷰 카메라).
type MortalityCamera struct {
	TankID   string
	CameraID string
}

type mortalityHistory struct {
	at   time.Time
	w, h int
	mask []bool
}

// MortalityWatcher — 주기 감시 워커.
type MortalityWatcher struct {
	log     *slog.Logger
	dir     string // 증거 프레임 저장 경로
	cameras func(ctx context.Context) ([]MortalityCamera, error)
	grab    func(ctx context.Context, cameraID string) (media.Frame, error)
	// raiseAlert — main 배선: open-alert dedupe upsert. evidence 는 payload 에 병합.
	raiseAlert func(ctx context.Context, tankID, cameraID, message string, evidence map[string]any)

	history map[string][]mortalityHistory // cameraID → 정지옅음 마스크 이력 (오래된 것부터)
}

func NewMortalityWatcher(
	dir string,
	cameras func(ctx context.Context) ([]MortalityCamera, error),
	grab func(ctx context.Context, cameraID string) (media.Frame, error),
	raiseAlert func(ctx context.Context, tankID, cameraID, message string, evidence map[string]any),
) *MortalityWatcher {
	return &MortalityWatcher{
		log:        slog.With("service", "mortality_watch"),
		dir:        dir,
		cameras:    cameras,
		grab:       grab,
		raiseAlert: raiseAlert,
		history:    map[string][]mortalityHistory{},
	}
}

func (m *MortalityWatcher) Run(ctx context.Context) {
	t := time.NewTicker(mortalityCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.checkOnce(ctx)
		}
	}
}

func (m *MortalityWatcher) checkOnce(ctx context.Context) {
	cams, err := m.cameras(ctx)
	if err != nil {
		m.log.Warn("camera list failed", "err", err)
		return
	}
	for _, cam := range cams {
		if ctx.Err() != nil {
			return
		}
		m.checkCamera(ctx, cam)
	}
}

func (m *MortalityWatcher) checkCamera(ctx context.Context, cam MortalityCamera) {
	// 프레임 3장 샘플 (간격 sampleGap).
	type sample struct {
		cells [][3]float64 // 셀별 RGB 평균
		w, h  int
		jpeg  []byte
	}
	var samples []sample
	for i := 0; i < mortalitySamples; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(mortalitySampleGap):
			}
		}
		fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		frame, err := m.grab(fctx, cam.CameraID)
		cancel()
		if err != nil {
			m.log.Debug("frame grab failed", "camera_id", cam.CameraID, "err", err)
			return // 카메라 오프라인 등 — 이번 틱 skip
		}
		cells, w, h, derr := cellMeans(frame.Data)
		if derr != nil {
			m.log.Warn("frame decode failed", "camera_id", cam.CameraID, "err", derr)
			return
		}
		samples = append(samples, sample{cells: cells, w: w, h: h, jpeg: frame.Data})
	}
	w, h := samples[0].w, samples[0].h
	for _, s := range samples[1:] {
		if s.w != w || s.h != h {
			m.log.Warn("frame size changed mid-check", "camera_id", cam.CameraID)
			delete(m.history, cam.CameraID)
			return
		}
	}

	// 정지 옅음 마스크: 세 샘플 모두 옅고(장면 중앙값 대비 상대), 샘플 간 변화가 작음.
	n := w * h
	medians := make([]float64, len(samples))
	for si, s := range samples {
		medians[si] = medianBright(s.cells)
	}
	still := make([]bool, n)
	for i := 0; i < n; i++ {
		if i/w < mortalityOSDTopRows {
			continue // OSD 자막 대역 — 후보·참조 모두 제외
		}
		ok := true
		for si, s := range samples {
			r, g, b := s.cells[i][0], s.cells[i][1], s.cells[i][2]
			bright := (r + g + b) / 3
			if bright < mortalityBrightFloor || bright-medians[si] < mortalityRelBright || r < b {
				ok = false
				break
			}
			if si > 0 {
				p := samples[si-1].cells[i]
				d := (abs64(r-p[0]) + abs64(g-p[1]) + abs64(b-p[2])) / 3
				if d > mortalityStillMax {
					ok = false
					break
				}
			}
		}
		still[i] = ok
	}

	now := time.Now()
	hist := m.history[cam.CameraID]
	// 참조: refAge 이전 마스크들의 합집합 (어군의 일시 가림에도 구조물 유지).
	// 없으면 워밍업 — 기록만.
	var ref []bool
	for i := range hist {
		if now.Sub(hist[i].at) < mortalityRefAge || hist[i].w != w || hist[i].h != h {
			continue
		}
		if ref == nil {
			ref = make([]bool, n)
		}
		for j, v := range hist[i].mask {
			if v {
				ref[j] = true
			}
		}
	}
	// 이력 추가 + 오래된 항목 정리 (2×refAge 초과 버림, 최대 32개).
	hist = append(hist, mortalityHistory{at: now, w: w, h: h, mask: still})
	for len(hist) > 32 || (len(hist) > 1 && now.Sub(hist[0].at) > 2*mortalityRefAge) {
		hist = hist[1:]
	}
	m.history[cam.CameraID] = hist
	if ref == nil {
		return // 워밍업 (참조 없음)
	}

	// 후보 = 지금 정지 옅음 ∧ 참조에 없음 (새로 정지한 것).
	cand := make([]bool, n)
	found := false
	for i := 0; i < n; i++ {
		if still[i] && !ref[i] {
			cand[i] = true
			found = true
		}
	}
	if !found {
		return
	}

	// 4-이웃 클러스터 → 어체 크기 블롭.
	blobY, blobX, blobSize, blobCells := largestBlob(cand, w, h)
	if blobSize < mortalityBlobMin || blobSize > mortalityBlobMax {
		return
	}

	// 물 링 게이트 — 사체는 수면 위(링=물), 수조 밖 구조물은 데크/바닥(링=비물).
	last := samples[len(samples)-1]
	ringFrac := waterRingFrac(blobCells, last.cells, medians[len(medians)-1], w, h)
	if ringFrac < mortalityWaterRingMin {
		m.log.Info("mortality candidate rejected (water ring)",
			"tank_id", cam.TankID, "camera_id", cam.CameraID,
			"center_cell", []int{blobX, blobY}, "cells", blobSize,
			"water_ring", fmt.Sprintf("%.2f", ringFrac))
		return
	}

	px, py := blobX*mortalityCellPx+mortalityCellPx/2, blobY*mortalityCellPx+mortalityCellPx/2
	// 증거 프레임 저장 (best-effort).
	evidencePath := ""
	if m.dir != "" {
		if err := os.MkdirAll(m.dir, 0o755); err == nil {
			evidencePath = filepath.Join(m.dir,
				fmt.Sprintf("mortality_%s_%s.jpg", cam.CameraID, now.Format("20060102-150405")))
			_ = os.WriteFile(evidencePath, samples[len(samples)-1].jpeg, 0o644)
		}
	}

	msg := fmt.Sprintf("Cage/Tank %s 폐사 의심 — 카메라 %s 화면 (%d,%d) 부근에 정지한 옅은 어체 형체가 %d분 이상 지속. 영상 확인 필요",
		cam.TankID, cam.CameraID, px, py, int(mortalityRefAge.Minutes()))
	m.raiseAlert(ctx, cam.TankID, cam.CameraID, msg, map[string]any{
		"blob_center_px": []int{px, py},
		"blob_cells":     blobSize,
		"water_ring":     ringFrac,
		"evidence_jpg":   evidencePath,
		"detected_at":    now.UTC().Format(time.RFC3339),
	})
	m.log.Info("mortality suspect alert raised",
		"tank_id", cam.TankID, "camera_id", cam.CameraID, "center_px", []int{px, py}, "cells", blobSize)
}

// cellMeans — JPEG → 8px 셀 그리드 RGB 평균. (w,h)=셀 수.
func cellMeans(jpegData []byte) ([][3]float64, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	w := b.Dx() / mortalityCellPx
	h := b.Dy() / mortalityCellPx
	if w == 0 || h == 0 {
		return nil, 0, 0, fmt.Errorf("frame too small: %dx%d", b.Dx(), b.Dy())
	}
	cells := make([][3]float64, w*h)
	counts := make([]int, w*h)
	for y := 0; y < h*mortalityCellPx; y++ {
		for x := 0; x < w*mortalityCellPx; x++ {
			r, g, bb, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y/mortalityCellPx)*w + x/mortalityCellPx
			cells[i][0] += float64(r >> 8)
			cells[i][1] += float64(g >> 8)
			cells[i][2] += float64(bb >> 8)
			counts[i]++
		}
	}
	for i := range cells {
		if counts[i] > 0 {
			cells[i][0] /= float64(counts[i])
			cells[i][1] /= float64(counts[i])
			cells[i][2] /= float64(counts[i])
		}
	}
	return cells, w, h, nil
}

// largestBlob — 4-이웃 flood fill 로 가장 큰 클러스터의 (셀 y, 셀 x 무게중심, 크기, 셀 목록).
func largestBlob(mask []bool, w, h int) (int, int, int, []int) {
	visited := make([]bool, len(mask))
	bestSize, bestY, bestX := 0, 0, 0
	var bestCells []int
	var stack []int
	for start := range mask {
		if !mask[start] || visited[start] {
			continue
		}
		stack = stack[:0]
		stack = append(stack, start)
		visited[start] = true
		size, sumY, sumX := 0, 0, 0
		var cells []int
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			size++
			sumY += i / w
			sumX += i % w
			cells = append(cells, i)
			y, x := i/w, i%w
			for _, ni := range []int{i - w, i + w, i - 1, i + 1} {
				if ni < 0 || ni >= len(mask) || !mask[ni] || visited[ni] {
					continue
				}
				ny, nx := ni/w, ni%w
				if (ny == y && absInt(nx-x) == 1) || (nx == x && absInt(ny-y) == 1) {
					visited[ni] = true
					stack = append(stack, ni)
				}
			}
		}
		if size > bestSize {
			bestSize, bestY, bestX, bestCells = size, sumY/size, sumX/size, cells
		}
	}
	_ = h
	return bestY, bestX, bestSize, bestCells
}

// medianBright — 셀 밝기 중앙값 (장면 조도 기준점).
func medianBright(cells [][3]float64) float64 {
	b := make([]float64, len(cells))
	for i, c := range cells {
		b[i] = (c[0] + c[1] + c[2]) / 3
	}
	sort.Float64s(b)
	return b[len(b)/2]
}

// waterRingFrac — 블롭 주변 링(체비셰프 거리 1~2)에서 물성 셀 비율.
// 물성 = B ≥ R+2 (물은 청록) 또는 장면 중앙값보다 어두움 (야간 IR·그늘 수면).
func waterRingFrac(blobCells []int, cells [][3]float64, median float64, w, h int) float64 {
	inBlob := map[int]bool{}
	for _, i := range blobCells {
		inBlob[i] = true
	}
	ring := map[int]bool{}
	for _, i := range blobCells {
		y, x := i/w, i%w
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				ny, nx := y+dy, x+dx
				if ny < 0 || nx < 0 || ny >= h || nx >= w {
					continue
				}
				ni := ny*w + nx
				if !inBlob[ni] {
					ring[ni] = true
				}
			}
		}
	}
	if len(ring) == 0 {
		return 0
	}
	water := 0
	for i := range ring {
		r, b := cells[i][0], cells[i][2]
		bright := (r + cells[i][1] + b) / 3
		if b >= r+2 || bright < median {
			water++
		}
	}
	return float64(water) / float64(len(ring))
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
