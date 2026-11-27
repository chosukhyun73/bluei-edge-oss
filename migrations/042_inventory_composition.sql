-- 사료(및 기타 품목) 영양성분표(보증성분) 저장. 라벨의 조단백/조지방/라이신/총인/
-- 수분/조섬유/조회분/칼슘(%) + 제품모델/펠릿크기/대상어종을 JSON 1벌로 품목 마스터에 부착.
-- 품목당 1벌(로트 무관). 급이 AI(단백/지방 참조)·GDST 이력에 사용.

ALTER TABLE inventory_items ADD COLUMN composition_json TEXT;
