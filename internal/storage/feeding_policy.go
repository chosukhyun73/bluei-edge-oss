package storage

// Phase 1b — 운영 정책 (BSF mode / operating mode / max daily cycles) 의 effective resolution.
//
// 정책 위치:
//   - tank_profiles.metadata_json.feeding_policy_override  : 수조 override (선택 필드만)
//   - group_profiles.metadata_json.feeding_policy          : 그룹 default (전체)
//   - 빠진 필드는 system default.
//
// AI worker (Phase 1c) 가 이 함수를 통해 단일 진실원으로 정책을 조회.
// 호출 비용: SQLite 2 회 read (tank_profiles + group_profiles). 캐시 X — 정책 변경 즉시 반영.

import (
	"context"
	"encoding/json"
)

// FeedingPolicy is the resolved effective policy used by AI worker / Mode Arbiter.
type FeedingPolicy struct {
	BsfMode        string // "aggressive" | "standard" | "conservative"
	OperatingMode  string // "auto" | "manual"
	MaxDailyCycles int
	Source         string // "tank_override" | "group_default" | "system_default"
}

// System default — group/tank 모두 정책 미지정 시 fallback.
var systemDefaultFeedingPolicy = FeedingPolicy{
	BsfMode:        "standard",
	OperatingMode:  "auto",
	MaxDailyCycles: 4,
	Source:         "system_default",
}

// GetEffectiveFeedingPolicy resolves the effective policy for a tank.
// Resolution order (field-by-field):
//  1. tank_profiles.metadata.feeding_policy_override.<field>
//  2. group_profiles.metadata.feeding_policy.<field>   (via tank.group_id)
//  3. system default.
//
// Source 는 가장 우선한 소스를 표시:
//   - tank override 가 한 필드라도 기여하면 "tank_override".
//   - 아니면 group default 가 기여하면 "group_default".
//   - 아니면 "system_default".
//
// tank 가 없으면 system default 반환 (에러 X — AI worker 가 견고하게 fallback).
func (s *sqliteStore) GetEffectiveFeedingPolicy(ctx context.Context, tankID string) (*FeedingPolicy, error) {
	policy := systemDefaultFeedingPolicy

	tank, err := s.GetTankProfile(ctx, tankID)
	if err != nil {
		return &policy, err
	}
	if tank == nil {
		return &policy, nil
	}

	// Group default 먼저 적용 (있으면).
	var groupContributed bool
	if tank.GroupID != "" {
		g, err := s.GetGroupProfile(ctx, tank.GroupID)
		if err == nil && g != nil {
			if fp, ok := extractFeedingPolicy(g.Metadata, "feeding_policy"); ok {
				if v, ok := fp["bsf_mode"].(string); ok && v != "" {
					policy.BsfMode = v
					groupContributed = true
				}
				if v, ok := fp["operating_mode"].(string); ok && v != "" {
					policy.OperatingMode = v
					groupContributed = true
				}
				if n, ok := numericInt(fp["max_daily_cycles"]); ok && n > 0 {
					policy.MaxDailyCycles = n
					groupContributed = true
				}
			}
		}
	}
	if groupContributed {
		policy.Source = "group_default"
	}

	// Tank override 덮어쓰기 (있으면).
	var tankContributed bool
	if fp, ok := extractFeedingPolicy(tank.Metadata, "feeding_policy_override"); ok {
		if v, ok := fp["bsf_mode"].(string); ok && v != "" {
			policy.BsfMode = v
			tankContributed = true
		}
		if v, ok := fp["operating_mode"].(string); ok && v != "" {
			policy.OperatingMode = v
			tankContributed = true
		}
		if n, ok := numericInt(fp["max_daily_cycles"]); ok && n > 0 {
			policy.MaxDailyCycles = n
			tankContributed = true
		}
	}
	if tankContributed {
		policy.Source = "tank_override"
	}

	return &policy, nil
}

// extractFeedingPolicy reads metadata[key] as a JSON object (map[string]any).
// metadata 가 nil 이거나 key 가 없거나 object 가 아니면 (nil, false) 반환.
// metadata[key] 가 string (JSON-encoded) 인 경우도 지원 — yaml flat 값이 들어온 경우 호환.
func extractFeedingPolicy(metadata map[string]any, key string) (map[string]any, bool) {
	if metadata == nil {
		return nil, false
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil, false
	}
	if obj, ok := raw.(map[string]any); ok {
		return obj, true
	}
	if str, ok := raw.(string); ok && str != "" {
		var obj map[string]any
		if json.Unmarshal([]byte(str), &obj) == nil && obj != nil {
			return obj, true
		}
	}
	return nil, false
}

// numericInt extracts an int from any (float64 from JSON, or int).
func numericInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	}
	return 0, false
}
