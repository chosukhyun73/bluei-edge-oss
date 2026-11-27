-- 가공공장 HACCP 계획(공정도·CCP)과 버전.
-- 계획 내용(제품설명서·팀·단계·연결·위해요소·CCP·한계기준)은 버전마다 content_json 한 덩어리로 둔다.
-- 게시(published) 뒤에는 내용이 바뀌지 않는다 — 아래 트리거가 DB 단에서 막는다.
-- References: docs/54-haccp-process-flow-ccp-design.md §5, §19.

CREATE TABLE IF NOT EXISTS haccp_plans (
  plan_id       TEXT PRIMARY KEY,
  site_id       TEXT NOT NULL,            -- sites.site_type = 'processing'
  product_group TEXT NOT NULL DEFAULT '',
  name          TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_haccp_plans_site ON haccp_plans(site_id);

CREATE TABLE IF NOT EXISTS haccp_plan_versions (
  version_id          TEXT PRIMARY KEY,
  plan_id             TEXT NOT NULL,
  version_no          INTEGER NOT NULL,
  status              TEXT NOT NULL,      -- draft | published | retired
  template_ref        TEXT NOT NULL DEFAULT '',
  content_json        TEXT NOT NULL,
  content_hash        TEXT NOT NULL DEFAULT '',
  change_reason       TEXT NOT NULL DEFAULT '',
  ccp_changed         INTEGER NOT NULL DEFAULT 0,  -- 직전 게시본 대비 CCP 변경(인증 변경 신청 대상)
  onsite_confirmed_by TEXT NOT NULL DEFAULT '',
  onsite_confirmed_at TEXT,
  published_by        TEXT NOT NULL DEFAULT '',
  published_at        TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (plan_id, version_no),
  FOREIGN KEY (plan_id) REFERENCES haccp_plans(plan_id)
);

CREATE INDEX IF NOT EXISTS idx_haccp_plan_versions_plan ON haccp_plan_versions(plan_id, status);

CREATE TRIGGER IF NOT EXISTS haccp_plan_versions_immutable
BEFORE UPDATE ON haccp_plan_versions
WHEN OLD.status <> 'draft' AND (
     NEW.content_json <> OLD.content_json
  OR NEW.content_hash <> OLD.content_hash
  OR NEW.version_no <> OLD.version_no
  OR NEW.plan_id <> OLD.plan_id
  OR NEW.status = 'draft')
BEGIN
  SELECT RAISE(ABORT, 'published haccp plan version is immutable');
END;

CREATE TRIGGER IF NOT EXISTS haccp_plan_versions_no_delete
BEFORE DELETE ON haccp_plan_versions
WHEN OLD.status <> 'draft'
BEGIN
  SELECT RAISE(ABORT, 'published haccp plan version cannot be deleted');
END;
