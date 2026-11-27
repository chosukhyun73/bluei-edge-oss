// Package vision_pipeline — G-3 capture → LRCN → VisionObservation 적재.
//
// capture.Worker 가 만든 mp4 path 를 LRCN service 로 보내고 응답을
// VisionObservation event 로 적재한다. cycle_id 는 payload.Evidence map 에 넣어
// 후속 dispute (G-4) 가 cycle 과 observation 을 연결할 수 있게 한다.
//
// LRCN 호출 실패 / payload validation 실패는 cycle 진행을 막지 않는다 (warn 로깅).
// G-2 capture worker 의 실패 처리와 동일한 패턴.
package vision_pipeline

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/inference"
)

// LRCNScorer — capture clip 으로 feeding-activity score 계산. inference.LRCNClient 가 구현.
type LRCNScorer interface {
	Score(ctx context.Context, req inference.LRCNRequest) (inference.LRCNResponse, error)
}

// EventSink — VisionObservation event 적재. runtime.App 이 구현.
type EventSink interface {
	AppendEvent(ctx context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error)
}

// Pipeline — G-3 orchestrator.
type Pipeline struct {
	lrcn LRCNScorer
	sink EventSink
	log  *slog.Logger
}

// New — Pipeline 생성. lrcn / sink 둘 다 nil 이면 OnCaptureResult 는 no-op.
func New(lrcn LRCNScorer, sink EventSink) *Pipeline {
	return &Pipeline{
		lrcn: lrcn,
		sink: sink,
		log:  slog.With("service", "vision_pipeline"),
	}
}

// OnCaptureResult — capture.Worker 의 SetOnResult callback 으로 등록.
// capture goroutine 안에서 sync 호출됨. LRCN CPU 1~2s 소요.
func (p *Pipeline) OnCaptureResult(ctx context.Context, r *capture.Result) {
	if p == nil || r == nil || p.sink == nil {
		return
	}
	// 신호게이팅 급이 클립 — 수면 요동(먹이반응 프록시) 곡선을 비동기 계산·적재.
	// LRCN 과 독립 (모델 미기동이어도 학습 라벨 데이터는 쌓인다). 실패는 warn 만.
	if r.Reason == capture.ReasonFeedCycleGated {
		go p.recordAgitation(r)
	}
	if p.lrcn == nil {
		return
	}
	// LRCN(먹이반응) 라인은 탑뷰 전용 (2026-07-17 운영자 확정): 수중캠 클립은
	// LRCN 추론·학습에서 제외하고 별도 라인(세그먼트 채점 → 어체 마스킹 파일럿)
	// 으로만 쓴다. 요동곡선(위)은 전 카메라 유지 — 판정·prior 가 소비.
	if strings.Contains(r.CameraID, "underwater") {
		return
	}
	log := p.log.With("cycle_id", r.CycleID, "tank_id", r.TankID, "camera_id", r.CameraID)

	resp, err := p.lrcn.Score(ctx, inference.LRCNRequest{
		ClipPath: r.MP4Path,
		TankID:   r.TankID,
	})
	if err != nil {
		if errors.Is(err, inference.ErrLRCNModelNotReady) {
			log.Info("lrcn model not ready — skipping observation", "error", err)
			return
		}
		log.Warn("lrcn score failed", "error", err)
		return
	}

	score := resp.FeedingActivityScore
	payload := events.VisionObservationPayload{
		ObservationID: common.NewID("vis_obs"),
		CameraID:      r.CameraID,
		TankID:        r.TankID,
		Mode:          "enhanced",      // LRCN heavy inference
		Phase:         "feeding_start", // cycle 시작 시점 캡처
		ObservedAt:    common.FormatTime(common.NowUTC()),
		ClipRef:       r.MP4Path,
		ModelVersion:  resp.ModelVersion,
		Confidence:    &score,
		Scores:        map[string]float64{"feeding_activity": score},
		Evidence: map[string]any{
			"cycle_id":     r.CycleID,
			"inference_ms": resp.InferenceMs,
			"frame_count":  resp.FrameCount,
			"duration_s":   r.DurationS,
			"source":       "lrcn",
			"captured_at":  common.FormatTime(r.CapturedAt),
		},
		Quality: events.QualityOK,
	}

	if err := payload.Validate(); err != nil {
		log.Warn("vision observation payload invalid", "error", err)
		return
	}

	seq, err := p.sink.AppendEvent(ctx, "g3_pipeline", "vision", r.CameraID,
		events.EventVisionObservationRecorded, payload.ObservationID, payload)
	if err != nil {
		log.Warn("append vision observation event failed", "error", err)
		return
	}

	log.Info("vision observation recorded",
		"observation_id", payload.ObservationID,
		"feeding_activity_score", score,
		"model_version", resp.ModelVersion,
		"inference_ms", resp.InferenceMs,
		"sequence", seq,
		"elapsed_since_capture_ms", time.Since(r.CapturedAt).Milliseconds(),
	)
}
