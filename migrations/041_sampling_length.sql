-- Cage/Tank sampling 에 평균 길이(cm) 추가.
-- 입식/샘플링 모두 길이 전용 필드가 없어 도입. optional (nullable).
-- 영구 history 는 events 테이블 (tank.sampling.recorded) payload 에 동반.

ALTER TABLE current_tank_sampling ADD COLUMN avg_length_cm REAL;
