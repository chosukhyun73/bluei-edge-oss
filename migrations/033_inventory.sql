-- 재고관리: 품목 마스터 + 현재고. 구매(입고)로 +, 사용(급이/투약/자재사용)으로 − 한다.
-- on_hand_qty 는 inventory.purchase.recorded / inventory.consumption.recorded 이벤트로 증감되는 projection.
-- 권위 기록은 events (append-only). References: docs/49-gdst-traceability-contract.md.

CREATE TABLE IF NOT EXISTS inventory_items (
  item_id        TEXT PRIMARY KEY,
  category       TEXT NOT NULL,            -- feed | drug | material
  name           TEXT NOT NULL,
  unit           TEXT NOT NULL,            -- kg | g | mL | 병 | ea | 포대 ...
  on_hand_qty    REAL NOT NULL DEFAULT 0,
  spec           TEXT,
  supplier       TEXT,
  reorder_level  REAL,                     -- 재발주점 (미만 시 경고)
  notes          TEXT,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);

-- 같은 카테고리 내 품목명 유일.
CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_cat_name ON inventory_items(category, name);
CREATE INDEX IF NOT EXISTS idx_inventory_category ON inventory_items(category);
