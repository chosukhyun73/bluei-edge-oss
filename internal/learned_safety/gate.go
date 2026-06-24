package learned_safety

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"bluei.kr/edge/internal/config"
	"bluei.kr/edge/internal/storage"
)

// stalenessDefault — staleness_max_sec 미설정 시 기본값(초).
const stalenessDefault = 300

// Gate implements feed_cycle.SafetyGate using C-3l learned rules.
//
// 규칙은 생성 시 캐시에 로드되고, RefreshRules() 로 갱신할 수 있다.
// 게이트가 비활성화(config.LearnedSafety.Enabled=false)되면 항상 허용.
type Gate struct {
	store storage.Store
	cfg   config.LearnedSafetyConfig
	log   *slog.Logger

	mu    sync.RWMutex
	rules []*storage.LearnedRule // 활성 규칙 캐시
}

// NewGate creates a C-3l Gate and loads rules from storage.
// 초기 로드 실패 시 경고만 출력 — fail-open.
func NewGate(store storage.Store, cfg config.LearnedSafetyConfig) *Gate {
	// staleness_max_sec 미설정 시 기본값 적용
	if cfg.StalenessMaxSec == 0 {
		cfg.StalenessMaxSec = stalenessDefault
	}
	g := &Gate{
		store: store,
		cfg:   cfg,
		log:   slog.With("component", "learned_safety_gate"),
	}
	if cfg.Enabled {
		if err := g.RefreshRules(context.Background()); err != nil {
			g.log.Warn("C-3l initial rule load failed; gate starts empty", "error", err)
		}
	}
	return g
}

// RefreshRules reloads enabled learned rules from storage.
func (g *Gate) RefreshRules(ctx context.Context) error {
	rules, err := g.store.ListLearnedRules(ctx, true)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.rules = rules
	g.mu.Unlock()
	g.log.Info("C-3l rules refreshed", "count", len(rules))
	return nil
}

// Check implements feed_cycle.SafetyGate.
// current_tank_environment 테이블의 최신 센서값을 조회해 학습 규칙을 평가한다.
// staleness_max_sec 이상 오래된 측정값은 skip — fail-open.
func (g *Gate) Check(tankID string) (bool, string) {
	if !g.cfg.Enabled {
		return false, ""
	}

	g.mu.RLock()
	rules := g.rules
	g.mu.RUnlock()

	if len(rules) == 0 {
		return false, ""
	}

	// 탱크의 모든 최신 환경 측정값을 한 번에 조회 (단일 SELECT)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	readings, err := g.store.ListTankEnvironment(ctx, tankID)
	if err != nil {
		// 조회 실패 시 fail-open
		g.log.Warn("C-3l ListTankEnvironment failed; gate passes", "tank_id", tankID, "error", err)
		return false, ""
	}

	// metric → 최신값 맵 구성
	liveValues := make(map[string]*storage.CurrentTankEnvironmentReading, len(readings))
	for _, r := range readings {
		liveValues[r.Metric] = r
	}

	staleness := time.Duration(g.cfg.StalenessMaxSec) * time.Second
	now := time.Now().UTC()

	for _, r := range rules {
		var cond Condition
		if err := json.Unmarshal([]byte(r.ConditionJSON), &cond); err != nil {
			continue
		}

		reading, ok := liveValues[cond.Metric]
		if !ok || reading.Value == nil {
			// 해당 메트릭 데이터 없음 — skip (fail-open)
			continue
		}

		// staleness 검사: observed_at 파싱
		observedAt, parseErr := time.Parse(time.RFC3339Nano, reading.ObservedAt)
		if parseErr != nil {
			// ISO8601 대소문자 변형 대응
			observedAt, parseErr = time.Parse("2006-01-02T15:04:05Z", reading.ObservedAt)
		}
		if parseErr != nil || now.Sub(observedAt) > staleness {
			// 오래된 데이터 — skip (fail-open)
			continue
		}

		liveVal := *reading.Value
		if evalCondition(cond.Operator, liveVal, cond.Threshold) {
			reason := fmt.Sprintf("learned_rule=%s metric=%s %s %.2f (val=%.2f)",
				r.RuleID, cond.Metric, cond.Operator, cond.Threshold, liveVal)
			// hit_count 비동기 업데이트 — gate 판단에 영향 없음
			go func(ruleID string) {
				hCtx, hCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer hCancel()
				if err := g.store.IncrementLearnedRuleHit(hCtx, ruleID, time.Now().UTC()); err != nil {
					g.log.Warn("C-3l hit_count update failed", "rule_id", ruleID, "error", err)
				}
			}(r.RuleID)
			return true, reason
		}
	}

	return false, ""
}

// CheckWithValue evaluates a single rule against a provided value.
// 테스트 + API 에서 직접 호출 가능. 실제 sensor 값을 외부에서 주입.
func (g *Gate) CheckWithValue(tankID string, metric string, value float64) (bool, string) {
	if !g.cfg.Enabled {
		return false, ""
	}

	g.mu.RLock()
	rules := g.rules
	g.mu.RUnlock()

	for _, r := range rules {
		var cond Condition
		if err := json.Unmarshal([]byte(r.ConditionJSON), &cond); err != nil {
			continue
		}
		if cond.Metric != metric {
			continue
		}
		if evalCondition(cond.Operator, value, cond.Threshold) {
			reason := fmt.Sprintf("learned_rule=%s metric=%s %s %.2f (val=%.2f)",
				r.RuleID, cond.Metric, cond.Operator, cond.Threshold, value)
			// hit_count 비동기 업데이트 — 실패해도 gate 판단에 영향 없음
			if g.store != nil {
				go func(ruleID string) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := g.store.IncrementLearnedRuleHit(ctx, ruleID, time.Now().UTC()); err != nil {
						g.log.Warn("C-3l hit_count update failed", "rule_id", ruleID, "error", err)
					}
				}(r.RuleID)
			}
			return true, reason
		}
	}
	return false, ""
}

// evalCondition evaluates operator against value and threshold.
func evalCondition(op string, value, threshold float64) bool {
	switch op {
	case "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	default:
		return false
	}
}
