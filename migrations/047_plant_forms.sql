-- 공장이 쓰는 기록 양식(점검표·일지·서식)의 판.
-- 양식은 고칠 때마다 개정 번호가 올라가는 새 줄로 쌓는다 — 지난 판을 덮어쓰지 않는다.
-- 가장 큰 revision 이 지금 쓰는 판이다. 양식 내용은 internal/plantform.Form 의 JSON.
-- References: docs/56-haccp-team-leader-work-system-design.md §17, §18.

CREATE TABLE IF NOT EXISTS plant_forms (
  site_id    TEXT NOT NULL,
  form_id    TEXT NOT NULL,
  revision   INTEGER NOT NULL,
  source     TEXT NOT NULL,             -- default(기본 양식에서 받음) | import(쓰던 양식을 가져옴) | edit(고침)
  base_pack  TEXT NOT NULL DEFAULT '',  -- 기본 양식 묶음 id ('' = 없음)
  base_id    TEXT NOT NULL DEFAULT '',  -- 연결된 기본 양식 id ('' = 없음)
  content    TEXT NOT NULL,
  note       TEXT NOT NULL DEFAULT '',  -- 이 개정에서 바뀐 점
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,          -- UTC unix 밀리초
  PRIMARY KEY (site_id, form_id, revision)
);
