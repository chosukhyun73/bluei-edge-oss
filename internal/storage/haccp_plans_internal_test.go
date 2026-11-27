package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 게시된 계획 버전은 DB 트리거가 지킨다 — 저장 계층을 거치지 않은 직접 수정·삭제도 막힌다.
func TestHACCPPublishedVersionTriggers(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := Migrate(st, filepath.Join("..", "..", "migrations", "043_processing_haccp_plans.sql")); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	plan := &HACCPPlan{PlanID: "p1", SiteID: "plant", Name: "계획", CreatedAt: now, UpdatedAt: now}
	v := &HACCPPlanVersion{VersionID: "v1", PlanID: "p1", VersionNo: 1, Status: "draft",
		ContentJSON: `{"a":1}`, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateHACCPPlan(ctx, plan, v); err != nil {
		t.Fatalf("create: %v", err)
	}
	db := st.(*sqliteStore).db

	// 초안은 고칠 수 있다
	if _, err := db.Exec(`UPDATE haccp_plan_versions SET content_json='{"a":2}' WHERE version_id='v1'`); err != nil {
		t.Fatalf("초안 수정은 되어야 함: %v", err)
	}

	v.ContentHash = "h1"
	v.PublishedBy, v.OnsiteConfirmedBy = "u", "u"
	v.PublishedAt, v.OnsiteConfirmedAt = &now, &now
	if ok, err := st.PublishHACCPPlanVersion(ctx, v); err != nil || !ok {
		t.Fatalf("publish: ok=%v err=%v", ok, err)
	}

	for name, q := range map[string]string{
		"내용 수정":    `UPDATE haccp_plan_versions SET content_json='{"a":3}' WHERE version_id='v1'`,
		"해시 수정":    `UPDATE haccp_plan_versions SET content_hash='x' WHERE version_id='v1'`,
		"초안으로 되돌림": `UPDATE haccp_plan_versions SET status='draft' WHERE version_id='v1'`,
		"삭제":       `DELETE FROM haccp_plan_versions WHERE version_id='v1'`,
	} {
		if _, err := db.Exec(q); err == nil || !strings.Contains(err.Error(), "haccp plan version") {
			t.Errorf("%s: 트리거가 막아야 함, err=%v", name, err)
		}
	}
	// 폐기 처리(상태만 바꿈)는 허용된다
	if _, err := db.Exec(`UPDATE haccp_plan_versions SET status='retired' WHERE version_id='v1'`); err != nil {
		t.Fatalf("폐기 처리는 되어야 함: %v", err)
	}
	got, _ := st.GetHACCPPlanVersion(ctx, "v1")
	if got.ContentJSON != `{"a":2}` || got.ContentHash != "h1" {
		t.Fatalf("내용·해시가 그대로여야 함: %+v", got)
	}
}
