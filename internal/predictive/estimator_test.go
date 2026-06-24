package predictive

import (
	"math"
	"testing"

	"bluei.kr/edge/internal/species"
)

// atlanticSalmonWaste is a realistic waste model for atlantic salmon.
// NH3ExcretionPerKgFeed = 30 g/kg is a commonly cited value.
var atlanticSalmonWaste = species.WasteModel{
	NH3ExcretionPerKgFeed: 30.0, // g NH3 per kg feed
}

func TestEstimateWaste_BasicNumerical(t *testing.T) {
	// Worked example:
	//   feed = 100 g, biomass = 1200 kg, cycle = 0.5 h, intake_efficiency = 0.85
	//
	//   feedNH3 = 100 × (30/1000) × (1-0.85) = 100 × 0.03 × 0.15 = 0.45 g
	//   metabolismNH3 = 1200 × 0.01 × 0.5 = 6.0 g
	//   total = 0.45 + 6.0 = 6.45 g
	//   rate = (6.45/1000) / 0.5 = 0.0129 kg/h

	in := WasteInput{
		FeedAmountG:    100,
		BiomasKg:       1200,
		CycleDurationH: 0.5,
		WasteModel:     atlanticSalmonWaste,
		// IntakeEfficiency = 0 → defaults to 0.85
	}
	est, err := EstimateWaste(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantNH3G := 6.45
	if math.Abs(est.PredictedNH3G-wantNH3G) > 0.001 {
		t.Errorf("PredictedNH3G = %.4f; want %.4f", est.PredictedNH3G, wantNH3G)
	}

	wantRateKgPerH := 0.0129
	if math.Abs(est.PredictedNH3KgPerH-wantRateKgPerH) > 0.0001 {
		t.Errorf("PredictedNH3KgPerH = %.6f; want %.6f", est.PredictedNH3KgPerH, wantRateKgPerH)
	}
}

func TestEstimateWaste_ZeroBiomass(t *testing.T) {
	// Only feed-driven NH3, no metabolism term.
	// feed = 200g, intake_eff = 0.85, nh3=30g/kg
	// feedNH3 = 200 × 0.03 × 0.15 = 0.9 g
	in := WasteInput{
		FeedAmountG:    200,
		BiomasKg:       0,
		CycleDurationH: 1.0,
		WasteModel:     atlanticSalmonWaste,
	}
	est, err := EstimateWaste(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 0.9
	if math.Abs(est.PredictedNH3G-want) > 0.001 {
		t.Errorf("PredictedNH3G = %.4f; want %.4f", est.PredictedNH3G, want)
	}
}

func TestEstimateWaste_MissingWasteModel(t *testing.T) {
	in := WasteInput{
		FeedAmountG: 100,
		WasteModel:  species.WasteModel{NH3ExcretionPerKgFeed: 0},
	}
	_, err := EstimateWaste(in)
	if err != ErrMissingWasteModel {
		t.Errorf("expected ErrMissingWasteModel, got %v", err)
	}
}

func TestEstimateWaste_CustomIntakeEfficiency(t *testing.T) {
	// intake_efficiency = 0.90
	// feed=100g, nh3=30g/kg → feedNH3 = 100 × 0.03 × 0.10 = 0.3 g
	in := WasteInput{
		FeedAmountG:      100,
		BiomasKg:         0,
		CycleDurationH:   0,
		IntakeEfficiency: 0.90,
		WasteModel:       atlanticSalmonWaste,
	}
	est, err := EstimateWaste(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 0.3
	if math.Abs(est.PredictedNH3G-want) > 0.001 {
		t.Errorf("PredictedNH3G = %.4f; want %.4f", est.PredictedNH3G, want)
	}
}

func TestEstimateWaste_ZeroCycleDuration(t *testing.T) {
	// CycleDurationH=0 → rate should be 0 (no division by zero)
	in := WasteInput{
		FeedAmountG:    100,
		BiomasKg:       100,
		CycleDurationH: 0,
		WasteModel:     atlanticSalmonWaste,
	}
	est, err := EstimateWaste(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est.PredictedNH3KgPerH != 0 {
		t.Errorf("expected 0 rate for zero duration, got %.6f", est.PredictedNH3KgPerH)
	}
}
