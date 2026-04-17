package models

// Material represents a row in the materials table.
type Material struct {
	ID                    int      `json:"id" db:"id"`
	Name                  string   `json:"name" db:"name"`
	Formula               string   `json:"formula" db:"formula"`
	Category              string   `json:"category" db:"category"`
	Subcategory           *string  `json:"subcategory,omitempty" db:"subcategory"`
	Density               *float64 `json:"density,omitempty" db:"density"`
	GlassTransitionTemp   *float64 `json:"glass_transition_temp,omitempty" db:"glass_transition_temp"`
	HeatDeflectionTemp    *float64 `json:"heat_deflection_temp,omitempty" db:"heat_deflection_temp"`
	MeltingPoint          *float64 `json:"melting_point,omitempty" db:"melting_point"`
	BoilingPoint          *float64 `json:"boiling_point,omitempty" db:"boiling_point"`
	ThermalConductivity   *float64 `json:"thermal_conductivity,omitempty" db:"thermal_conductivity"`
	SpecificHeat          *float64 `json:"specific_heat,omitempty" db:"specific_heat"`
	ThermalExpansion      *float64 `json:"thermal_expansion,omitempty" db:"thermal_expansion"`
	ElectricalResistivity *float64 `json:"electrical_resistivity,omitempty" db:"electrical_resistivity"`
	YieldStrength         *float64 `json:"yield_strength,omitempty" db:"yield_strength"`
	TensileStrength       *float64 `json:"tensile_strength,omitempty" db:"tensile_strength"`
	YoungsModulus         *float64 `json:"youngs_modulus,omitempty" db:"youngs_modulus"`
	HardnessVickers       *float64 `json:"hardness_vickers,omitempty" db:"hardness_vickers"`
	PoissonsRatio         *float64 `json:"poissons_ratio,omitempty" db:"poissons_ratio"`
	ProcessingTempMinC    *float64 `json:"processing_temp_min_c,omitempty" db:"processing_temp_min_c"`
	ProcessingTempMaxC    *float64 `json:"processing_temp_max_c,omitempty" db:"processing_temp_max_c"`
	Crystallinity         *float64 `json:"crystallinity,omitempty" db:"crystallinity"`
	CrystalSystem         *string  `json:"crystal_system,omitempty" db:"crystal_system"`
	FractureToughness     *float64 `json:"fracture_toughness,omitempty" db:"fracture_toughness"`
	WeibullModulus        *float64 `json:"weibull_modulus,omitempty" db:"weibull_modulus"`
	InterlaminarShear     *float64 `json:"interlaminar_shear_strength,omitempty" db:"interlaminar_shear_strength"`
	FiberVolumeFraction   *float64 `json:"fiber_volume_fraction,omitempty" db:"fiber_volume_fraction"`
	Source                string   `json:"source" db:"source"`
	MpMaterialID          *string  `json:"mp_material_id,omitempty" db:"mp_material_id"`
}

// ──────────────────────────────────────────────────────────────────────────
//  API request / response types
// ──────────────────────────────────────────────────────────────────────────

// RecommendRequest is the POST /recommend request body.
type RecommendRequest struct {
	Query       string       `json:"query" binding:"required"`
	Domain      string       `json:"domain"`
	Constraints []Constraint `json:"constraints,omitempty"`
}

// ChatTurn represents a single turn in a conversational thread.
type ChatTurn struct {
	Role    string `json:"role"` // user | assistant
	Content string `json:"content"`
}

// FollowUpChatRequest is used for conversational follow-up after initial recommendation.
type FollowUpChatRequest struct {
	Message            string     `json:"message" binding:"required"`
	History            []ChatTurn `json:"history,omitempty"`
	InitialReport      string     `json:"initial_report,omitempty"`
	TopRecommendations []string   `json:"top_recommendations,omitempty"`
}

// FollowUpChatResponse is a plain conversational assistant response.
type FollowUpChatResponse struct {
	Reply      string `json:"reply"`
	TokensUsed int    `json:"tokens_used,omitempty"`
}

// Constraint holds a single constraint applied by the user
type Constraint struct {
	Key      string      `json:"key"`
	Operator string      `json:"operator"` // "min", "max", "equals", "contains"
	Value    interface{} `json:"value"`
}

// RecommendResponse is the POST /recommend response.
type RecommendResponse struct {
	Query           string     `json:"query"`
	ExtractedIntent IntentJSON `json:"extracted_intent"`
	Recommendations []Material `json:"recommendations"`
	Report          string     `json:"report"` // Gemini reframed markdown
	TokensUsed      int        `json:"tokens_used,omitempty"`
}

// IntentJSON holds the structured LLM-extracted constraints.
type IntentJSON struct {
	Filters  map[string]RangeFilter `json:"filters"`
	Category string                 `json:"category,omitempty"`
	SortBy   string                 `json:"sort_by,omitempty"`
	SortDir  string                 `json:"sort_dir,omitempty"`
}

// RangeFilter holds optional min/max for a property.
type RangeFilter struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// PredictRequest is the POST /predict request body.
type PredictRequest struct {
	// Composition maps element symbol → weight percentage (must sum to 100)
	Composition map[string]float64 `json:"composition" binding:"required"`
}

// PredictResponse is the POST /predict response.
type PredictResponse struct {
	Composition   map[string]float64 `json:"composition"`
	PredictedName string             `json:"predicted_name"`
	// Phase 1: Rule-of-mixtures baseline from DB
	BaselineProperties map[string]*float64 `json:"baseline_properties,omitempty"`
	// Phase 2: LLM-refined predictions
	Density               *float64 `json:"density,omitempty"`
	MeltingPoint          *float64 `json:"melting_point,omitempty"`
	ThermalConductivity   *float64 `json:"thermal_conductivity,omitempty"`
	ElectricalResistivity *float64 `json:"electrical_resistivity,omitempty"`
	YieldStrength         *float64 `json:"yield_strength,omitempty"`
	YoungsModulus         *float64 `json:"youngs_modulus,omitempty"`
	// LLM-generated content
	ScientificExplanation string `json:"scientific_explanation,omitempty"`
	Method                string `json:"method"`
	Notes                 string `json:"notes,omitempty"`
}
