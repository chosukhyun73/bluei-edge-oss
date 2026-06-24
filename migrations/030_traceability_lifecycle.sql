-- GDST/ASC first-mile traceability: lot anchoring + transfer lineage.
-- lot_no 는 입식(tank.stocking.recorded) 시점에 immutable 하게 고정되어 lineage 를 추적한다.
-- parent_lot_no 는 transfer/grading(tank.transfer.recorded) 으로 파생된 lot 의 상위 lot (split/merge lineage).
-- 영구 history 는 events 테이블 (append-only CTE). 이 컬럼들은 현재상태 projection 의 편의 캐시다.
-- References: docs/49-gdst-traceability-contract.md.

ALTER TABLE current_tank_lifecycle ADD COLUMN lot_no TEXT;
ALTER TABLE current_tank_lifecycle ADD COLUMN parent_lot_no TEXT;
