-- Phase A.1 — feed_cycles 테이블에 살포모터/공급모터 출력 컬럼 추가.
-- ESP32 펌웨어 (firmware/esp32-feeder-controller.ino) 가 이미 feed.dispense 명령에서
-- speed_rpm (살포 모터, GPIO 25 DAC1) 와 amount (공급 모터, GPIO 26 DAC2) 를 받지만
-- backend dispatchPulse 가 이를 전달하지 않아 default 가 적용되고 있었음.
-- 본 마이그는 cycle 별 모터 출력 설정을 영구 저장.
--
-- 범위:
--   speed_rpm  14~42 rpm (펌웨어 DAC 매핑: 14rpm → 0.8V (64), 42rpm → 3.3V (255))
--   amount     0~255    (펌웨어 DAC 0~3.3V 매핑)
-- NULL = controller default 사용 (back-compat).

ALTER TABLE feed_cycles ADD COLUMN speed_rpm INTEGER;
ALTER TABLE feed_cycles ADD COLUMN amount INTEGER;
