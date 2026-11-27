package api

// AI↔운영자 수렴 지표 (2026-07-21 운영자 재정의) — "자율 & 결정" 탭 신뢰도의 실운영 근거.
//
// 급이 철학: 총량은 급이설정(BSF mode)으로 이미 결정, AI 는 반응 보고 잘 나눠 주고
// 안 먹으면 멈춘다. "정확한 급이"의 척도 = AI 판정값과 운영자 평가값의 격차(gap)가
// 얼마나 작고, 줄어드는가. 이 지표를 운영자에게 보여 자율 전환 판단을 돕는다.
//
// 원천: vision.observation.disputed 의 (operator_score, ai_score). ai_score 는 제출
// 시점 모달 프리필(r3 펄스평균 우선). 과거 이벤트(ai_score 필드 이전)는 reason 문자열의
// "gap=X.XX" 를 파싱해 ai = operator - gap 로 복원한다.

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// ConvergenceState — AI↔운영자 격차 수렴 지표.
type ConvergenceState struct {
	SampleCount int `json:"sample_count"` // 격차 계산에 쓴 라벨 수
	// RecentGap — 최근 gapWindow 라벨의 평균 |operator-ai|. nil=표본 부족.
	RecentGap *float64 `json:"recent_gap,omitempty"`
	// ConvergenceScore — 1 - RecentGap (0~1, 높을수록 수렴=정확). nil=표본 부족.
	ConvergenceScore *float64 `json:"convergence_score,omitempty"`
	// Trend — 최근 절반 vs 이전 절반 평균 격차 차이. 음수=줄어드는 중(개선). nil=표본 부족.
	Trend *float64 `json:"trend,omitempty"`
	// Direction — "improving"(격차↓) | "worsening"(격차↑) | "stable" | "insufficient".
	Direction string `json:"direction"`
	// Series — 스파크라인용 최근 라벨의 격차 시계열(오래된→최신). 부호 있는 (op-ai).
	Series []convergencePoint `json:"series,omitempty"`
	Notes  []string           `json:"notes,omitempty"`
}

type convergencePoint struct {
	At   string  `json:"at"`
	Gap  float64 `json:"gap"`  // |op-ai|
	Op   float64 `json:"op"`   // operator_score
	AI   float64 `json:"ai"`   // ai_score
	Diff float64 `json:"diff"` // op-ai (부호)
}

const (
	convergenceScanLimit = 200 // 최근 dispute 스캔 상한
	convergenceMaxSeries = 30  // 스파크라인 포인트 상한
	convergenceGapWindow = 8   // RecentGap 이동평균 창
	convergenceTrendEps  = 0.02
)

var gapReasonRe = regexp.MustCompile(`gap=(-?[0-9]*\.?[0-9]+)`)

// collectConvergenceState — tank 의 최근 dispute 에서 AI↔운영자 격차 지표 산출.
func (s *Server) collectConvergenceState(r *http.Request, tankID string) ConvergenceState {
	out := ConvergenceState{Direction: "insufficient", Notes: []string{}}

	evs, err := s.store.QueryEvents(r.Context(), storage.EventFilter{
		EventType: events.EventVisionObservationDisputed,
		Limit:     convergenceScanLimit,
	})
	if err != nil {
		out.Notes = append(out.Notes, "격차 이벤트 조회 실패")
		return out
	}

	// QueryEvents 는 최신순(sequence DESC). 같은 observation 을 여러 클라이언트(앱+브라우저)나
	// 재제출로 중복 dispute 하면 이중 계산되므로 observation_id 별 최신 1건만 채택
	// (training.countLabels 와 동일 dedup 패턴). 최신순으로 dedup 후 뒤집어 오래된→최신 시계열 구성.
	seen := map[string]bool{}
	var pts []convergencePoint
	for _, e := range evs {
		var p events.VisionObservationDisputedPayload
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.TankID != tankID {
			continue
		}
		if seen[p.ObservationID] {
			continue
		}
		op, ai, ok := extractOpAI(p)
		if !ok {
			continue
		}
		seen[p.ObservationID] = true
		pts = append(pts, convergencePoint{
			At: p.DisputedAt, Op: op, AI: ai,
			Diff: op - ai, Gap: math.Abs(op - ai),
		})
	}
	// pts 는 최신→오래된 — 뒤집어 오래된→최신.
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}

	out.SampleCount = len(pts)
	if len(pts) == 0 {
		out.Notes = append(out.Notes, "AI↔운영자 격차 표본 없음 — 사이클 평가 라벨 필요")
		return out
	}

	// 스파크라인 — 최근 convergenceMaxSeries 개.
	if len(pts) > convergenceMaxSeries {
		out.Series = pts[len(pts)-convergenceMaxSeries:]
	} else {
		out.Series = pts
	}

	// RecentGap — 최근 convergenceGapWindow 개 평균 |op-ai|.
	recent := lastN(pts, convergenceGapWindow)
	rg := meanGap(recent)
	out.RecentGap = &rg
	cs := math.Max(0, math.Min(1, 1-rg))
	out.ConvergenceScore = &cs

	// Trend — 최근 절반 vs 이전 절반 평균 격차. 음수=개선. 표본 4+ 에서만.
	if len(pts) >= 4 {
		half := len(pts) / 2
		older := meanGap(pts[:half])
		newer := meanGap(pts[half:])
		tr := newer - older
		out.Trend = &tr
		switch {
		case tr < -convergenceTrendEps:
			out.Direction = "improving"
		case tr > convergenceTrendEps:
			out.Direction = "worsening"
		default:
			out.Direction = "stable"
		}
	} else {
		out.Direction = "stable"
		out.Notes = append(out.Notes, "추세 판정에 표본 부족 (4건+ 필요)")
	}
	return out
}

// extractOpAI — dispute payload 에서 (operator_score, ai_score) 복원.
// ai_score 필드 우선, 없으면 reason 의 "gap=X.XX" 로 ai = op - gap 복원.
func extractOpAI(p events.VisionObservationDisputedPayload) (op, ai float64, ok bool) {
	if p.OperatorScore == nil {
		return 0, 0, false
	}
	op = *p.OperatorScore
	if p.AIScore != nil {
		return op, *p.AIScore, true
	}
	if m := gapReasonRe.FindStringSubmatch(p.Reason); m != nil {
		if gap, err := strconv.ParseFloat(m[1], 64); err == nil {
			// reason 의 gap = operator - ai (부호 있음, CycleCompletionDispute).
			return op, op - gap, true
		}
	}
	return 0, 0, false // ai 복원 불가 — 격차 계산 제외
}

func lastN(pts []convergencePoint, n int) []convergencePoint {
	if len(pts) <= n {
		return pts
	}
	return pts[len(pts)-n:]
}

func meanGap(pts []convergencePoint) float64 {
	if len(pts) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range pts {
		sum += p.Gap
	}
	return sum / float64(len(pts))
}
