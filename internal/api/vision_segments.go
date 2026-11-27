package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
	"bluei.kr/edge/internal/vision_pipeline"
)

const (
	// prior 반응창 — 펄스 시작 기준 [+10s,+20s]. 앵커(config rise_anchor_by_camera)가
	// 이 창의 최고 상승폭으로 캘리브레이션되므로 창을 바꾸면 눈금도 함께 바꿔야 한다.
	//
	// ⚠ 2026-08-14 부터 feed_cycle 의 dischargePostStartS/EndS(23~30)와 **의도적으로
	// 다르다**. 판정 창만 먼저 옮겼다 — prior 를 같이 옮기면 앵커가 구 창 기준이라
	// 프리필 눈금이 어긋나고, 그 프리필이 운영자 라벨(정답지)을 오염시킨다.
	// 앵커를 새 창으로 재캘리브레이션한 뒤 이 값도 23~30 으로 맞출 것.
	priorWindowStartS = 10
	priorWindowEndS   = 20
)

// pulseCompletedPayload — feed.pulse.completed 파싱용 최소 필드.
type pulseCompletedPayload struct {
	CycleID          string `json:"cycle_id"`
	TankID           string `json:"tank_id"`
	PulseNo          int    `json:"pulse_no"`
	ActualDurationMs int    `json:"actual_duration_ms"`
	CompletedAt      string `json:"completed_at"`
}

// handleVisionSegments — GET /v1/vision/segments?cycle_id=...
//
// 급이 사이클의 펄스 타임스탬프(feed.pulse.completed)로부터 펄스 사이클 단위
// 학습 세그먼트를 유도해 반환한다(docs/50 1단계). 통클립을 물리 분할하지 않고
// 원본 대비 오프셋만 계산 — 캡처 파이프라인은 건드리지 않는 읽기 전용 조회.
//
// 응답: 세그먼트 절대 시각(카메라 무관) + 해당 사이클 급이 클립별 오프셋(ms).
func (s *Server) handleVisionSegments(w http.ResponseWriter, r *http.Request) {
	cycleID := r.URL.Query().Get("cycle_id")
	if cycleID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CYCLE_ID", "cycle_id query param required", "")
		return
	}

	// 1. 펄스 타임스탬프 조회(DB 단 cycle_id 필터 — 오래된 사이클도 비파편적).
	pulseEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:      events.EventFeedPulseCompleted,
		PayloadCycleID: cycleID,
		Limit:          200,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	var pulses []feed_cycle.PulseTiming
	var tankID string
	for _, e := range pulseEvents {
		var p pulseCompletedPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		completedAt, perr := time.Parse(time.RFC3339Nano, p.CompletedAt)
		if perr != nil {
			continue
		}
		if tankID == "" {
			tankID = p.TankID
		}
		pulses = append(pulses, feed_cycle.PulseTiming{
			PulseNo:          p.PulseNo,
			CompletedAt:      completedAt,
			ActualDurationMs: p.ActualDurationMs,
		})
	}

	// 세그먼트 기준 = 그 사이클의 (리드+공급+정지) 합. 다중 펄스면 DeriveSegments 가
	// 실측 간격으로 동적 산출하고, 단일 펄스일 때만 이 정책 합을 fallback 으로 쓴다
	// (사용자 지시 2026-07-06: 고정 아님, 상황 따라 변경되는 총합 기준).
	pulseSumMs := 0
	if tankID != "" {
		if pol, perr := s.store.GetEffectiveFeedingPolicy(r.Context(), tankID); perr == nil && pol != nil {
			pulseSumMs = (pol.FeederSpinupSec + pol.FeederRunSec + pol.FeederStopSec) * 1000
		}
	}

	segs := feed_cycle.DeriveSegments(pulses, pulseSumMs)
	if len(segs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"cycle_id": cycleID,
			"found":    false,
		})
		return
	}

	segJSON := make([]map[string]any, len(segs))
	for i, sg := range segs {
		segJSON[i] = map[string]any{
			"index":       sg.Index,
			"pulse_no":    sg.PulseNo,
			"start_abs":   sg.StartAbs.UTC().Format(time.RFC3339Nano),
			"end_abs":     sg.EndAbs.UTC().Format(time.RFC3339Nano),
			"duration_ms": sg.DurationMs,
		}
	}

	// 사전 채점된 2-head 모델 점수 (reporter 가 보고 직후 적재) — 프리필은 모델 점수
	// 우선, 없으면 요동 prior 폴백. 최신 1건만 사용 (재채점 시 최신이 정본).
	modelScores := map[int]float64{}
	modelVersion := ""
	if msEvs, mserr := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:      events.EventVisionSegmentScoresRecorded,
		PayloadCycleID: cycleID,
		Limit:          1,
	}); mserr == nil && len(msEvs) > 0 {
		var mp struct {
			ModelVersion string `json:"model_version"`
			Scores       []struct {
				PulseNo      int      `json:"pulse_no"`
				FeedingScore *float64 `json:"feeding_score"`
			} `json:"scores"`
		}
		if json.Unmarshal([]byte(msEvs[0].PayloadJSON), &mp) == nil {
			modelVersion = mp.ModelVersion
			for _, sc := range mp.Scores {
				if sc.FeedingScore != nil {
					modelScores[sc.PulseNo] = *sc.FeedingScore
				}
			}
		}
	}

	// 2단계: 클립별 요동곡선 조회 → clip_id 별 map. 세그먼트 창으로 slice 해 prior 분배.
	//
	// ⚠ 아래 두 조회는 이 사이클 것만 읽는다 (2026-09-17). 종전엔 각각 최근 2000건을
	// 통째로 읽어 초당 1샘플 시계열까지 전부 파싱했고, 이벤트 108만 건 시점에서 요청당
	// 6.5초·사이클 보고 직후 14.7초(앱 타임아웃 20초)가 됐다. 그 지연으로 평가 모달이
	// 펄스별 UI 를 못 받고 칩 폴백으로 떨어졌다(9-17 15:37 실측).
	// 이 사이클의 시간창 — 두 조회를 idx_events_type_time 범위 스캔으로 묶는다.
	winFrom := segs[0].StartAbs.Add(-1 * time.Hour)
	winTo := segs[len(segs)-1].EndAbs.Add(1 * time.Hour)

	agByClip := map[string]*agitationEventPayload{}
	agEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: vision_pipeline.EventAgitationRecorded, PayloadCycleID: cycleID, Limit: 50,
	})
	if err == nil && len(agEvents) == 0 {
		// payload 에 cycle_id 가 없던 옛 사이클 — 같은 시간창으로 조회한다.
		agEvents, err = s.store.QueryEvents(r.Context(), storage.EventFilter{
			EventType: vision_pipeline.EventAgitationRecorded,
			Since:     &winFrom, Until: &winTo, Limit: 50,
		})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	for _, e := range agEvents {
		var ap agitationEventPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &ap) == nil && ap.ClipID != "" {
			if _, seen := agByClip[ap.ClipID]; !seen {
				agByClip[ap.ClipID] = &ap
			}
		}
	}

	// 2. 해당 사이클의 급이 클립별 오프셋(오프셋 참조 = 원본 clip 대비 [start,end) ms)
	//    + 세그먼트별 요동 prior.
	// 사이클 시간창으로 좁힌다 — 종전엔 최근 2000건(급이 phase)을 통째로 읽어 파싱했다.
	// ⚠ evidence.cycle_id 로 거르면 안 된다: 희소 조건이라 LIMIT 을 못 채우고
	// media.clip.stored 26만 행을 전부 스캔하게 되는데, 순수 Go sqlite 드라이버에서는
	// 그 스캔이 10초를 넘는다 (9-17 실측 6.5s → 28s 악화). recorded_at 범위는
	// idx_events_type_time 을 그대로 타서 스캔 자체가 사라진다.
	// 클립 식별은 아래 루프의 evidence.cycle_id 비교가 그대로 담당한다.
	clipEvents, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType:     events.EventMediaClipStored,
		EvidencePhase: "feeding",
		Since:         &winFrom,
		Until:         &winTo,
		// 창에는 옆 사이클 클립도 들어온다(아래 루프에서 걸러진다) — 상한은 그 몫까지
		// 감안한다. 최근 60 사이클 실측 최대 12건이지만 카메라가 늘면 함께 늘고,
		// 잘리면 조용히 펄스별 UI 가 비므로 여유를 크게 잡는다(클립 payload 는 가볍다).
		Limit: 200,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}

	type clipRec struct {
		p     events.MediaClipStoredPayload
		start time.Time
	}
	var recs []clipRec
	for _, e := range clipEvents {
		var p events.MediaClipStoredPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if cid, _ := p.Evidence["cycle_id"].(string); cid != cycleID {
			continue
		}
		clipStart, perr := time.Parse(time.RFC3339Nano, p.StartedAt)
		if perr != nil {
			continue
		}
		recs = append(recs, clipRec{p: p, start: clipStart})
	}

	// prior 소스 선택 — 요동 prior 는 수면 지표라 비수중(탑뷰) 곡선이 정본.
	// 수중캠 곡선은 어체 근접 통과로 상승률이 포화(+200%↑ → prior 1.00 고정)돼 제안값으로
	// 무의미하다(2026-07-06 운영자 실측: 같은 사이클에서 탑뷰 0.80→0.20 감쇠 vs 수중 전부
	// 1.00). 사이클 내 비수중 클립의 곡선이 있으면 모든 클립의 세그먼트 prior 를 그 곡선으로
	// 계산하고, 출처가 다른 클립에는 prior_camera_id 로 명시한다. 비수중 곡선이 없으면 자기
	// 곡선을 쓰되 수중캠엔 prior_unreliable 를 표시한다. (카메라 프로필 view_angle 이 미입력
	// 배포라 camera_id 규약("underwater" 포함 여부)으로 구분.)
	var priorAg *agitationEventPayload
	var priorStart time.Time
	var priorCam string
	for _, rc := range recs {
		if strings.Contains(rc.p.CameraID, "underwater") {
			continue
		}
		if ag := agByClip[rc.p.ClipID]; ag != nil {
			priorAg, priorStart, priorCam = ag, rc.start, rc.p.CameraID
			break
		}
	}

	// prior 정규화 자 (rise_calibration.go segmentPriorScore) — prior 층에만 적용.
	// 앵커가 정본이고, 앵커 없는 카메라만 카메라 자기 기준 P90 으로 폴백한다.
	anchors := s.riseAnchorByCamera()
	riseP90s := s.riseP90ByCamera(r.Context())

	var clips []map[string]any
	for _, rc := range recs {
		p := rc.p
		ag := agByClip[p.ClipID] // 자기 곡선 (스파크라인/has_agitation 용)
		srcAg, srcStart, srcCam := ag, rc.start, p.CameraID
		unreliable := false
		if priorAg != nil {
			srcAg, srcStart, srcCam = priorAg, priorStart, priorCam
		} else if strings.Contains(p.CameraID, "underwater") {
			unreliable = ag != nil
		}
		offsets := make([]map[string]any, len(segs))
		for i, sg := range segs {
			startMs := sg.StartAbs.Sub(rc.start).Milliseconds()
			endMs := sg.EndAbs.Sub(rc.start).Milliseconds()
			seg := map[string]any{
				"index":           sg.Index,
				"pulse_no":        sg.PulseNo,
				"offset_start_ms": startMs,
				"offset_end_ms":   endMs,
				"duration_ms":     sg.DurationMs,
			}
			if ms, ok := modelScores[sg.PulseNo]; ok {
				seg["model_score"] = math.Round(ms*100) / 100
			}
			if srcAg != nil {
				// prior 인덱스는 prior 소스 클립의 시작 기준(클립 간 시작 시각 수 ms 차).
				pStartS := int(sg.StartAbs.Sub(srcStart).Milliseconds() / 1000)
				pEndS := int(sg.EndAbs.Sub(srcStart).Milliseconds() / 1000)
				_, peak, mean, n := vision_pipeline.SegmentPrior(
					srcAg.Series, srcAg.Baseline, pStartS, pEndS)
				if n > 0 {
					seg["seg_peak"] = math.Round(peak*100) / 100
					seg["seg_mean"] = math.Round(mean*100) / 100
					seg["seg_baseline"] = srcAg.Baseline
					if srcCam != p.CameraID {
						seg["prior_camera_id"] = srcCam
					}
					if unreliable {
						seg["prior_unreliable"] = true
					}
					// prior 는 세그먼트 전 구간이 아니라 **반응창**의 최고 상승폭으로
					// 잰다 (2026-08-05). 앵커가 반응창 기준으로 캘리브레이션됐으므로
					// 창이 넓으면 peak 이 앵커를 넘어 1.00 에 포화한다(실측 20펄스 중
					// 7개). 창까지 맞춰야 모달 prior = 화면 차트값 = 섀도 판정값이 된다.
					// 반응창이 곡선 밖이면 prior 는 표기하지 않는다(섀도가 판정을
					// 포기하는 것과 같은 조건). seg_peak/seg_mean 은 세그먼트 전
					// 구간 그대로 — 참고 표시용이라 창을 바꾸지 않는다.
					_, rPeak, _, rN := vision_pipeline.SegmentPrior(
						srcAg.Series, srcAg.Baseline,
						pStartS+priorWindowStartS, pStartS+priorWindowEndS)
					if rN > 0 {
						prior := segmentPriorScore(
							srcAg.Baseline, rPeak, anchors[srcCam], riseP90s[srcCam])
						seg["agitation_prior"] = math.Round(prior*100) / 100
					}
				}
			}
			offsets[i] = seg
		}
		clip := map[string]any{
			"clip_id":    p.ClipID,
			"camera_id":  p.CameraID,
			"started_at": p.StartedAt,
			"uri":        p.URI,
			"segments":   offsets,
		}
		if ag != nil {
			clip["has_agitation"] = true
		}
		clips = append(clips, clip)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cycle_id":      cycleID,
		"found":         true,
		"tank_id":       tankID,
		"model_version": modelVersion,
		"pulse_count":   len(segs),
		// 정책상 (리드+공급+정지) 합. 세그먼트 실제 길이는 각 segment.duration_ms 참조
		// (실측 간격 기반, 상황 따라 변동). 0 이면 정책 미설정 → 실측 간격만 사용.
		"policy_pulse_sum_ms": pulseSumMs,
		"segments":            segJSON,
		"clips":               clips,
	})
}
