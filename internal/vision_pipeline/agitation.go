package vision_pipeline

// 수면 요동(먹이반응 프록시) 곡선 — scripts/agitation_curve.py 실행 결과를
// vision.agitation.recorded 이벤트로 적재한다.
//
// 신호게이팅 급이 클립은 공급모터 리드(~11s)의 "급이 전" 구간으로 시작하므로
// 클립 스스로 기저(baseline)를 갖는다. 곡선의 피크→감쇠→진정이 LRCN A1(먹이반응)
// 헤드의 학습 타깃 원형이 된다 (2026-07-04 설계).

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"bluei.kr/edge/internal/capture"
	"bluei.kr/edge/internal/common"
)

// EventAgitationRecorded — 요동 곡선 이벤트 타입.
const EventAgitationRecorded = "vision.agitation.recorded"

// agitationScriptTimeout — 1fps 디코드 기준 실측 ~1s/3.5분 클립. 백스톱 20분 클립 여유.
const agitationScriptTimeout = 120 * time.Second

// AgitationMetrics — agitation_curve.py 의 JSON 출력.
type AgitationMetrics struct {
	NSeconds         int       `json:"n_seconds"`
	Baseline         float64   `json:"baseline"`
	Peak             float64   `json:"peak"`
	PeakAtS          int       `json:"peak_at_s"`
	Last5Mean        float64   `json:"last5_mean"`
	CalmReached      bool      `json:"calm_reached"`
	AUCAboveBaseline float64   `json:"auc_above_baseline"`
	Series           []float64 `json:"series"`
	Error            string    `json:"error,omitempty"`
}

// AgitationEvent — vision.agitation.recorded payload 의 게이트/집계용 필드.
type AgitationEvent struct {
	ClipID   string  `json:"clip_id"`
	CycleID  string  `json:"cycle_id"`
	TankID   string  `json:"tank_id"`
	CameraID string  `json:"camera_id"`
	Baseline float64 `json:"baseline"`
	Peak     float64 `json:"peak"`
}

// ParseAgitationPayload — 요동 이벤트 payload JSON 파싱 (소비처 공용).
func ParseAgitationPayload(payloadJSON string) (AgitationEvent, error) {
	var e AgitationEvent
	err := json.Unmarshal([]byte(payloadJSON), &e)
	return e, err
}

func parseAgitationOutput(out []byte) (*AgitationMetrics, error) {
	var m AgitationMetrics
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("parse agitation output: %w", err)
	}
	if m.Error != "" {
		return nil, fmt.Errorf("agitation script: %s", m.Error)
	}
	if m.NSeconds <= 0 || len(m.Series) == 0 {
		return nil, fmt.Errorf("agitation output empty (n=%d)", m.NSeconds)
	}
	return &m, nil
}

// AgitationClipMeta — 요동 곡선 이벤트 적재에 필요한 클립 메타.
type AgitationClipMeta struct {
	ClipID     string
	CycleID    string
	TankID     string
	CameraID   string
	MP4Path    string
	CapturedAt string // RFC3339. 없으면 "".
}

// runAgitationScript — agitation_curve.py 실행 + 파싱.
func runAgitationScript(mp4Path string) (*AgitationMetrics, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agitationScriptTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "python3", "scripts/agitation_curve.py",
		"--clip", mp4Path).Output()
	if err != nil {
		return nil, err
	}
	return parseAgitationOutput(out)
}

// PeakRiseFrac — 상대 피크 상승률 = (peak − baseline) / baseline. 자기 기저 대비 요동
// 증가폭. baseline≤0 이면 0. (api 제안점수·사이클 보고가 공유 — 공식 단일화.)
func PeakRiseFrac(baseline, peak float64) float64 {
	if baseline <= 0 {
		return 0
	}
	return (peak - baseline) / baseline
}

// StartleSuspect — 놀람·사람오염 의심 = 진정 미도달 + 피크가 클립 말미(80% 이후) 발생.
// 급이 반응은 사료 낙하 직후 피크→감쇠하지만, 사람 접근 오염은 말미 급상승 후 진정 안 됨.
// 강한 지속 급이반응(피크 중후반) 오탐 방지로 80% 보수화 (api 배지와 공식 단일화).
func StartleSuspect(calmReached bool, nSeconds, peakAtS int) bool {
	return !calmReached && nSeconds > 0 && float64(peakAtS) >= 0.8*float64(nSeconds)
}

// SegmentPrior — 요동 Series 의 [startSec,endSec) 구간(펄스 세그먼트 창)에서 제안점수.
//
// docs/50 2단계: 통클립 요동곡선을 펄스 세그먼트 창별로 잘라 각 세그먼트에 prior 분배.
// prior = clamp(PeakRiseFrac(baseline, segPeak), 0, 1). baseline 은 클립 전체 기저(요동
// 이벤트의 baseline)를 공유 — 세그먼트별 상대 상승을 같은 기준으로 비교해 펄스 간 하강
// 추세를 보존한다. Series 는 초 단위(클립 시작 정렬)이므로 offset_ms/1000 이 곧 인덱스.
//
// 반환: prior(0~1), segPeak(구간 최대), segMean(구간 평균), n(구간 표본 수).
// 구간이 비거나 Series 밖이면 n=0, 나머지 0.
func SegmentPrior(series []float64, baseline float64, startSec, endSec int) (prior, segPeak, segMean float64, n int) {
	if len(series) == 0 {
		return 0, 0, 0, 0
	}
	if startSec < 0 {
		startSec = 0
	}
	if endSec > len(series) {
		endSec = len(series)
	}
	if endSec <= startSec {
		return 0, 0, 0, 0
	}
	segPeak = series[startSec]
	sum := 0.0
	for i := startSec; i < endSec; i++ {
		if series[i] > segPeak {
			segPeak = series[i]
		}
		sum += series[i]
	}
	n = endSec - startSec
	segMean = sum / float64(n)
	prior = PeakRiseFrac(baseline, segPeak)
	if prior < 0 {
		prior = 0
	} else if prior > 1 {
		prior = 1
	}
	return prior, segPeak, segMean, n
}

// agitationPayload — 요동 이벤트 payload. recordAgitation 과 backfill 이 공유(포맷 단일화).
func agitationPayload(meta AgitationClipMeta, m *AgitationMetrics) map[string]any {
	return map[string]any{
		"clip_id":            meta.ClipID,
		"cycle_id":           meta.CycleID,
		"tank_id":            meta.TankID,
		"camera_id":          meta.CameraID,
		"mp4_path":           meta.MP4Path,
		"captured_at":        meta.CapturedAt,
		"n_seconds":          m.NSeconds,
		"baseline":           m.Baseline,
		"peak":               m.Peak,
		"peak_at_s":          m.PeakAtS,
		"last5_mean":         m.Last5Mean,
		"calm_reached":       m.CalmReached,
		"auc_above_baseline": m.AUCAboveBaseline,
		"series":             m.Series,
	}
}

// BackfillAgitation — 이미 저장된 과거 클립에 요동 곡선을 소급 계산·적재한다.
// 파이프라인 실가동 이전 급이 클립용. 라이브 recordAgitation 과 동일 payload·경로.
func BackfillAgitation(ctx context.Context, sink EventSink, meta AgitationClipMeta) error {
	m, err := runAgitationScript(meta.MP4Path)
	if err != nil {
		return err
	}
	_, err = sink.AppendEvent(ctx, "g3_pipeline", "vision", meta.CameraID,
		EventAgitationRecorded, meta.ClipID, agitationPayload(meta, m))
	return err
}

// recordAgitation — 클립 하나의 요동 곡선 계산 + 이벤트 적재. goroutine 에서 호출.
func (p *Pipeline) recordAgitation(r *capture.Result) {
	log := p.log.With("cycle_id", r.CycleID, "tank_id", r.TankID, "camera_id", r.CameraID)

	m, err := runAgitationScript(r.MP4Path)
	if err != nil {
		log.Warn("agitation curve script failed", "error", err)
		return
	}
	meta := AgitationClipMeta{
		ClipID: r.ClipID, CycleID: r.CycleID, TankID: r.TankID,
		CameraID: r.CameraID, MP4Path: r.MP4Path, CapturedAt: common.FormatTime(r.CapturedAt),
	}
	if _, err := p.sink.AppendEvent(context.Background(), "g3_pipeline", "vision", r.CameraID,
		EventAgitationRecorded, r.ClipID, agitationPayload(meta, m)); err != nil {
		log.Warn("append agitation event failed", "error", err)
		return
	}
	log.Info("agitation curve recorded",
		"clip_id", r.ClipID, "n_seconds", m.NSeconds,
		"baseline", m.Baseline, "peak", m.Peak, "peak_at_s", m.PeakAtS,
		"calm_reached", m.CalmReached)
}
