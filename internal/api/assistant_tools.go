package api

// 어시스턴트 도구(tool use) — 로컬 LLM 이 운영 데이터를 직접 조회해 답하도록 한다.
//
// 배경 (2026-07-28): 어시스턴트가 "호퍼 잔량이 얼마냐"에 답하지 못하고 "저는 DB 에
// 접근할 권한이 없습니다"라고 응답했다. 실제로 주입 컨텍스트(수질·장비·오늘 급이량)에
// 없는 값은 모델이 알 방법이 없었다. 운영자는 Claude 를 쓰지만 현장 사용자는 이
// 로컬 어시스턴트만 쓰므로, 고정 요약이 아니라 필요한 데이터를 스스로 가져오게 한다.
//
// 안전 경계 (반드시 유지):
//   - 여기 등록되는 도구는 전부 **읽기 전용**이다. 급이 실행·장비 제어·설정 변경 도구는
//     추가하지 않는다. 실시간 안전 판단은 Arbiter, 급이 결정은 룰/LRCN 이 담당하며
//     LLM 은 관여하지 않는다(CLAUDE.md 원칙).
//   - 조회 결과는 행 수 상한을 둬 컨텍스트 폭주를 막는다.
//   - 모든 도구 호출은 로그로 남긴다(감사).

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/feed_cycle"
	"bluei.kr/edge/internal/storage"
)

const (
	// assistantMaxToolRounds — 도구 호출 왕복 상한. 26B 로컬 추론이라 왕복마다 수 초가
	// 붙는다. 3회면 "잔량 조회 → 사이클 조회 → 답변" 정도가 가능하다.
	assistantMaxToolRounds = 3
	// assistantToolRowLimit — 도구 1건이 돌려줄 최대 행 수.
	assistantToolRowLimit = 40
)

// assistantToolDefs — ollama /api/chat 의 tools 파라미터로 넘길 도구 스펙.
func assistantToolDefs() []map[string]any {
	tool := func(name, desc string, props map[string]any, required []string) map[string]any {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters": map[string]any{
					"type":       "object",
					"properties": props,
					"required":   required,
				},
			},
		}
	}
	tankProp := map[string]any{"type": "string", "description": "수조 ID (예: tank_01)"}
	dayProp := map[string]any{"type": "string", "description": "조회 날짜 YYYY-MM-DD. 생략하면 오늘"}

	return []map[string]any{
		tool("get_hopper", "수조의 사료 호퍼 잔량(g) 추정치와 마지막 보충 시각을 조회한다",
			map[string]any{"tank_id": tankProp}, []string{"tank_id"}),
		tool("get_tank_state", "수조의 현재 상태(수질·장비·오늘 급이량·자율모드)를 조회한다",
			map[string]any{"tank_id": tankProp}, []string{"tank_id"}),
		tool("get_feed_cycles", "수조의 급이 사이클 기록(발사량·펄스 수·종료 사유)을 조회한다",
			map[string]any{"tank_id": tankProp, "date": dayProp}, []string{"tank_id"}),
		tool("get_pulse_judgments", "펄스별 배출 판정(yes/no/unclear)과 보상 여부를 조회한다. 무배출이 몇 건인지 물을 때 쓴다",
			map[string]any{"tank_id": tankProp, "date": dayProp}, []string{"tank_id"}),
		tool("get_feeding_schedule", "수조의 급이 스케줄(시각 목록)과 자율모드 설정을 조회한다",
			map[string]any{"tank_id": tankProp}, []string{"tank_id"}),
		tool("get_open_alerts", "현재 열려 있는 알림(경보) 목록을 조회한다", map[string]any{}, nil),
		tool("get_sensor_series", "수조의 센서 시계열 요약(최소/평균/최대)을 조회한다",
			map[string]any{
				"tank_id": tankProp,
				"metric":  map[string]any{"type": "string", "description": "water_temperature | dissolved_oxygen | ph"},
				"hours":   map[string]any{"type": "integer", "description": "최근 몇 시간. 기본 24"},
			}, []string{"tank_id", "metric"}),
		tool("search_guide", "운영자 가이드북에서 관련 내용을 찾는다. 시스템 사용법·화면 위치·급이 동작 원리·용어·문제 해결을 물으면 반드시 먼저 이 도구를 쓴다",
			map[string]any{"query": map[string]any{"type": "string", "description": "찾을 내용 (예: 호퍼 보충, 급이 스케줄, 요동)"}},
			[]string{"query"}),
		tool("query_events", "이벤트 원장을 직접 조회한다. 위 도구로 안 되는 항목을 확인할 때만 쓴다",
			map[string]any{
				"event_type": map[string]any{"type": "string", "description": "예: feed.cycle.report, feeder.hopper.refilled"},
				"tank_id":    tankProp,
				"hours":      map[string]any{"type": "integer", "description": "최근 몇 시간. 기본 24"},
			}, []string{"event_type"}),
	}
}

// execAssistantTool — 도구 1건을 실행해 모델에 돌려줄 텍스트(JSON)를 만든다.
// 오류도 텍스트로 돌려준다 — 모델이 "조회 실패"를 스스로 설명할 수 있게.
func (s *Server) execAssistantTool(r *http.Request, name string, args map[string]any) string {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	tankID := argString(args, "tank_id")
	switch name {
	case "get_hopper":
		est, err := feed_cycle.EstimateHopper(ctx, s.store, tankID)
		if err != nil {
			return toolErr(err)
		}
		if est == nil || est.RemainingG == nil {
			return `{"note":"보충 기록이 없어 잔량을 추정할 수 없습니다"}`
		}
		return toolJSON(map[string]any{
			"tank_id": tankID, "remaining_g": *est.RemainingG,
			"last_refill_at": est.LastRefillAt, "last_refill_g": est.LastRefillG,
			"consumed_since_refill_g": est.ConsumedSinceRefillG,
			"last_recalibrated_at":    est.LastRecalibratedAt,
			"low":                     est.Low,
			"note":                    "무게 센서가 없는 개루프 추정치입니다. 실제 잔량과 다를 수 있습니다.",
		})

	case "get_tank_state":
		v, err := s.buildTankStateVector(r, tankID)
		if err != nil || v == nil {
			return toolErr(err)
		}
		water := map[string]any{}
		for k, m := range v.Water.Metrics {
			if m != nil && m.Value != nil {
				water[k] = *m.Value
			}
		}
		return toolJSON(map[string]any{
			"tank_id": tankID, "species": v.BiologicalContext.Species,
			"autonomous_mode": v.Autonomous.Mode, "water": water,
			"equipment": v.Equipment.HealthSummary, "today_feed_g": v.Feeding.TodayTotalG,
		})

	case "get_feed_cycles":
		rows := s.toolQueryEvents(ctx, events.EventFeedCycleReport, tankID, argDaySince(args))
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			var p map[string]any
			if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
				continue
			}
			out = append(out, map[string]any{
				"at": e.RecordedAt.Local().Format("15:04"), "pulses": p["pulse_count"],
				"amount_g": p["actual_total_amount_g"], "end_reason": p["end_reason"],
			})
		}
		return toolJSON(map[string]any{"tank_id": tankID, "cycles": out, "count": len(out)})

	case "get_pulse_judgments":
		rows := s.toolQueryEvents(ctx, "feed.pulse.discharge_judged", tankID, argDaySince(args))
		counts := map[string]int{}
		comp := 0
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			var p struct {
				PulseNo    int    `json:"pulse_no"`
				Verdict    string `json:"verdict"`
				Granted    bool   `json:"compensation_granted"`
				CycleID    string `json:"cycle_id"`
				AutonMode  string `json:"autonomous_mode"`
				DeductedG  float64
				RecordedAt string
			}
			if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
				continue
			}
			counts[p.Verdict]++
			if p.Granted {
				comp++
			}
			out = append(out, map[string]any{
				"at": e.RecordedAt.Local().Format("15:04"), "pulse_no": p.PulseNo,
				"verdict": p.Verdict, "compensated": p.Granted,
			})
		}
		return toolJSON(map[string]any{
			"tank_id": tankID, "summary": counts, "compensations": comp, "pulses": out,
		})

	case "get_feeding_schedule":
		scheds, err := s.store.ListAllSchedules(ctx)
		if err != nil {
			return toolErr(err)
		}
		var mine []map[string]any
		for _, sc := range scheds {
			for _, t := range sc.TankIDs {
				if t != tankID {
					continue
				}
				mine = append(mine, map[string]any{
					"schedule_id": sc.ScheduleID, "times": sc.Times,
					"priority": sc.Priority, "enabled": sc.Enabled,
					"pulses": sc.Pattern.TotalPulses, "target_amount_g": sc.Pattern.TargetAmountG,
				})
			}
		}
		res := map[string]any{"tank_id": tankID, "schedules": mine}
		if mode, err := s.store.GetTankAutonomousMode(ctx, tankID); err == nil && mode != nil {
			res["autonomous_mode"] = mode.Mode
		}
		if len(mine) == 0 {
			res["note"] = "등록된 급이 스케줄이 없습니다 (수동 모드이거나 AI 스케줄이 삭제된 상태)"
		}
		return toolJSON(res)

	case "get_open_alerts":
		alerts, err := s.store.ListOpenAlerts(ctx)
		if err != nil {
			return toolErr(err)
		}
		out := make([]map[string]any, 0, len(alerts))
		for i, a := range alerts {
			if i >= assistantToolRowLimit {
				break
			}
			out = append(out, map[string]any{
				"alert_type": a.AlertType, "severity": a.Severity,
				"subject":   a.SubjectKind + "/" + a.SubjectID,
				"raised_at": a.RaisedAt.Local().Format("01-02 15:04"),
			})
		}
		return toolJSON(map[string]any{"count": len(alerts), "alerts": out})

	case "get_sensor_series":
		metric := argString(args, "metric")
		rows := s.toolQueryEvents(ctx, events.EventSensorReadingRecorded, tankID, argHoursSince(args, 24))
		var vals []float64
		for _, e := range rows {
			var p struct {
				Metric string  `json:"metric"`
				Value  float64 `json:"value"`
			}
			if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil || p.Metric != metric {
				continue
			}
			vals = append(vals, p.Value)
		}
		if len(vals) == 0 {
			return toolJSON(map[string]any{"tank_id": tankID, "metric": metric, "note": "해당 구간 데이터 없음"})
		}
		min, max, sum := vals[0], vals[0], 0.0
		for _, v := range vals {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			sum += v
		}
		return toolJSON(map[string]any{
			"tank_id": tankID, "metric": metric, "n": len(vals),
			"min": min, "avg": sum / float64(len(vals)), "max": max, "latest": vals[0],
		})

	case "search_guide":
		return s.searchOperatorGuide(argString(args, "query"))

	case "query_events":
		et := argString(args, "event_type")
		if et == "" {
			return `{"error":"event_type 이 필요합니다"}`
		}
		rows := s.toolQueryEvents(ctx, et, tankID, argHoursSince(args, 24))
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			payload := e.PayloadJSON
			if len(payload) > 400 {
				payload = payload[:400] + "…"
			}
			out = append(out, map[string]any{
				"at": e.RecordedAt.Local().Format("01-02 15:04"), "payload": payload,
			})
		}
		return toolJSON(map[string]any{"event_type": et, "count": len(out), "events": out})
	}
	return fmt.Sprintf(`{"error":"알 수 없는 도구: %s"}`, name)
}

// toolQueryEvents — 도구 공통 이벤트 조회 (행 수 상한 적용, 최신순).
func (s *Server) toolQueryEvents(ctx context.Context, eventType, tankID string, since time.Time) []*storage.Event {
	rows, err := s.store.QueryEvents(ctx, storage.EventFilter{
		EventType: eventType, DeviceID: tankID, Since: &since, Limit: assistantToolRowLimit,
	})
	if err != nil {
		slog.Warn("assistant tool query failed", "event_type", eventType, "error", err)
		return nil
	}
	return rows
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// argDaySince — date 인자(YYYY-MM-DD)의 0시. 없거나 파싱 실패면 오늘 0시.
func argDaySince(args map[string]any) time.Time {
	if d := argString(args, "date"); d != "" {
		if t, err := time.ParseInLocation("2006-01-02", d, time.Local); err == nil {
			return t.UTC()
		}
	}
	now := time.Now().Local()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).UTC()
}

func argHoursSince(args map[string]any, def int) time.Time {
	h := def
	if v, ok := args["hours"].(float64); ok && v > 0 {
		h = int(v)
	}
	return time.Now().UTC().Add(-time.Duration(h) * time.Hour)
}

func toolJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return toolErr(err)
	}
	return string(b)
}

func toolErr(err error) string {
	if err == nil {
		return `{"error":"조회 결과 없음"}`
	}
	return toolJSON(map[string]any{"error": err.Error()})
}
