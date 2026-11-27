package vision_pipeline

// 바닥 상태 스틸 자동 수집 (축3 3a — docs/wip/autonomous-feeding-roadmap-2026-07-06.md).
//
// 수중캠 보유 수조(트랙 U)에서 바닥 상태(배설물/펠릿 잔류) 라벨링·학습용 스틸을
// 자동 축적한다. 타이밍: 사이클 직전(pre_cycle, 공급모터 리드 전 = 급이 전 바닥) /
// 직후(post_cycle) / 정기(periodic). 하루 수십 장, 장당 ~50KB — 저장 부담 미미.
//
// 스틸은 프레임 전체를 저장한다(바닥 ROI 크롭은 라벨/CV 단계에서) — 수집 시점에
// ROI 를 고정하면 이후 라벨 UI·고전 CV 의 자유도를 잃는다. 파일은 data_dir 하위
// (mp4 retention 삭제 대상인 capture temp_dir 과 분리 — 학습 데이터는 영속).

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/media"
)

// EventFloorStillStored — 바닥 스틸 적재 이벤트. 라벨링 UI(축3 3b)의 목록 source.
const EventFloorStillStored = "media.floor_still.stored"

// floorStillPeriodicInterval — 정기 수집 간격. 하루 48장 + 사이클당 2장.
const floorStillPeriodicInterval = 30 * time.Minute

// floorStillFreshWindow — 스틸로 인정할 프레임 최대 나이. 카메라 단절 시 skip.
const floorStillFreshWindow = 10 * time.Second

// floorStillMinLuma — 조도 게이트: 평균 휘도(0~255)가 이 값 미만이면 소등/야간으로 보고
// 저장하지 않는다 (yolo-lrcn-fusion 설계 3.1 — 초기값 40). JPEG 디코드 실패 프레임은
// 게이트를 적용하지 않고 저장한다 (판별 불가를 이유로 데이터를 잃지 않는다).
const floorStillMinLuma = 40.0

// FloorCamera — 수집 대상 수중캠.
type FloorCamera struct {
	TankID   string
	CameraID string
}

// FloorStillCollector — 수중캠 바닥 스틸 수집기.
type FloorStillCollector struct {
	sink EventSink
	log  *slog.Logger
	dir  string
	// cameras — position=underwater 카메라 목록 (매 호출 재평가 — 카메라 등록 동적 반영).
	cameras func(ctx context.Context) ([]FloorCamera, error)
	// grab — 카메라의 fresh 프레임 1장 (main 에서 api 프레임허브로 구현).
	grab func(ctx context.Context, cameraID string) (media.Frame, error)
}

// NewFloorStillCollector — dir 은 저장 루트 (<data_dir>/floor_stills 권장).
func NewFloorStillCollector(
	sink EventSink,
	dir string,
	cameras func(ctx context.Context) ([]FloorCamera, error),
	grab func(ctx context.Context, cameraID string) (media.Frame, error),
) *FloorStillCollector {
	return &FloorStillCollector{
		sink:    sink,
		log:     slog.With("service", "floor_still"),
		dir:     dir,
		cameras: cameras,
		grab:    grab,
	}
}

// CollectTank — 수조의 수중캠 스틸 수집 (사이클 훅용). 수중캠 없는 수조(트랙 T)는 no-op.
func (c *FloorStillCollector) CollectTank(ctx context.Context, tankID, trigger, cycleID string) {
	cams, err := c.cameras(ctx)
	if err != nil {
		c.log.Warn("floor still: camera list failed", "tank_id", tankID, "err", err)
		return
	}
	for _, cam := range cams {
		if cam.TankID != tankID {
			continue
		}
		if err := c.collect(ctx, cam, trigger, cycleID); err != nil {
			c.log.Warn("floor still collect failed",
				"tank_id", cam.TankID, "camera_id", cam.CameraID, "trigger", trigger, "err", err)
		}
	}
}

// Run — 정기 수집 루프. ctx 취소 시 종료.
func (c *FloorStillCollector) Run(ctx context.Context) {
	t := time.NewTicker(floorStillPeriodicInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cams, err := c.cameras(ctx)
			if err != nil {
				c.log.Warn("floor still: camera list failed", "err", err)
				continue
			}
			for _, cam := range cams {
				if err := c.collect(ctx, cam, "periodic", ""); err != nil {
					c.log.Warn("floor still collect failed",
						"tank_id", cam.TankID, "camera_id", cam.CameraID, "trigger", "periodic", "err", err)
				}
			}
		}
	}
}

// collect — 스틸 1장 저장 + 이벤트 적재.
func (c *FloorStillCollector) collect(ctx context.Context, cam FloorCamera, trigger, cycleID string) error {
	frame, err := c.grab(ctx, cam.CameraID)
	if err != nil {
		return err
	}
	if len(frame.Data) == 0 {
		return fmt.Errorf("empty frame")
	}
	if luma, lerr := meanLuma(frame.Data); lerr == nil && luma < floorStillMinLuma {
		c.log.Info("floor still skipped (dark)",
			"tank_id", cam.TankID, "camera_id", cam.CameraID, "trigger", trigger,
			"luma", fmt.Sprintf("%.1f", luma))
		return nil
	}
	capturedAt := frame.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	tankDir := filepath.Join(c.dir, cam.TankID)
	if err := os.MkdirAll(tankDir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s_%s.jpg", capturedAt.UTC().Format("20060102T150405"), trigger)
	path := filepath.Join(tankDir, name)
	if err := os.WriteFile(path, frame.Data, 0o644); err != nil {
		return err
	}
	stillID := strings.TrimSuffix(fmt.Sprintf("%s_%s", cam.TankID, name), ".jpg")
	payload := map[string]any{
		"still_id":    stillID,
		"tank_id":     cam.TankID,
		"camera_id":   cam.CameraID,
		"path":        path,
		"trigger":     trigger,
		"cycle_id":    cycleID,
		"captured_at": common.FormatTime(capturedAt),
	}
	if c.sink != nil {
		if _, err := c.sink.AppendEvent(ctx, "g3_pipeline", "vision", cam.CameraID,
			EventFloorStillStored, stillID, payload); err != nil {
			return fmt.Errorf("append floor still event: %w", err)
		}
	}
	c.log.Info("floor still stored",
		"tank_id", cam.TankID, "camera_id", cam.CameraID, "trigger", trigger, "path", path)
	return nil
}

// meanLuma — JPEG 평균 휘도(0~255). 8px 간격 샘플링으로 충분 (게이트 판별용).
func meanLuma(jpegData []byte) (float64, error) {
	img, err := jpeg.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return 0, err
	}
	b := img.Bounds()
	const stride = 8
	var sum float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y += stride {
		for x := b.Min.X; x < b.Max.X; x += stride {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
			n++
		}
	}
	if n == 0 {
		return 0, fmt.Errorf("empty image bounds")
	}
	return sum / float64(n), nil
}
