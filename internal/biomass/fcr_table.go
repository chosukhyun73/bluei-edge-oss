// Package biomass — D-3 FCR-based weight projector + 향후 D-4 calibrator.
// docs/29 의 평균 체중 추정 도메인 모델.
package biomass

// FCRKey — (어종 × 성장단계) 룩업 키.
type FCRKey struct {
	Species     string
	GrowthStage string
}

// expectedFCRTable — 어종/단계별 표준 FCR (한국 양식 + FishBase 기반 baseline).
// D-4 의 calibrator 가 이 값을 Cage/Tank별로 보정한다.
var expectedFCRTable = map[FCRKey]float64{
	{Species: "참돔", GrowthStage: "juvenile"}:   1.4,
	{Species: "참돔", GrowthStage: "growout"}:    1.6,
	{Species: "연어", GrowthStage: "juvenile"}:   1.0,
	{Species: "연어", GrowthStage: "growout"}:    1.2,
	{Species: "광어", GrowthStage: "juvenile"}:   1.6,
	{Species: "광어", GrowthStage: "growout"}:    1.8,
	{Species: "넙치", GrowthStage: "juvenile"}:   1.6, // 광어 별칭
	{Species: "넙치", GrowthStage: "growout"}:    1.8,
	{Species: "우럭", GrowthStage: "juvenile"}:   1.5,
	{Species: "우럭", GrowthStage: "growout"}:    1.7,
	{Species: "조피볼락", GrowthStage: "juvenile"}: 1.5,
	{Species: "조피볼락", GrowthStage: "growout"}:  1.7,
	{Species: "농어", GrowthStage: "juvenile"}:   1.5,
	{Species: "농어", GrowthStage: "growout"}:    1.7,
	{Species: "방어", GrowthStage: "juvenile"}:   1.5,
	{Species: "방어", GrowthStage: "growout"}:    1.7,
	{Species: "감성돔", GrowthStage: "juvenile"}:  1.4,
	{Species: "감성돔", GrowthStage: "growout"}:   1.6,
}

// DefaultFCR — 룩업 미스 시 폴백.
const DefaultFCR = 1.5

// LookupFCR — Species + GrowthStage → expected FCR.
// 미등록 어종/단계 → DefaultFCR + ok=false.
func LookupFCR(species, growthStage string) (float64, bool) {
	if v, ok := expectedFCRTable[FCRKey{species, growthStage}]; ok {
		return v, true
	}
	return DefaultFCR, false
}
