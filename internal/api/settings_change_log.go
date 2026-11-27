package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"bluei.kr/edge/internal/events"
	"bluei.kr/edge/internal/storage"
)

// 그룹 설정·수조 급이정책 덮어쓰기의 변경 이력 (2026-10-19 운영자 요청).
//
// 급이 모드(안정적/일반/공격적 = bsf_mode)·하루 횟수·점등창 등은 계획 급이량을 바로
// 바꾸는데, 종전에는 저장만 하고 이벤트를 남기지 않아 "그날 어떤 모드였나"를 사후에
// 확인할 수 없었다. 저장 전후 값을 비교해 바뀐 항목만 {from, to} 로 남긴다.
// 실제 적용 정책 = 그룹 feeding_policy 위에 수조 feeding_policy_override 를 덮은 값
// (storage.GetEffectiveFeedingPolicy) 이라 두 경로를 모두 기록한다.

type settingChange struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// flattenSettings — 중첩 map 을 "a.b.c" 키로 펼친다. nil map 은 빈 결과.
func flattenSettings(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			flattenSettings(key, sub, out)
			continue
		}
		out[key] = v
	}
}

// diffSettings — 펼친 두 map 의 차이. 값 비교는 JSON 표현 기준(숫자 타입 차이 무시).
func diffSettings(before, after map[string]any) map[string]settingChange {
	changes := map[string]settingChange{}
	same := func(a, b any) bool {
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		return string(ja) == string(jb)
	}
	for k, b := range before {
		a, ok := after[k]
		if !ok {
			changes[k] = settingChange{From: b, To: nil}
		} else if !same(a, b) {
			changes[k] = settingChange{From: b, To: a}
		}
	}
	for k, a := range after {
		if _, ok := before[k]; !ok {
			changes[k] = settingChange{From: nil, To: a}
		}
	}
	return changes
}

func groupSettings(g *storage.GroupProfile) map[string]any {
	out := map[string]any{}
	if g == nil {
		return out
	}
	out["name"] = g.Name
	out["description"] = g.Description
	out["color"] = g.Color
	flattenSettings("metadata", g.Metadata, out)
	return out
}

// logGroupChange — 그룹 저장 전(before, 신규면 nil)·후(after) 비교, 바뀐 게 있으면 이벤트.
// 기록 실패는 저장을 되돌리지 않는다(경고 로그만).
func (s *Server) logGroupChange(ctx context.Context, before, after *storage.GroupProfile) {
	if s.app == nil || after == nil {
		return
	}
	changes := diffSettings(groupSettings(before), groupSettings(after))
	if len(changes) == 0 {
		return
	}
	action := "updated"
	if before == nil {
		action = "created"
	}
	payload := map[string]any{
		"group_id":   after.GroupID,
		"action":     action,
		"changes":    changes,
		"changed_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := s.app.AppendEvent(ctx, "api", "operator", after.GroupID,
		events.EventGroupProfileChanged, after.GroupID, payload); err != nil {
		slog.Warn("group change event append failed", "group_id", after.GroupID, "error", err)
	}
}

// logTankPolicyOverrideChange — 수조 metadata.feeding_policy_override 의 변경만 기록.
// (수조 프로필의 다른 항목은 대상 아님.)
func (s *Server) logTankPolicyOverrideChange(ctx context.Context, tankID string, beforeMeta, afterMeta map[string]any) {
	if s.app == nil {
		return
	}
	pick := func(meta map[string]any) map[string]any {
		out := map[string]any{}
		if ov, ok := meta["feeding_policy_override"].(map[string]any); ok {
			flattenSettings("feeding_policy_override", ov, out)
		}
		return out
	}
	changes := diffSettings(pick(beforeMeta), pick(afterMeta))
	if len(changes) == 0 {
		return
	}
	payload := map[string]any{
		"tank_id":    tankID,
		"changes":    changes,
		"changed_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := s.app.AppendEvent(ctx, "api", "operator", tankID,
		events.EventTankFeedingPolicyChanged, tankID, payload); err != nil {
		slog.Warn("tank policy override event append failed", "tank_id", tankID, "error", err)
	}
}
