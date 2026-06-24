package learned_safety

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"bluei.kr/edge/internal/common"
	"bluei.kr/edge/internal/storage"
)

const (
	mineWindowDays    = 7
	mineMinHits       = 3
	minedRuleSeverity = "high"
)

// metricThresholdKey extracts a normalised key from a comment for aggregation.
// Simple heuristic: look for patterns like "temp>28" or "dissolved_oxygen<6.5".
var metricPattern = regexp.MustCompile(`(?i)([a-z_]+)\s*([><]=?)\s*(\d+(?:\.\d+)?)`)

// MineFromDisputes aggregates disputes from the last mineWindowDays days.
// If ≥3 disputes share the same metric+operator+threshold pattern within 7 days,
// a learned rule with severity="high" is emitted.
//
// Phase 4 MVP: 단순 패턴 집계. 실제 자연어 파싱은 Phase 5에서 개선 예정.
func MineFromDisputes(disputes []*storage.OperatorDispute) []*storage.LearnedRule {
	cutoff := common.NowUTC().Add(-mineWindowDays * 24 * time.Hour)

	// count occurrences per normalised pattern key
	type entry struct {
		count  int
		metric string
		op     string
		thresh string
	}
	counts := map[string]*entry{}

	for _, d := range disputes {
		if d.DisputedAt.Before(cutoff) {
			continue
		}
		text := d.Comment + " " + d.DisputeType
		for _, m := range metricPattern.FindAllStringSubmatch(text, -1) {
			if len(m) < 4 {
				continue
			}
			key := strings.ToLower(fmt.Sprintf("%s_%s_%s", m[1], m[2], m[3]))
			if e, ok := counts[key]; ok {
				e.count++
			} else {
				counts[key] = &entry{count: 1, metric: m[1], op: m[2], thresh: m[3]}
			}
		}
	}

	// sort keys for deterministic output
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var rules []*storage.LearnedRule
	for _, k := range keys {
		e := counts[k]
		if e.count < mineMinHits {
			continue
		}
		// parse threshold
		var thresh float64
		fmt.Sscanf(e.thresh, "%f", &thresh)
		goOp := sqlToGoOp(e.op)
		cond := Condition{
			Metric:    e.metric,
			Operator:  goOp,
			Threshold: thresh,
			WindowH:   mineWindowDays * 24,
		}
		condJSON, _ := json.Marshal(cond)

		rules = append(rules, &storage.LearnedRule{
			RuleID:        common.NewID("lrule"),
			ConditionJSON: string(condJSON),
			Severity:      minedRuleSeverity,
			Source:        "operator_dispute",
			Confidence:    float64(e.count) / float64(mineMinHits+3), // 단순 신뢰도 추정
			HitCount:      0,
			CreatedAt:     common.NowUTC(),
			Enabled:       true,
		})
	}
	return rules
}

func sqlToGoOp(op string) string {
	switch op {
	case ">":
		return "gt"
	case ">=":
		return "gte"
	case "<":
		return "lt"
	case "<=":
		return "lte"
	default:
		return "gt"
	}
}
