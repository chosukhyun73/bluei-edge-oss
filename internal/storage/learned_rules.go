package storage

import (
	"context"
	"time"
)

// LearnedRule is a row in learned_rules.
type LearnedRule struct {
	RuleID        string     `json:"rule_id"`
	ConditionJSON string     `json:"condition_json"`
	Severity      string     `json:"severity"`
	Source        string     `json:"source"`
	Confidence    float64    `json:"confidence"`
	HitCount      int        `json:"hit_count"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMatchedAt *time.Time `json:"last_matched_at,omitempty"`
	Enabled       bool       `json:"enabled"`
}

func (s *sqliteStore) InsertLearnedRule(ctx context.Context, r *LearnedRule) error {
	enabled := 1
	if !r.Enabled {
		enabled = 0
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO learned_rules(rule_id,condition_json,severity,source,confidence,hit_count,created_at,last_matched_at,enabled)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.RuleID, r.ConditionJSON, r.Severity, r.Source,
		r.Confidence, r.HitCount, fmtTime(r.CreatedAt),
		fmtTimePtr(r.LastMatchedAt), enabled,
	)
	return err
}

func (s *sqliteStore) ListLearnedRules(ctx context.Context, onlyEnabled bool) ([]*LearnedRule, error) {
	q := `SELECT rule_id,condition_json,severity,source,confidence,hit_count,created_at,last_matched_at,enabled
	      FROM learned_rules`
	if onlyEnabled {
		q += ` WHERE enabled=1`
	}
	q += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*LearnedRule
	for rows.Next() {
		r := &LearnedRule{}
		var createdAt string
		var lastMatchedAt *string
		var enabledInt int
		if err := rows.Scan(&r.RuleID, &r.ConditionJSON, &r.Severity, &r.Source,
			&r.Confidence, &r.HitCount, &createdAt, &lastMatchedAt, &enabledInt); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if lastMatchedAt != nil {
			t, _ := time.Parse(time.RFC3339Nano, *lastMatchedAt)
			r.LastMatchedAt = &t
		}
		r.Enabled = enabledInt == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) SetLearnedRuleEnabled(ctx context.Context, ruleID string, enabled bool) error {
	v := 1
	if !enabled {
		v = 0
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE learned_rules SET enabled=? WHERE rule_id=?`, v, ruleID)
	return err
}

func (s *sqliteStore) IncrementLearnedRuleHit(ctx context.Context, ruleID string, matchedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE learned_rules SET hit_count=hit_count+1, last_matched_at=? WHERE rule_id=?`,
		fmtTime(matchedAt), ruleID)
	return err
}
