package haccp

import (
	"fmt"
	"sort"
)

// CCP 한계기준 판정 — 규칙 엔진. 측정값과 lot 이 그 CCP 범위 안에 있던 구간만 보고 판정한다.
// 순수 계산이다: 저장소·시계·LLM 을 쓰지 않는다. 시각은 UTC unix 밀리초.
// References: docs/54-haccp-process-flow-ccp-design.md §7.2, §26.

// Limit statuses.
const (
	StatusPending    = "pending"     // lot 이 아직 이 CCP 범위에 들어오지 않았다
	StatusOK         = "ok"          // 한계기준 안
	StatusInProgress = "in_progress" // 조건을 채우는 중(유지 시간)
	StatusNeedsInput = "needs_input" // 사람이 값을 넣어야 한다
	StatusDeviation  = "deviation"   // 한계기준 이탈
	StatusOutage     = "outage"      // 모니터링 불능(값이 들어오지 않음)
)

// Sample is one measured or entered value.
type Sample struct {
	At    int64
	Value float64
}

// Window is one period the lot spent inside the CCP's scope. End 0 = still inside.
type Window struct {
	Start int64
	End   int64
}

// LimitInput is everything needed to judge one limit for one lot.
type LimitInput struct {
	Limit          Limit
	Windows        []Window
	Samples        []Sample // 이 한계기준의 값(센서 측정값·사람 입력·계산값). 시각순
	Now            int64
	OutageAfterSec int // 0 = 불능 판정 안 함
	// CarryMs: 범위에 들어오기 직전(이 시간 안)의 센서 값을 진입 시점의 값으로 쓴다.
	// 단계를 측정 주기보다 빨리 지나가도 그때의 상태가 기록되게 한다. 0 = 쓰지 않음.
	CarryMs int64
}

// LimitResult is the judgement of one limit.
type LimitResult struct {
	Status      string   `json:"status"`
	Active      bool     `json:"active"` // lot 이 지금 범위 안에 있다
	Detail      string   `json:"detail"`
	Count       int      `json:"count"`
	Last        *float64 `json:"last,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	ExposureMin float64  `json:"exposure_min"` // 누적 노출(분) — cumulative_exposure 만
	// Deviation / Outage 는 Status 와 별개로 둘 다 참일 수 있다(이탈도 있고 끊김도 있었다).
	Deviation   bool  `json:"deviation"`
	DeviationAt int64 `json:"deviation_at,omitempty"`
	Outage      bool  `json:"outage"`
	OutageAt    int64 `json:"outage_at,omitempty"`
}

func inRange(l Limit, v float64) bool {
	return (l.Min == nil || v >= *l.Min) && (l.Max == nil || v <= *l.Max)
}

// EvaluateLimit judges one limit.
func EvaluateLimit(in LimitInput) LimitResult {
	res := LimitResult{Status: StatusPending}
	if len(in.Windows) == 0 {
		return res
	}
	end := func(w Window) int64 {
		if w.End == 0 {
			return in.Now
		}
		return w.End
	}
	res.Active = in.Windows[len(in.Windows)-1].End == 0

	// 범위 구간 안의 값만 본다.
	samples := append([]Sample(nil), in.Samples...)
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].At < samples[j].At })
	var inside [][]Sample
	for _, w := range in.Windows {
		var ws []Sample
		if in.CarryMs > 0 {
			for i := len(samples) - 1; i >= 0; i-- {
				if samples[i].At < w.Start {
					if w.Start-samples[i].At <= in.CarryMs {
						ws = append(ws, Sample{At: w.Start, Value: samples[i].Value})
					}
					break
				}
			}
		}
		for _, s := range samples {
			if s.At >= w.Start && s.At <= end(w) {
				ws = append(ws, s)
			}
		}
		inside = append(inside, ws)
		for _, s := range ws {
			v := s.Value
			res.Count++
			res.Last = &v
			if res.Min == nil || v < *res.Min {
				res.Min = &v
			}
			if res.Max == nil || v > *res.Max {
				res.Max = &v
			}
		}
	}
	deviate := func(at int64, format string, a ...any) {
		if !res.Deviation {
			res.Deviation, res.DeviationAt, res.Detail = true, at, fmt.Sprintf(format, a...)
		}
	}

	l := in.Limit
	switch l.RuleKind {
	case RuleInstant:
		for _, ws := range inside {
			for _, s := range ws {
				if !inRange(l, s.Value) {
					deviate(s.At, "%s %.3g%s — 한계기준 %s", ItemLabel(l.Item), s.Value, l.Unit, limitRangeText(l))
				}
			}
		}
	case RuleDuration:
		// 범위 안의 값이 끊기지 않고 이어진 가장 긴 시간.
		need := int64(l.Params["min_duration_sec"]) * 1000
		var best int64
		for _, ws := range inside {
			var runStart int64 = -1
			for _, s := range ws {
				if inRange(l, s.Value) {
					if runStart < 0 {
						runStart = s.At
					}
					if d := s.At - runStart; d > best {
						best = d
					}
				} else {
					runStart = -1
				}
			}
		}
		if best < need {
			if res.Active {
				res.Status = StatusInProgress
				res.Detail = fmt.Sprintf("유지 %d초 / 필요 %d초", best/1000, need/1000)
			} else {
				deviate(end(in.Windows[len(in.Windows)-1]), "%s 유지 시간 %d초 — 필요 %d초", ItemLabel(l.Item), best/1000, need/1000)
			}
		}
	case RuleCumulativeExposure:
		// 기준 온도를 넘은 채로 지난 시간을 더한다. 값 사이 간격은 앞 값이 이어진 것으로 본다.
		threshold, high := l.Params["threshold"], l.Params["high_threshold"]
		var total, totalHigh float64 // 분
		var lastAt int64
		for i, ws := range inside {
			for j, s := range ws {
				next := end(in.Windows[i])
				if j+1 < len(ws) {
					next = ws[j+1].At
				}
				min := float64(next-s.At) / 60000
				if s.Value > threshold {
					total += min
					lastAt = next
				}
				if high > 0 && s.Value > high {
					totalHigh += min
				}
			}
		}
		res.ExposureMin = total
		limit := l.Params["max_minutes"]
		// 고온 기준을 넘은 적이 있으면 더 짧은 한도를 쓴다.
		if totalHigh > 0 && l.Params["max_minutes_high"] > 0 {
			limit = l.Params["max_minutes_high"]
		}
		if total > limit {
			deviate(lastAt, "누적 노출 %.0f분 — 한도 %.0f분", total, limit)
		}
	case RuleCount:
		var sum float64
		for _, ws := range inside {
			for _, s := range ws {
				sum += s.Value
				if l.Max != nil && sum > *l.Max {
					deviate(s.At, "%s %.4g%s — 허용 %.4g", ItemLabel(l.Item), sum, l.Unit, *l.Max)
				}
			}
		}
	case RulePeriodicCheck:
		// 범위에 들어온 뒤, 그리고 점검과 점검 사이가 주기를 넘으면 점검 누락이다.
		interval := int64(l.Params["interval_min"]) * 60000
		for i, w := range in.Windows {
			prev := w.Start
			for _, s := range inside[i] {
				if s.At-prev > interval {
					deviate(prev+interval, "%s 점검 누락 — 주기 %.0f분", ItemLabel(l.Item), l.Params["interval_min"])
				}
				prev = s.At
			}
			if end(w)-prev > interval {
				deviate(prev+interval, "%s 점검 누락 — 주기 %.0f분", ItemLabel(l.Item), l.Params["interval_min"])
			}
		}
	}

	// 모니터링 불능: 센서 값이 범위 구간 안에서 정해진 시간 넘게 들어오지 않았다.
	if in.OutageAfterSec > 0 && l.SourceKind != SourceOperator && l.RuleKind != RulePeriodicCheck &&
		l.Item != "wait_time" {
		gap := int64(in.OutageAfterSec) * 1000
		for i, w := range in.Windows {
			prev := w.Start
			for _, s := range inside[i] {
				if s.At-prev > gap && !res.Outage {
					res.Outage, res.OutageAt = true, prev+gap
				}
				prev = s.At
			}
			if end(w)-prev > gap && !res.Outage {
				res.Outage, res.OutageAt = true, prev+gap
			}
		}
	}

	// 센서 값이 하나도 없이 범위를 지나갔으면 감시한 기록이 없는 것이다 — 적합이라고 하지 않는다.
	sensorBased := l.SourceKind != SourceOperator && l.RuleKind != RulePeriodicCheck && l.RuleKind != RuleCount && l.Item != "wait_time"
	if sensorBased && res.Count == 0 && !res.Active && !res.Outage {
		res.Outage, res.OutageAt = true, in.Windows[0].Start
	}

	switch {
	case res.Deviation:
		res.Status = StatusDeviation
	case res.Outage:
		res.Status = StatusOutage
		res.Detail = ItemLabel(l.Item) + " 값이 들어오지 않았습니다"
	case res.Status == StatusInProgress:
	case res.Count == 0 && l.SourceKind == SourceOperator:
		res.Status = StatusNeedsInput
		res.Detail = ItemLabel(l.Item) + " 값을 입력해야 합니다"
	case res.Count == 0 && l.RuleKind != RulePeriodicCheck && l.RuleKind != RuleCount && res.Active:
		res.Status = StatusInProgress
		res.Detail = "첫 값을 기다리는 중"
	default:
		res.Status = StatusOK
	}
	return res
}

// itemLabels: 템플릿이 쓰는 한계기준 항목의 표시 이름. 목록에 없으면 항목 이름을 그대로 쓴다.
var itemLabels = map[string]string{
	"temperature": "온도", "core_temperature": "중심온도", "wait_time": "대기 시간", "exposure_time": "누적 노출 시간",
	"wash_time": "세척 시간", "wash_water_flow": "세척수 유량", "raw_feed_rate": "원료 투입 속도",
	"metal_detected_count": "금속 검출 수", "test_piece_check": "시편 통과 점검",
}

// ItemLabel is the display name of a limit item.
func ItemLabel(item string) string {
	if v, ok := itemLabels[item]; ok {
		return v
	}
	return item
}

func limitRangeText(l Limit) string {
	switch {
	case l.Min != nil && l.Max != nil:
		return fmt.Sprintf("%.4g~%.4g%s", *l.Min, *l.Max, l.Unit)
	case l.Max != nil:
		return fmt.Sprintf("%.4g%s 이하", *l.Max, l.Unit)
	case l.Min != nil:
		return fmt.Sprintf("%.4g%s 이상", *l.Min, l.Unit)
	}
	return ""
}

// LimitText describes a limit for people: range, or the rule's parameters.
func LimitText(l Limit) string {
	switch l.RuleKind {
	case RuleCumulativeExposure:
		s := fmt.Sprintf("%.4g℃ 초과 누적 %.0f분 이내", l.Params["threshold"], l.Params["max_minutes"])
		if l.Params["high_threshold"] > 0 {
			s += fmt.Sprintf(" (%.4g℃ 초과가 있으면 %.0f분 이내)", l.Params["high_threshold"], l.Params["max_minutes_high"])
		}
		return s
	case RulePeriodicCheck:
		return fmt.Sprintf("%.0f분마다 점검", l.Params["interval_min"])
	case RuleDuration:
		return fmt.Sprintf("%s 을(를) %.0f초 이상 유지", limitRangeText(l), l.Params["min_duration_sec"])
	case RuleCount:
		if l.Max != nil {
			return fmt.Sprintf("%.4g%s 이하", *l.Max, l.Unit)
		}
	}
	return limitRangeText(l)
}
