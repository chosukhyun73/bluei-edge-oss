package feed_cycle

// Reporter — 사이클 완료 자동 보고 (2026-07-05, HANDOFF 9).
//
// finalizeCycle 훅에서 goroutine 으로 실행된다. 사이클 사실(feed_cycles 행)은 완료
// 시점에 이미 확정되지만, 비전 결과(LRCN observation·요동곡선)는 클립 finalize 후
// 비동기로 수초~수십초 뒤에 적재된다. 따라서 폴링으로 대기했다가 사이클 사실 +
// 비전 요약을 feed.cycle.report 이벤트 1건으로 종합 적재한다.
//
// Claude 세션 cron(임시 보고)을 대체하는 365일 무인 경로. 보고 실패/타임아웃은
// 있는 데이터만으로 확정하며 사이클 실행에는 어떤 영향도 주지 않는다.

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/inference"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

const (
	defaultReportPollInterval = 10 * time.Second
	// 요동 스크립트 타임아웃(120s) + LRCN 추론 여유. LRCN 미기동 등으로 비전 결과가
	// 끝내 안 오면 이 시간 후 있는 것만으로 보고한다.
	defaultReportMaxWait = 150 * time.Second
)

// reporterStore — Reporter 가 쓰는 storage 부분집합 (테스트 주입용).
type reporterStore interface {
	GetFeedCycle(ctx context.Context, cycleID string) (*storage.FeedCycle, error)
	QueryEvents(ctx context.Context, f storage.EventFilter) ([]*storage.Event, error)
	ListCameraProfiles(ctx context.Context, tankID string) ([]*storage.CameraProfile, error)
}

// reportSink — feed.cycle.report 적재. runtime.App 이 구현.
type reportSink interface {
	AppendEvent(ctx context.Context, module, adapter, deviceID, eventType, corrID string, payload any) (int64, error)
}

// segmentScorer — 2-head 세그먼트 모델 채점 (inference.LRCNClient 가 구현).
// 보고 후 탑뷰 클립의 펄스 세그먼트를 사전 채점해 모달 프리필용 이벤트로 적재한다.
type segmentScorer interface {
	SegmentScores(ctx context.Context, req inference.LRCNSegmentScoresRequest) (inference.LRCNSegmentScoresResponse, error)
}

// Reporter generates one feed.cycle.report event per completed cycle.
type Reporter struct {
	store reporterStore
	sink  reportSink
	log   *slog.Logger

	pollInterval time.Duration
	maxWait      time.Duration
	segScorer    segmentScorer // nil 이면 세그먼트 사전 채점 skip

	// 사후 채널 맥락 게이트 (2026-08-01). 라이브 판정과 같은 보정을 사후 채널에도
	// 걸어 두 채널이 같은 사이클을 다르게 기록하지 않게 한다. 종전에는 라이브만
	// 게이트를 타서, 같은 펄스가 라이브 unclear / 사후 no 로 갈렸다(운영자 지적).
	gateEnabled bool
	gateRelMax  float64
	gateVarMax  float64
	// 만복 전제조건 — 배출(yes)이 이 발수만큼 확인된 뒤에만 게이트 적용 (2026-08-02).
	gateMinConfirmed int

	// 섀도 중단 판정 v1 반응 임계 (2026-08-03). 수조별 덮어쓰기 → 공유 기본값 →
	// 코드 기본값 순으로 폴백.
	shadowStop       config.ShadowStopThresholds
	shadowStopByTank map[string]config.ShadowStopThresholds
	// 카메라별 반응 천장(2026-08-05). 비어 있으면 종전 rolling P90 스케일로 폴백.
	riseAnchorByCamera map[string]float64

	// 사이클 반응 지수 기준선 (2026-08-06, clip_index.go). 강도는 수조별 절대
	// 스케일이 달라 수조별로, 방향은 비율이라 공통으로 받는다. 미설정이면 지수 미산출.
	clipIndexLevelP25 map[string]float64
	clipIndexLevelP75 map[string]float64
	clipIndexDirLow   float64
	clipIndexDirHigh  float64
}

// SetShadowStop wires the v1 stop thresholds (worker 와 동일 config).
func (rp *Reporter) SetShadowStop(def config.ShadowStopThresholds,
	byTank map[string]config.ShadowStopThresholds) {
	rp.shadowStop, rp.shadowStopByTank = def, byTank
}

// SetRiseAnchors wires the per-camera response ceiling (config rise_anchor_by_camera).
func (rp *Reporter) SetRiseAnchors(anchors map[string]float64) {
	rp.riseAnchorByCamera = anchors
}

// SetClipIndex wires the cycle response index baselines (config clip_index_*).
func (rp *Reporter) SetClipIndex(levelP25, levelP75 map[string]float64, dirLow, dirHigh float64) {
	rp.clipIndexLevelP25, rp.clipIndexLevelP75 = levelP25, levelP75
	rp.clipIndexDirLow, rp.clipIndexDirHigh = dirLow, dirHigh
}

// SetCompensationGate wires the context gate thresholds (worker 와 동일 config).
func (rp *Reporter) SetCompensationGate(enabled bool, relMax, varMax float64, minConfirmed int) {
	rp.gateEnabled, rp.gateRelMax, rp.gateVarMax = enabled, relMax, varMax
	rp.gateMinConfirmed = minConfirmed
}

// NewReporter creates a Reporter. store 는 storage.Store, sink 는 runtime.App.
func NewReporter(store reporterStore, sink reportSink) *Reporter {
	return &Reporter{
		store:        store,
		sink:         sink,
		log:          slog.With("service", "feed_cycle_reporter"),
		pollInterval: defaultReportPollInterval,
		maxWait:      defaultReportMaxWait,
	}
}

// SetSegmentScorer wires the LRCN segment scorer (LRCN enabled 일 때만 호출됨).
func (rp *Reporter) SetSegmentScorer(s segmentScorer) { rp.segScorer = s }

// lrcnObservation — vision.observation.recorded 중 이 사이클 클립의 LRCN 스코어.
type lrcnObservation struct {
	CameraID     string  `json:"camera_id"`
	Score        float64 `json:"score"`
	ModelVersion string  `json:"model_version,omitempty"`
}

// agitationSummary — vision.agitation.recorded 중 이 사이클 클립의 요동 요약.
type agitationSummary struct {
	ClipID         string  `json:"clip_id"`
	CameraID       string  `json:"camera_id"`
	PeakRiseFrac   float64 `json:"peak_rise_frac"`
	CalmReached    bool    `json:"calm_reached"`
	StartleSuspect bool    `json:"startle_suspect"`
	NSeconds       int     `json:"n_seconds"`
	PeakAtS        int     `json:"peak_at_s"`
}

// OnCycleComplete waits for this cycle's vision results and appends one
// feed.cycle.report event. finalizeCycle 에서 goroutine 으로 호출.
func (rp *Reporter) OnCycleComplete(ctx context.Context, cycleID string) {
	row, err := rp.store.GetFeedCycle(ctx, cycleID)
	if err != nil || row == nil {
		rp.log.Warn("cycle report: load cycle failed", "cycle_id", cycleID, "error", err)
		return
	}
	since := row.StartedAt

	var (
		clipCount int
		obs       []lrcnObservation
		ag        []agitationSummary
	)
	deadline := time.Now().Add(rp.maxWait)
	for {
		clipCount = rp.countFeedingClips(ctx, cycleID, since)
		obs = rp.collectLRCN(ctx, cycleID, since)
		ag = rp.collectAgitation(ctx, cycleID, since)
		// 모든 클립의 요동+LRCN 이 도착하면 조기 확정. 클립 0(캡처 비활성/실패)이거나
		// LRCN 미기동으로 영영 못 채우면 deadline 에 있는 것만으로 확정.
		if clipCount > 0 && len(ag) >= clipCount && len(obs) >= clipCount {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(rp.pollInterval):
		}
	}

	completedAt := common.NowUTC()
	if row.CompletedAt != nil {
		completedAt = *row.CompletedAt
	}

	shadowJudge, dischargeCheck := rp.shadowJudgeBlock(ctx, row.CycleID, row.TankID, since, row.PulsesExecuted)

	// 세그먼트 사전 채점 — 모달 프리필 + clip 지수의 입력. 보고보다 **앞에서** 돈다
	// (2026-08-08). 타임아웃 30s 이고 실패하면 nil 이라 보고는 어떤 경우에도 나간다.
	segScores := rp.scoreSegments(ctx, row, since)

	payload := map[string]any{
		"report_id":       common.NewID("fcr"),
		"cycle_id":        row.CycleID,
		"tank_id":         row.TankID,
		"mode":            row.Mode,
		"priority":        row.Priority,
		"started_at":      common.FormatTime(row.StartedAt),
		"completed_at":    common.FormatTime(completedAt),
		"duration_s":      int(completedAt.Sub(row.StartedAt).Seconds()),
		"pulses_executed": row.PulsesExecuted,
		// 실제 사용한 펄스 간격 — 2026-10-01 부터 수온에 따라 달라진다(저수온일수록 길다).
		// "간격 끝 복귀도" 같은 지표는 간격이 길면 더 가라앉은 것처럼 보이므로,
		// 사이클 간 비교 시 이 값으로 정규화해야 한다.
		"avg_gap_ms":            rp.avgGapMs(ctx, row.CycleID),
		"total_amount_g":        row.TotalAmountG,
		"actual_total_amount_g": row.ActualTotalAmountG,
		"target_amount_g":       row.TargetAmountG,
		"termination_reason":    row.TerminationReason,
		"clip_count":            clipCount,
		"lrcn":                  lrcnBlock(obs),
		"agitation":             agitationBlock(ag),
		"shadow_judge":          shadowJudge,
		"discharge_check":       dischargeCheck,
		"clip_index":            rp.clipIndexBlock(ctx, row.CycleID, row.TankID, segScores, dischargeVerdictsOf(dischargeCheck)),
		"compensation":          rp.compensationBlock(ctx, row.CycleID, since),
		"generated_at":          common.FormatTime(common.NowUTC()),
	}

	if _, err := rp.sink.AppendEvent(ctx, "feed_cycle", "", row.TankID,
		events.EventFeedCycleReport, row.CycleID, payload); err != nil {
		rp.log.Warn("cycle report: append event failed", "cycle_id", cycleID, "error", err)
		return
	}
	rp.log.Info("cycle report recorded",
		"cycle_id", cycleID, "tank_id", row.TankID,
		"clip_count", clipCount, "lrcn_observations", len(obs), "agitation_curves", len(ag))

	rp.hopperAlerts(ctx, row, dischargeCheck)
	rp.patternWeakAlert(ctx, row, dischargeCheck)
}

// scoreSegments — 탑뷰 급이 클립의 펄스 세그먼트를 2-head 모델(feeding head)로
// 채점해 vision.segment.model_scores.recorded 1건으로 적재하고, 펄스별 점수를
// 돌려준다. 모달/② 탭 프리필 + clip 지수용.
//
// ⚠ 2026-08-08 부터 **보고 적재 앞에서** 호출한다. clip 지수가 요동 곡선 대신 r3
// 점수를 쓰게 되어(운영자 결정) 보고 payload 를 만들기 전에 점수가 있어야 한다.
// 종전에는 "LRCN 이 죽어도 보고는 남게" 하려고 뒤에서 돌렸는데, 그 속성은
// 아래 두 장치로 결과적으로 보존한다.
//
//	· 추론 타임아웃 30s (실측 최대 18.1s · 평균 9.5s, 292건) — 무한정 안 막는다
//	· 어떤 실패도 nil 을 돌려줄 뿐 보고를 중단시키지 않는다 → clip_index 만 비고 보고는 나간다
//
// 반환: pulse_no → feeding_score. 채점 불가면 nil.
func (rp *Reporter) scoreSegments(ctx context.Context, row *storage.FeedCycle, since time.Time) map[int]float64 {
	if rp.segScorer == nil {
		return nil
	}
	// 펄스 타이밍 → 세그먼트 창 (vision_segments API 와 동일 소스: feed.pulse.completed).
	pulseEvents, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType:      events.EventFeedPulseCompleted,
		PayloadCycleID: row.CycleID,
		Limit:          200,
	})
	if err != nil {
		rp.log.Warn("segment scores: query pulses failed", "cycle_id", row.CycleID, "error", err)
		return nil
	}
	var pulses []PulseTiming
	for _, e := range pulseEvents {
		var p struct {
			PulseNo          int    `json:"pulse_no"`
			CompletedAt      string `json:"completed_at"`
			ActualDurationMs int    `json:"actual_duration_ms"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		completedAt, perr := time.Parse(time.RFC3339Nano, p.CompletedAt)
		if perr != nil {
			continue
		}
		pulses = append(pulses, PulseTiming{
			PulseNo: p.PulseNo, CompletedAt: completedAt, ActualDurationMs: p.ActualDurationMs,
		})
	}
	// 단일 펄스 fallback 은 정책 합이 필요하지만 reporter 는 정책 접근이 없다 —
	// 단일 펄스 사이클(희귀)은 skip (모달은 요동 prior 사용).
	segs := DeriveSegments(pulses, 0)
	if len(segs) == 0 {
		return nil
	}

	// 탑뷰 급이 클립 선택 (수중 제외 — 세그먼트 모델은 탑뷰 학습).
	clipEvs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType:     events.EventMediaClipStored,
		EvidencePhase: "feeding",
		Since:         &since,
		Limit:         200,
	})
	if err != nil {
		rp.log.Warn("segment scores: query clips failed", "cycle_id", row.CycleID, "error", err)
		return nil
	}
	var clip *events.MediaClipStoredPayload
	var clipStart time.Time
	for _, e := range clipEvs {
		var p events.MediaClipStoredPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if cid, _ := p.Evidence["cycle_id"].(string); cid != row.CycleID {
			continue
		}
		if strings.Contains(p.CameraID, "underwater") {
			continue
		}
		st, perr := time.Parse(time.RFC3339Nano, p.StartedAt)
		if perr != nil {
			continue
		}
		clip, clipStart = &p, st
		break
	}
	if clip == nil {
		rp.log.Info("segment scores: no top-view feeding clip; skip", "cycle_id", row.CycleID)
		return nil
	}

	windows := make([]inference.LRCNSegmentWindow, len(segs))
	for i, sg := range segs {
		windows[i] = inference.LRCNSegmentWindow{
			PulseNo:       sg.PulseNo,
			OffsetStartMs: int(sg.StartAbs.Sub(clipStart).Milliseconds()),
			OffsetEndMs:   int(sg.EndAbs.Sub(clipStart).Milliseconds()),
		}
	}

	sctx, cancel := context.WithTimeout(ctx, clipIndexScoreTimeout)
	defer cancel()
	resp, err := rp.segScorer.SegmentScores(sctx, inference.LRCNSegmentScoresRequest{
		ClipPath: clip.URI,
		Segments: windows,
		TankID:   row.TankID,
	})
	if err != nil {
		rp.log.Warn("segment scores: inference failed", "cycle_id", row.CycleID, "error", err)
		return nil
	}

	scores := make([]map[string]any, 0, len(resp.Scores))
	ordered := make([]*float64, 0, len(resp.Scores))
	for _, s := range resp.Scores {
		m := map[string]any{"pulse_no": s.PulseNo}
		if s.FeedingScore != nil {
			m["feeding_score"] = math.Round(*s.FeedingScore*1000) / 1000
		}
		if s.Error != "" {
			m["error"] = s.Error
		}
		scores = append(scores, m)
		ordered = append(ordered, s.FeedingScore)
	}
	payload := map[string]any{
		"cycle_id":      row.CycleID,
		"tank_id":       row.TankID,
		"clip_id":       clip.ClipID,
		"camera_id":     clip.CameraID,
		"model_version": resp.ModelVersion,
		"scores":        scores,
		"inference_ms":  resp.InferenceMs,
	}
	// 섀도 중단 판정 v2 — 여기서 붙이는 이유는 r3 펄스별 점수가 이 시점에만 있기
	// 때문이다. 채점은 보고 적재 **뒤에** 돌아서(LRCN 이 죽어도 보고는 남게) 사이클
	// 보고 payload 에는 넣을 수 없다. 검토 하네스는 이 이벤트를 조인해서 읽는다.
	if v2 := shadowStopV2Block(ordered); v2 != nil {
		payload["shadow_stop_v2"] = v2
	}
	if _, err := rp.sink.AppendEvent(ctx, "feed_cycle", "", row.TankID,
		events.EventVisionSegmentScoresRecorded, row.CycleID, payload); err != nil {
		rp.log.Warn("segment scores: append event failed", "cycle_id", row.CycleID, "error", err)
		return nil
	}
	rp.log.Info("segment model scores recorded",
		"cycle_id", row.CycleID, "model_version", resp.ModelVersion,
		"segments", len(scores), "inference_ms", resp.InferenceMs)

	out := make(map[int]float64, len(resp.Scores))
	for _, sc := range resp.Scores {
		if sc.FeedingScore != nil {
			out[sc.PulseNo] = *sc.FeedingScore
		}
	}
	return out
}

// lrcnBlock — 보고 payload 의 lrcn 요약 (없으면 nil → JSON null).
func lrcnBlock(obs []lrcnObservation) map[string]any {
	if len(obs) == 0 {
		return nil
	}
	sum := 0.0
	for _, o := range obs {
		sum += o.Score
	}
	return map[string]any{
		"mean_score": math.Round(sum/float64(len(obs))*1000) / 1000,
		"per_camera": obs,
	}
}

// agitationBlock — 보고 payload 의 요동 요약 (없으면 nil → JSON null).
func agitationBlock(ag []agitationSummary) map[string]any {
	if len(ag) == 0 {
		return nil
	}
	maxRise := 0.0
	anyStartle := false
	for _, a := range ag {
		if a.PeakRiseFrac > maxRise {
			maxRise = a.PeakRiseFrac
		}
		anyStartle = anyStartle || a.StartleSuspect
	}
	return map[string]any{
		"max_peak_rise_frac": math.Round(maxRise*1000) / 1000,
		"startle_suspect":    anyStartle,
		"per_camera":         ag,
	}
}

// countFeedingClips — 이 사이클의 신호게이팅 급이 클립 수 (media.clip.stored,
// evidence.phase=feeding + evidence.cycle_id 매칭). 카메라 수만큼 생긴다.
func (rp *Reporter) countFeedingClips(ctx context.Context, cycleID string, since time.Time) int {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType:     events.EventMediaClipStored,
		EvidencePhase: "feeding",
		Since:         &since,
		Limit:         200,
	})
	if err != nil {
		rp.log.Warn("cycle report: query clips failed", "cycle_id", cycleID, "error", err)
		return 0
	}
	n := 0
	for _, e := range evs {
		var p events.MediaClipStoredPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if cid, _ := p.Evidence["cycle_id"].(string); cid == cycleID {
			n++
		}
	}
	return n
}

// collectLRCN — 이 사이클 클립의 LRCN feeding_activity 스코어들.
func (rp *Reporter) collectLRCN(ctx context.Context, cycleID string, since time.Time) []lrcnObservation {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventVisionObservationRecorded,
		Since:     &since,
		Limit:     200,
	})
	if err != nil {
		rp.log.Warn("cycle report: query lrcn observations failed", "cycle_id", cycleID, "error", err)
		return nil
	}
	var out []lrcnObservation
	for _, e := range evs {
		var p events.VisionObservationPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if cid, _ := p.Evidence["cycle_id"].(string); cid != cycleID {
			continue
		}
		out = append(out, lrcnObservation{
			CameraID:     p.CameraID,
			Score:        p.Scores["feeding_activity"],
			ModelVersion: p.ModelVersion,
		})
	}
	return out
}

// compensationBlock — gap 끝 라이브 판정(feed.pulse.discharge_judged) 요약.
// 판정 이벤트가 없으면 nil (기능 비활성/판정 불가 사이클).
func (rp *Reporter) compensationBlock(ctx context.Context, cycleID string, since time.Time) map[string]any {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType: "feed.pulse.discharge_judged",
		Since:     &since,
		Limit:     50,
	})
	if err != nil {
		rp.log.Warn("cycle report: query discharge judgments failed", "cycle_id", cycleID, "error", err)
		return nil
	}
	type judged struct {
		CycleID string  `json:"cycle_id"`
		PulseNo int     `json:"pulse_no"`
		Verdict string  `json:"verdict"`
		Granted bool    `json:"compensation_granted"`
		Deduct  float64 `json:"deducted_g"`
		// 보상 정밀 게이트 결과 (2026-07-30). nil = 게이트 미평가.
		// false = 게이트가 "배출된 것으로 보인다"고 판단해 보상을 보류 → 화면·모달은
		// 이 펄스를 미급이로 표시하면 안 된다 (verdict 자체는 기록 보존을 위해 no 유지).
		GatePass *bool `json:"gate_pass,omitempty"`
	}
	perPulse := []map[string]any{}
	grantedTotal := 0
	deductedG := 0.0
	for _, e := range evs {
		var p judged
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.CycleID != cycleID {
			continue
		}
		row := map[string]any{
			"pulse_no": p.PulseNo,
			"verdict":  p.Verdict,
			"granted":  p.Granted,
		}
		if p.GatePass != nil {
			row["gate_pass"] = *p.GatePass
		}
		perPulse = append(perPulse, row)
		if p.Granted {
			grantedTotal++
			deductedG += p.Deduct
		}
	}
	if len(perPulse) == 0 {
		return nil
	}
	return map[string]any{
		"channel":       "live_agitation_gap_end",
		"per_pulse":     perPulse,
		"granted_total": grantedTotal,
		"deducted_g":    deductedG,
	}
}

// collectAgitation — 이 사이클 클립의 요동곡선 요약들 (곡선 series 는 제외 — 원본은
// vision.agitation.recorded 이벤트에 남아 있고 GET /v1/vision/agitation 으로 조회).
func (rp *Reporter) collectAgitation(ctx context.Context, cycleID string, since time.Time) []agitationSummary {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded,
		Since:     &since,
		Limit:     200,
	})
	if err != nil {
		rp.log.Warn("cycle report: query agitation failed", "cycle_id", cycleID, "error", err)
		return nil
	}
	var out []agitationSummary
	for _, e := range evs {
		var p struct {
			ClipID      string  `json:"clip_id"`
			CycleID     string  `json:"cycle_id"`
			CameraID    string  `json:"camera_id"`
			NSeconds    int     `json:"n_seconds"`
			Baseline    float64 `json:"baseline"`
			Peak        float64 `json:"peak"`
			PeakAtS     int     `json:"peak_at_s"`
			CalmReached bool    `json:"calm_reached"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.CycleID != cycleID {
			continue
		}
		out = append(out, agitationSummary{
			ClipID:         p.ClipID,
			CameraID:       p.CameraID,
			PeakRiseFrac:   math.Round(vision_pipeline.PeakRiseFrac(p.Baseline, p.Peak)*1000) / 1000,
			CalmReached:    p.CalmReached,
			StartleSuspect: vision_pipeline.StartleSuspect(p.CalmReached, p.NSeconds, p.PeakAtS),
			NSeconds:       p.NSeconds,
			PeakAtS:        p.PeakAtS,
		})
	}
	return out
}

// avgGapMs — 이 사이클에서 실제로 쓰인 펄스 간격의 중앙값(ms). 펄스가 2발 미만이면 0.
//
// 왜 기록하는가: 2026-10-01 부터 간격이 수온에 따라 달라진다(저수온일수록 길다,
// 13.5℃ 77초 → 6℃ 129초). "간격 끝 복귀도" 처럼 간격 길이에 민감한 지표는 사이클 간
// 직접 비교가 안 되므로, 사후 정규화에 쓸 수 있도록 실제 값을 남긴다.
// 계획값이 아니라 실측 간격을 쓰는 이유는 리드타임·재전송으로 실제가 달라지기 때문이다.
func (rp *Reporter) avgGapMs(ctx context.Context, cycleID string) int {
	evs, err := rp.store.QueryEvents(ctx, storage.EventFilter{
		EventType:      events.EventFeedPulseStarted,
		PayloadCycleID: cycleID,
		Limit:          100,
	})
	if err != nil || len(evs) < 2 {
		return 0
	}
	ts := make([]time.Time, 0, len(evs))
	for _, e := range evs {
		ts = append(ts, e.RecordedAt)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	gaps := make([]int, 0, len(ts)-1)
	for i := 1; i < len(ts); i++ {
		if d := int(ts[i].Sub(ts[i-1]).Milliseconds()); d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Ints(gaps)
	return gaps[len(gaps)/2]
}
