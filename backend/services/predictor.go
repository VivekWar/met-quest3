package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vivek/met-quest/models"
)

// ──────────────────────────────────────────────────────────────────────────
//  LLM-Enhanced Alloy Predictor
// ──────────────────────────────────────────────────────────────────────────

// PredictAlloyProperties takes a custom composition (symbol → weight%) and:
//  1. Looks up element properties from the DB (rule-of-mixtures baseline)
//  2. Sends the baseline + context to Gemini for thermodynamic refinement
//
// Returns an enriched PredictResponse with both phases of the prediction.
func PredictAlloyProperties(ctx context.Context, composition map[string]float64) (models.PredictResponse, error) {
	// ── Validate composition sums to ~100% ─────────────────────────────
	total := 0.0
	for _, pct := range composition {
		total += pct
	}
	if total < 95 || total > 105 {
		return models.PredictResponse{}, fmt.Errorf("composition must sum to ~100%% (got %.1f%%)", total)
	}

	// Normalise to exactly 100
	normalised := make(map[string]float64, len(composition))
	for sym, pct := range composition {
		normalised[sym] = pct / total * 100
	}

	// ── Build list of element symbols ──────────────────────────────────
	symbols := make([]string, 0, len(normalised))
	for sym := range normalised {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols) // deterministic order

	// ── Phase 1: Lookup element properties from DB ────────────────────
	elemData, err := LookupElements(ctx, symbols)
	if err != nil {
		return models.PredictResponse{}, fmt.Errorf("element lookup failed: %w", err)
	}

	// Build element props list + rule-of-mixtures baseline
	var elementProps []ElementProp
	baseline := map[string]float64{
		"density":                0,
		"melting_point":          0,
		"thermal_conductivity":   0,
		"electrical_resistivity": 0,
		"yield_strength":         0,
		"youngs_modulus":         0,
	}
	foundCount := map[string]int{}

	for _, sym := range symbols {
		wt := normalised[sym] / 100.0
		ep := ElementProp{
			Symbol:        sym,
			WeightPercent: normalised[sym],
		}

		if mat, ok := elemData[sym]; ok {
			ep.Density = mat.Density
			ep.MeltingPoint = mat.MeltingPoint
			ep.ThermalConductivity = mat.ThermalConductivity
			ep.ElectricalResistivity = mat.ElectricalResistivity
			ep.YieldStrength = mat.YieldStrength
			ep.YoungsModulus = mat.YoungsModulus

			// Accumulate weighted averages
			if mat.Density != nil {
				baseline["density"] += wt * *mat.Density
				foundCount["density"]++
			}
			if mat.MeltingPoint != nil {
				baseline["melting_point"] += wt * *mat.MeltingPoint
				foundCount["melting_point"]++
			}
			if mat.ThermalConductivity != nil {
				baseline["thermal_conductivity"] += wt * *mat.ThermalConductivity
				foundCount["thermal_conductivity"]++
			}
			if mat.ElectricalResistivity != nil {
				baseline["electrical_resistivity"] += wt * *mat.ElectricalResistivity
				foundCount["electrical_resistivity"]++
			}
			if mat.YieldStrength != nil {
				baseline["yield_strength"] += wt * *mat.YieldStrength
				foundCount["yield_strength"]++
			}
			if mat.YoungsModulus != nil {
				baseline["youngs_modulus"] += wt * *mat.YoungsModulus
				foundCount["youngs_modulus"]++
			}
		}

		elementProps = append(elementProps, ep)
	}

	// Zero out properties where we had no data for any element
	filteredBaseline := make(map[string]float64)
	for prop, val := range baseline {
		if foundCount[prop] > 0 {
			filteredBaseline[prop] = val
		}
	}

	// ── Phase 2: LLM Refinement ───────────────────────────────────────
	llmInput := PredictorLLMInput{
		Composition:  normalised,
		Baseline:     filteredBaseline,
		ElementProps: elementProps,
	}

	llmOut, _, err := RefinePrediction(ctx, llmInput)
	if err != nil {
		// Fall back to rule-of-mixtures if LLM fails
		return buildFallbackResponse(normalised, filteredBaseline, symbols, err.Error()), nil
	}

	// ── Build response ────────────────────────────────────────────────
	resp := models.PredictResponse{
		Composition:   normalised,
		PredictedName: buildAlloyName(symbols, normalised),
		Method:        "Rule-of-Mixtures Baseline + Gemini Thermodynamic Refinement",
	}

	// Refined properties from LLM
	resp.Density = llmOut.RefinedProperties.Density
	resp.MeltingPoint = llmOut.RefinedProperties.MeltingPoint
	resp.ThermalConductivity = llmOut.RefinedProperties.ThermalConductivity
	resp.ElectricalResistivity = llmOut.RefinedProperties.ElectricalResistivity
	resp.YieldStrength = llmOut.RefinedProperties.YieldStrength
	resp.YoungsModulus = llmOut.RefinedProperties.YoungsModulus

	// Combine explanation + phase notes
	notes := ""
	if llmOut.PhaseDiagramNotes != "" {
		notes += "**Phase Diagram:** " + llmOut.PhaseDiagramNotes + "\n\n"
	}
	notes += "**Confidence:** " + llmOut.Confidence
	resp.Notes = notes

	// Build baseline properties map for the response
	baselineProps := buildBaselineProperties(filteredBaseline)
	resp.BaselineProperties = baselineProps
	resp.ScientificExplanation = llmOut.ScientificExplanation

	return resp, nil
}

// buildAlloyName creates a readable alloy name from composition, e.g. "Cu70Zn30 Alloy"
func buildAlloyName(symbols []string, composition map[string]float64) string {
	parts := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		pct := composition[sym]
		parts = append(parts, fmt.Sprintf("%s%.0f", sym, pct))
	}
	return strings.Join(parts, "-") + " Alloy"
}

// buildFallbackResponse returns a rule-of-mixtures-only response when LLM fails.
func buildFallbackResponse(
	composition map[string]float64,
	baseline map[string]float64,
	symbols []string,
	errMsg string,
) models.PredictResponse {
	resp := models.PredictResponse{
		Composition:   composition,
		PredictedName: buildAlloyName(symbols, composition),
		Method:        "Rule-of-Mixtures (LLM refinement unavailable: " + errMsg + ")",
	}
	if v, ok := baseline["density"]; ok {
		resp.Density = &v
	}
	if v, ok := baseline["melting_point"]; ok {
		resp.MeltingPoint = &v
	}
	if v, ok := baseline["thermal_conductivity"]; ok {
		resp.ThermalConductivity = &v
	}
	if v, ok := baseline["electrical_resistivity"]; ok {
		resp.ElectricalResistivity = &v
	}
	if v, ok := baseline["yield_strength"]; ok {
		resp.YieldStrength = &v
	}
	if v, ok := baseline["youngs_modulus"]; ok {
		resp.YoungsModulus = &v
	}
	resp.Notes = "⚠️ LLM enhancement unavailable. Values are rule-of-mixtures estimates only."
	return resp
}

// buildBaselineProperties converts the map to the struct format.
func buildBaselineProperties(baseline map[string]float64) map[string]*float64 {
	result := make(map[string]*float64)
	for k, v := range baseline {
		val := v
		result[k] = &val
	}
	return result
}
