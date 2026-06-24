-- Phase 5 (load cell) — 실측 중량 기반 사료 공급량 트래킹.
-- ESP32 HX711 weight event 로 펄스당 1g/sec stub (estimatePerPulse) 를 대체.
-- pulse 별 actual_amount_g 는 events.payload (feed.pulse.weight) 에 저장.
-- 사이클 누적 실측은 본 테이블에 적재 (adaptive 종료 조건이 우선 참조).
-- See docs/45-hx711-load-cell-integration.md.

ALTER TABLE feed_cycles ADD COLUMN actual_total_amount_g REAL;
ALTER TABLE feed_cycles ADD COLUMN silo_depletion_warned INTEGER NOT NULL DEFAULT 0;
