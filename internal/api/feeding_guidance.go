package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 급이 판단 가이드 (2026-10-01, 운영자 설계).
//
// 왜: 수온이 급변하면 급이를 멈춰야 하는데 그 판단이 운영자 머릿속에만 있었다.
// 9-30 하루 -1.8℃ 뒤 10-01 새벽 +1.5℃/h 급등 — "절대 급이하면 안 되는 상황"인데
// 화면에는 수온 숫자만 있었다.
//
// 설계 원칙 (운영자 결정)
//   - 평상시에는 아무것도 띄우지 않는다. 늘 떠 있는 배너는 배경이 되어 아무도 안 본다.
//   - 결론만이 아니라 "언제부터 언제까지 어떻게 변했는지" 경로를 보여준다.
//   - 운영자가 확인하면 사라지고, 그 시점부터 다시 잰다. 이미 인지한 변화를 반복해서
//     띄우는 것은 소음이다. 중요한 건 "내가 본 뒤로 또 뭐가 달라졌나"다.
//   - 30분 창에서 0.5℃ 이상 변하면 즉시 잡는다. 일주일 실측에서 이 기준은 오탐 0 ·
//     10-01 새벽 사건만 정확히 잡았다(최대 0.78℃/30분, 평상시는 0.1℃ 수준).
const (
	guideRateWindow   = 30 * time.Minute // 급변 감지 창
	guideRateDeltaC   = 0.5              // 창 안에서 이만큼 변하면 급변
	guideCalmDeltaC   = 0.2              // 창 변화가 이 아래면 잠잠
	guideCalmHours    = 6                // 잠잠이 이만큼 지속되면 해제
	guideCautionC     = 1.0              // 누적 변화 — 감량
	guideHoldC        = 2.0              // 누적 변화 — 보류
	guideColdStopC    = 6.0
	guideColdCautionC = 9.0
	guideHotStopC     = 18.0
	guideAckTTL       = 12 * time.Hour // 확인 기준점 유효기간
	guideAckKeyPrefix = "feeding_guidance_ack:"
)

type guidancePoint struct {
	At    string  `json:"at"`
	TempC float64 `json:"temp_c"`
	Note  string  `json:"note,omitempty"`
}

type feedingGuidance struct {
	Level      string          `json:"level"`    // "ok" | "caution" | "hold"
	Headline   string          `json:"headline"` // 한 줄 결론 (level=ok 면 빈 문자열)
	Path       []guidancePoint `json:"path"`     // 변화 경로 — 언제 얼마였는지
	Reasons    []string        `json:"reasons"`  //
	TempNowC   *float64        `json:"temp_now_c"`
	BaselineAt string          `json:"baseline_at"`   // 기준 시각(확인 시점 또는 24시간 전)
	BaselineBy string          `json:"baseline_by"`   // "ack" | "24h"
	ElapsedH   float64         `json:"elapsed_hours"` // 급변 감지 후 경과
	Rate30mC   *float64        `json:"rate_30m_c"`    // 최근 30분 변화
	FactorPct  int             `json:"factor_pct"`    // 권고 급이량 비율
	Acked      bool            `json:"acked"`         // 현재 확인 상태인가
	Sticky     bool            `json:"sticky"`        // 확인해도 안 사라지는 항목(절대 한계)
}

type guidanceAck struct {
	At    time.Time `json:"at"`
	TempC float64   `json:"temp_c"`
}

// handleGroupFeedingGuidance — GET/POST /v1/groups/{group_id}/feeding-guidance.
//
// 수조가 아니라 그룹 단위다 (운영자 지적 2026-10-01): RAS 는 단일 순환수라 그룹 안
// 모든 수조가 같은 물을 쓴다. 실제로 4수조 수온이 항상 같은 값으로 들어온다 —
// 수조별로 같은 경고를 네 번 띄우는 것은 소음이다.
func (s *Server) handleGroupFeedingGuidance(w http.ResponseWriter, r *http.Request, groupID string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.buildFeedingGuidance(r.Context(), groupID))
	case http.MethodPost:
		s.ackFeedingGuidance(w, r, groupID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// groupTempTankID — 그룹의 수온을 대표하는 수조. 단일 순환수라 아무 수조나 같은 값이지만,
// 관측이 실제로 들어오는 수조를 고른다(센서 연결이 끊긴 수조를 피한다).
func (s *Server) groupTempTankID(ctx context.Context, groupID string) (string, bool) {
	profiles, err := s.store.ListTankProfiles(ctx)
	if err != nil {
		return "", false
	}
	for _, tp := range profiles {
		if tp.GroupID != groupID {
			continue
		}
		if _, ok := s.tankTempNow(ctx, tp.TankID); ok {
			return tp.TankID, true
		}
	}
	return "", false
}

// ackFeedingGuidance — 운영자 확인. 현재 수온을 새 기준점으로 저장하고 이벤트로 남긴다.
// 왜 이벤트까지 남기는가: 나중에 "그때 왜 급이를 멈췄나/재개했나"를 되짚을 수 있어야 한다.
func (s *Server) ackFeedingGuidance(w http.ResponseWriter, r *http.Request, groupID string) {
	tankID, okTank := s.groupTempTankID(r.Context(), groupID)
	if !okTank {
		writeError(w, http.StatusConflict, "NO_TEMP", "수온을 읽을 수 없어 확인 기준을 잡을 수 없습니다", "")
		return
	}
	now, ok := s.tankTempNow(r.Context(), tankID)
	if !ok {
		writeError(w, http.StatusConflict, "NO_TEMP", "수온을 읽을 수 없어 확인 기준을 잡을 수 없습니다", "")
		return
	}
	at := common.NowUTC()
	b, _ := json.Marshal(guidanceAck{At: at, TempC: now})
	if err := s.store.KVSet(r.Context(), guideAckKeyPrefix+groupID, string(b)); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error(), "")
		return
	}
	_, _ = s.app.AppendEvent(r.Context(), "operator", "", tankID,
		"feeding.guidance.acknowledged", "", map[string]any{
			"group_id":        groupID,
			"tank_id":         tankID,
			"temp_c":          math.Round(now*100) / 100,
			"acknowledged_at": common.FormatTime(at),
		})
	writeJSON(w, http.StatusOK, s.buildFeedingGuidance(r.Context(), groupID))
}

func (s *Server) buildFeedingGuidance(ctx context.Context, groupID string) feedingGuidance {
	g := feedingGuidance{Level: "ok", FactorPct: 100, Reasons: []string{}, Path: []guidancePoint{}}

	tankID, okTank := s.groupTempTankID(ctx, groupID)
	if !okTank {
		g.Level, g.FactorPct, g.Sticky = "hold", 0, true
		g.Headline = "수온을 읽을 수 없습니다 — 급이 보류"
		g.Reasons = append(g.Reasons, "이 그룹의 어느 수조에서도 수온 관측값이 들어오지 않습니다.")
		return g
	}
	tempNow, okNow := s.tankTempNow(ctx, tankID)
	if !okNow {
		g.Level, g.FactorPct, g.Sticky = "hold", 0, true
		g.Headline = "수온을 읽을 수 없습니다 — 급이 보류"
		g.Reasons = append(g.Reasons, "수온 관측값이 없어 급이량을 계산할 수 없습니다.")
		return g
	}
	g.TempNowC = &tempNow

	// 급변·안정 판정은 최근 8시간이면 충분하다(해제 조건이 6시간 지속).
	// 48시간을 통째로 끌면 센서 이벤트가 4만 건이라 상한에 잘려 오래된 쪽이 사라진다
	// (10-01 실측: limit 20000 으로는 6시간치만 들어와 24시간 전 기준점이 비었다).
	series := s.tankTempSeries(ctx, tankID, 8*time.Hour)
	ack := s.loadGuidanceAck(ctx, groupID)

	// 기준점 — 확인이 유효하면 그 시점, 아니면 24시간 전.
	baseAt := time.Now().UTC().Add(-24 * time.Hour)
	baseTemp, haveBase := s.tankTempAround(ctx, tankID, baseAt)
	g.BaselineBy = "24h"
	if ack != nil && time.Since(ack.At) < guideAckTTL {
		baseAt, baseTemp, haveBase = ack.At, ack.TempC, true
		g.BaselineBy, g.Acked = "ack", true
	}
	g.BaselineAt = common.FormatTime(baseAt)

	// 확인했다면 그 시점 이후만 본다. 운영자가 이미 본 급변을 계속 띄우는 것이 소음이었다.
	if g.Acked {
		series = sliceFrom(series, baseAt)
	}
	rate, haveRate := windowDelta(series, guideRateWindow)
	if haveRate {
		r := math.Round(rate*100) / 100
		g.Rate30mC = &r
	}
	spikeAt, haveSpike := lastSpike(series, guideRateWindow, guideRateDeltaC)
	calmFor := calmDuration(series, guideRateWindow, guideCalmDeltaC)

	var cum float64
	if haveBase {
		cum = tempNow - baseTemp
	}

	switch {
	case tempNow < guideColdStopC:
		g.Level, g.FactorPct, g.Sticky = "hold", 0, true
		g.Headline = "수온이 하한(6℃) 아래 — 급이 보류"
	case tempNow > guideHotStopC:
		g.Level, g.FactorPct, g.Sticky = "hold", 0, true
		g.Headline = "수온이 상한(18℃) 초과 — 급이 보류"
	case haveSpike && calmFor < time.Duration(guideCalmHours)*time.Hour:
		g.Level, g.FactorPct = "hold", 0
		g.Headline = "수온 급변 감지 — 급이 보류 권고"
		g.ElapsedH = math.Round(time.Since(spikeAt).Hours()*10) / 10
	case haveBase && math.Abs(cum) >= guideHoldC:
		g.Level, g.FactorPct = "hold", 0
		g.Headline = "수온이 크게 변했습니다 — 급이 보류 권고"
	case haveBase && math.Abs(cum) >= guideCautionC:
		g.Level, g.FactorPct = "caution", 50
		g.Headline = "수온 변화 — 평소의 절반으로 감량 권고"
	case tempNow < guideColdCautionC:
		g.Level, g.FactorPct = "caution", 100
		g.Headline = "저수온 운영 구간 — 양 감소·간격 연장이 자동 적용됩니다"
	}

	if g.Level == "ok" {
		return g // 평상시에는 아무것도 띄우지 않는다
	}

	// 변화 경로 — 기준 시각 · 급변 시점 · 지금.
	if haveBase {
		g.Path = append(g.Path, guidancePoint{
			At: common.FormatTime(baseAt), TempC: math.Round(baseTemp*100) / 100,
			Note: map[string]string{"ack": "확인 시점", "24h": "24시간 전"}[g.BaselineBy],
		})
	}
	if haveSpike {
		if t, ok := seriesAt(series, spikeAt); ok {
			g.Path = append(g.Path, guidancePoint{
				At: common.FormatTime(spikeAt), TempC: math.Round(t*100) / 100, Note: "급변 감지",
			})
		}
	}
	g.Path = append(g.Path, guidancePoint{
		At: common.FormatTime(common.NowUTC()), TempC: math.Round(tempNow*100) / 100, Note: "지금",
	})
	sort.Slice(g.Path, func(i, j int) bool { return g.Path[i].At < g.Path[j].At })

	if haveBase {
		dir := "상승"
		if cum < 0 {
			dir = "하강"
		}
		g.Reasons = append(g.Reasons,
			"기준 대비 "+trimF(math.Abs(cum))+"℃ "+dir)
	}
	if haveRate {
		g.Reasons = append(g.Reasons, "최근 30분 "+trimF(math.Abs(rate))+"℃ 변화")
	}
	if haveSpike {
		g.Reasons = append(g.Reasons,
			"급변 감지 후 "+trimF(g.ElapsedH)+"시간 경과 — 30분 변화가 "+
				trimF(guideCalmDeltaC)+"℃ 아래로 "+trimF(float64(guideCalmHours))+"시간 유지되면 해제됩니다")
	}
	if tempNow < guideColdCautionC && tempNow >= guideColdStopC {
		g.Reasons = append(g.Reasons, "9℃ 미만: 하루 2회로 축소 · 펄스 간격 연장 · 급이량 감소")
	}
	return g
}

func (s *Server) loadGuidanceAck(ctx context.Context, tankID string) *guidanceAck {
	raw, ok, err := s.store.KVGet(ctx, guideAckKeyPrefix+tankID)
	if err != nil || !ok || raw == "" {
		return nil
	}
	var a guidanceAck
	if json.Unmarshal([]byte(raw), &a) != nil {
		return nil
	}
	return &a
}

type tempPoint struct {
	at time.Time
	v  float64
}

// tankTempSeries — 최근 window 동안의 수조 수온 시계열(오름차순).
func (s *Server) tankTempSeries(ctx context.Context, tankID string, window time.Duration) []tempPoint {
	since := time.Now().UTC().Add(-window)
	evs, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		Since:     &since,
		Limit:     60000,
	})
	if err != nil {
		return nil
	}
	out := make([]tempPoint, 0, 512)
	for _, e := range evs {
		// ⚠ tank_id 는 최상위가 아니라 location 안에 있다 (10-01 실측 — 최상위로 읽으면 전부 탈락).
		var p struct {
			Metric   string   `json:"metric"`
			Value    *float64 `json:"value"`
			Location struct {
				TankID string `json:"tank_id"`
			} `json:"location"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if p.Metric != events.MetricWaterTemperature || p.Value == nil || p.Location.TankID != tankID {
			continue
		}
		out = append(out, tempPoint{at: e.RecordedAt, v: *p.Value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// seriesAt — 주어진 시각에 가장 가까운 관측값(±1시간).
func seriesAt(s []tempPoint, at time.Time) (float64, bool) {
	var best float64
	var bestD time.Duration = time.Hour
	found := false
	for _, p := range s {
		d := p.at.Sub(at)
		if d < 0 {
			d = -d
		}
		if d <= bestD {
			best, bestD, found = p.v, d, true
		}
	}
	return best, found
}

// windowDelta — 최근 window 동안의 변화량(끝 - 시작).
func windowDelta(s []tempPoint, window time.Duration) (float64, bool) {
	if len(s) < 2 {
		return 0, false
	}
	end := s[len(s)-1]
	for i := len(s) - 1; i >= 0; i-- {
		if end.at.Sub(s[i].at) >= window {
			return end.v - s[i].v, true
		}
	}
	return end.v - s[0].v, true
}

// lastSpike — 가장 최근에 창 변화가 임계를 넘은 시각.
func lastSpike(s []tempPoint, window time.Duration, deltaC float64) (time.Time, bool) {
	var last time.Time
	found := false
	i := 0
	for j := range s {
		for s[j].at.Sub(s[i].at) > window {
			i++
		}
		if math.Abs(s[j].v-s[i].v) >= deltaC {
			last, found = s[j].at, true
		}
	}
	return last, found
}

// calmDuration — 창 변화가 계속 calmC 미만이었던 가장 최근 지속 시간.
func calmDuration(s []tempPoint, window time.Duration, calmC float64) time.Duration {
	if len(s) < 2 {
		return 0
	}
	i := 0
	var calmFrom time.Time
	have := false
	for j := range s {
		for s[j].at.Sub(s[i].at) > window {
			i++
		}
		if math.Abs(s[j].v-s[i].v) >= calmC {
			have = false
			continue
		}
		if !have {
			calmFrom, have = s[j].at, true
		}
	}
	if !have {
		return 0
	}
	return s[len(s)-1].at.Sub(calmFrom)
}

func trimF(v float64) string {
	b, _ := json.Marshal(math.Round(v*10) / 10)
	return string(b)
}

func (s *Server) tankTempNow(ctx context.Context, tankID string) (float64, bool) {
	envs, err := s.store.ListTankEnvironment(ctx, tankID)
	if err != nil {
		return 0, false
	}
	for _, e := range envs {
		if e.Metric == events.MetricWaterTemperature && e.Value != nil {
			return *e.Value, true
		}
	}
	return 0, false
}

// tankTempAround — 특정 시각 ±1시간 창의 평균 수온. 기준점 조회용으로, 전체 시계열에
// 기대지 않는다(센서 이벤트가 많아 긴 창은 상한에 잘린다).
func (s *Server) tankTempAround(ctx context.Context, tankID string, at time.Time) (float64, bool) {
	from := at.Add(-time.Hour)
	to := at.Add(time.Hour)
	evs, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: events.EventSensorReadingRecorded,
		Since:     &from, Until: &to, Limit: 5000,
	})
	if err != nil {
		return 0, false
	}
	var sum float64
	var n int
	for _, e := range evs {
		var p struct {
			Metric   string   `json:"metric"`
			Value    *float64 `json:"value"`
			Location struct {
				TankID string `json:"tank_id"`
			} `json:"location"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		if p.Metric != events.MetricWaterTemperature || p.Value == nil || p.Location.TankID != tankID {
			continue
		}
		sum += *p.Value
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// sliceFrom — 주어진 시각 이후 구간만.
func sliceFrom(s []tempPoint, at time.Time) []tempPoint {
	for i, p := range s {
		if !p.at.Before(at) {
			return s[i:]
		}
	}
	return nil
}
