-- GDST KDE: species 식별을 FAO ASFIS 3-alpha 코드 + 학명으로 보강.
-- 내부 species 키('red_seabream')는 운영 short ID 로 유지하고, FAO 코드/학명은 추적성 KDE 로 추가한다.
-- 예: red_seabream → fao_asfis_code='RSE', scientific_name='Pagrus major'.
-- References: docs/49-gdst-traceability-contract.md.

ALTER TABLE species_profiles ADD COLUMN fao_asfis_code TEXT;
ALTER TABLE species_profiles ADD COLUMN scientific_name TEXT;
