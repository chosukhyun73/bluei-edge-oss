package predictive

import (
	"errors"

	"bluei.kr/edge/internal/species"
)

// ErrMissingWasteModel is returned when a species profile has no NH3 excretion data.
var ErrMissingWasteModel = errors.New("MISSING_WASTE_MODEL")

const (
	defaultIntakeEfficiency        = 0.85 // fraction of feed actually consumed by fish
	defaultMetabolismNH3GPerKgPerH = 0.01 // background NH3 from fish metabolism g/kg/h
)

// WasteInput holds all inputs for the D-6 estimator.
type WasteInput struct {
	FeedAmountG      float64 // grams of feed for this cycle
	BiomasKg         float64 // current biomass in kg
	CycleDurationH   float64 // expected cycle duration in hours
	IntakeEfficiency float64 // override; if 0 defaults to 0.85
	WasteModel       species.WasteModel
}

// WasteEstimate is the D-6 output.
type WasteEstimate struct {
	PredictedNH3G      float64 // predicted NH3 excretion this cycle (grams)
	PredictedNH3KgPerH float64 // rate (kg/h) over the cycle duration
}

// EstimateWaste computes predicted NH3 excretion for a single feed cycle (D-6).
//
// Formula:
//
//	predicted_nh3_g = feed_g × nh3_excretion_per_kg_feed/1000 × (1 - intake_efficiency)
//	               + biomass_kg × metabolism_nh3_g_per_kg_per_h × cycle_duration_h
func EstimateWaste(in WasteInput) (WasteEstimate, error) {
	if in.WasteModel.NH3ExcretionPerKgFeed == 0 {
		return WasteEstimate{}, ErrMissingWasteModel
	}

	ie := in.IntakeEfficiency
	if ie == 0 {
		ie = defaultIntakeEfficiency
	}

	feedNH3G := in.FeedAmountG * (in.WasteModel.NH3ExcretionPerKgFeed / 1000.0) * (1.0 - ie)
	metabolismNH3G := in.BiomasKg * defaultMetabolismNH3GPerKgPerH * in.CycleDurationH

	totalNH3G := feedNH3G + metabolismNH3G

	var rateKgPerH float64
	if in.CycleDurationH > 0 {
		rateKgPerH = (totalNH3G / 1000.0) / in.CycleDurationH
	}

	return WasteEstimate{
		PredictedNH3G:      totalNH3G,
		PredictedNH3KgPerH: rateKgPerH,
	}, nil
}
