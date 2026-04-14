# 🎯 LLM Dispatcher Implementation Guide

## Overview

This document describes the new **Material Category Dispatcher Logic** that routes user queries to specialized search and physics-driven analysis pipelines. The dispatcher uses LLM classification to intelligently categorize material requests and applies category-specific first-principles physics verification.

---

## Architecture

### Pipeline Flow

```
User Query
    ↓
[1] RouteQuery() - LLM-powered category classification
    ↓
[2] Category-Specific Search Functions
    • SearchPolymers()        → Glass transition, HDT, processing temps
    • SearchAlloys()          → Yield strength, fatigue, corrosion
    • SearchPureMetals()      → Electrical conductivity, thermal properties
    • SearchCeramics()        → Hardness, thermal shock, toughness
    • SearchComposites()      → ILSS, fiber volume fraction, anisotropy
    ↓
[3] ScientificAnalysis() - Physics-driven verification
    • First-principles validation per category
    • Merit index calculation
    • Failure rejection reasoning
    • Manufacturing feasibility checks
    ↓
Top Recommendation + Alternatives + Physics Report
```

---

## Core Functions

### 1. RouteQuery(ctx, query) → category, tokens, error

**Purpose:** LLM-powered intelligent routing of user queries into 5 material categories.

**Input:**
- Natural language query (e.g., "I need a lightweight polymer for 3D printing")

**Output:**
- `category` (string): One of `Polymers|Alloys|Pure_Metals|Ceramics|Composites`
- `confidence` (0.0-1.0): Confidence level of classification
- `reasoning` (string): Why the query was routed to this category

**Example:**
```go
category, tokens, err := services.RouteQuery(ctx, "Need a strong, lightweight metal for aerospace")
// Returns: "Alloys", 287 tokens, nil

category, tokens, err := services.RouteQuery(ctx, "3D printing material with good thermal stability")
// Returns: "Polymers", 245 tokens, nil
```

**Classification Rules:**
| Category | Keywords | Priority Property |
|----------|----------|-------------------|
| **Polymers** | plastic, polymer, 3D print, resin, ABS, PEEK, PLA, flexible | Glass Transition ($T_g$) |
| **Alloys** | alloy, steel, aluminum, temper, 6061, 7075, yield strength | Yield Strength ($\sigma_y$) |
| **Pure_Metals** | pure metal, copper, tungsten, elemental, pure aluminum | Electrical Conductivity |
| **Ceramics** | ceramic, oxide, carbide, Al2O3, SiC, thermal shock | Hardness (Vickers) |
| **Composites** | composite, fiber, CFRP, GFRP, laminate, anisotropic | Interlaminar Shear (ILSS) |

---

### 2. Specialized Search Functions

#### SearchPolymers(ctx, constraints, materials, limit) → candidates[]

**Category-Specific Filters:**
- `min_glass_transition_temp` (K): Minimum Tg for service temp constraints
- `max_glass_transition_temp` (K): Maximum allowed Tg
- `min_hdt` (K): Heat deflection temperature minimum
- `max_processing_temp` (K): Processing temperature ceiling
- `min_crystallinity` (%): Crystallinity requirement (affects stiffness)
- `max_density` (kg/m³): Weight constraint

**Sort Priority:** Glass Transition Temperature (descending)

**First-Principles Check:**
$$T_{service} < 0.8 \times T_g \quad \text{(avoid viscoelastic creep)}$$

**Example:**
```go
constraints := map[string]interface{}{
    "min_glass_transition_temp": 373.15,  // 100°C minimum
    "max_processing_temp": 480.0,         // 207°C max processing
    "max_density": 1200.0,                // Lightweight requirement
}
candidates := services.SearchPolymers(ctx, constraints, allMaterials, 3)
// Returns: [PEEK, PEI, Polycarbonate] sorted by Tg descending
```

---

#### SearchAlloys(ctx, constraints, materials, limit) → candidates[]

**Category-Specific Filters:**
- `min_yield_strength` (Pa): Minimum strength requirement
- `max_melting_point` (K): Processing thermal limit
- `min_corrosion_resistance` (Ω·m): Electrical resistivity proxy for corrosion
- `min_youngs_modulus` (Pa): Stiffness requirement (fatigue proxy)

**Sort Priority:** Yield Strength (descending)

**First-Principles Check:**
$$\text{Specific Strength} = \frac{\sigma_y}{\rho} \quad \text{(strength-to-weight ratio)}$$

**Example:**
```go
constraints := map[string]interface{}{
    "min_yield_strength": 400e6,      // 400 MPa minimum
    "max_melting_point": 1600.0,      // Hard to weld above this
}
candidates := services.SearchAlloys(ctx, constraints, allMaterials, 3)
// Returns: [7075-T6, 6061-T6, 2024-T4] for aircraft structures
```

---

#### SearchCeramics(ctx, constraints, materials, limit) → candidates[]

**Category-Specific Filters:**
- `min_hardness_vickers` (HV): Primary ceramic property
- `min_fracture_toughness` (MPa√m): Thermal shock resistance
- `min_melting_point` (K): High-temperature capability
- `min_thermal_conductivity` (W/m·K): Heat dissipation
- `min_youngs_modulus` (Pa): Stiffness

**Sort Priority:** Hardness Vickers (descending)

**First-Principles Check:**
$$R = \frac{\sigma_f \cdot k}{E \cdot \alpha} \quad \text{(thermal shock resistance)}$$

Where:
- $\sigma_f$ = fracture strength
- $k$ = thermal conductivity
- $E$ = Young's modulus
- $\alpha$ = thermal expansion coefficient

**Example:**
```go
constraints := map[string]interface{}{
    "min_hardness_vickers": 1200.0,    // Ultra-hard tool requirement
    "min_fracture_toughness": 4.0,     // Avoid brittle failures
}
candidates := services.SearchCeramics(ctx, constraints, allMaterials, 3)
// Returns: [SiC, Al2O3, Si3N4] for cutting tools
```

---

#### SearchComposites(ctx, constraints, materials, limit) → candidates[]

**Category-Specific Filters:**
- `min_ilss` (MPa): Interlaminar shear strength (composite integrity)
- `min_fiber_volume_fraction` (%): Quality indicator (>50% = high quality)
- `min_youngs_modulus` (Pa): Stiffness requirement
- `max_density` (kg/m³): Weight constraint
- `min_thermal_conductivity` (W/m·K): Thermal management

**Sort Priority:** Interlaminar Shear Strength (descending)

**First-Principles Check:**
$$E_{specific} = \frac{E}{\rho} \quad \text{(specific modulus for lightweight structures)}$$

**Example:**
```go
constraints := map[string]interface{}{
    "min_ilss": 50.0,                    // Minimum matrix strength
    "min_fiber_volume_fraction": 60.0,   // High-quality fiber content
    "max_density": 1600.0,               // Aerospace weight limit
}
candidates := services.SearchComposites(ctx, constraints, allMaterials, 3)
// Returns: [CFRP 60%, Carbon/PEEK, GFRP] for aircraft structures
```

---

#### SearchPureMetals(ctx, constraints, materials, limit) → candidates[]

**Category-Specific Filters:**
- `max_electrical_resistivity` (Ω·m): Purity indicator (lower = purer)
- `min_melting_point` (K): High-temperature requirement
- `min_thermal_conductivity` (W/m·K): Thermal performance
- `min_density` / `max_density` (kg/m³): Density range

**Sort Priority:** Thermal Conductivity (descending)

**First-Principles Check:**
$$\rho_e = \frac{m_e}{n_e \cdot q \cdot \mu} \quad \text{(electrical resistivity relates to purity)}$$

**Example:**
```go
constraints := map[string]interface{}{
    "max_electrical_resistivity": 1e-7,  // Highly conductive copper
    "min_thermal_conductivity": 380.0,   // Heat sink requirement
}
candidates := services.SearchPureMetals(ctx, constraints, allMaterials, 3)
// Returns: [Copper, Silver, Aluminum] for thermal management
```

---

### 3. ScientificAnalysis(ctx, query, category, topCandidates) → analysis, tokens, error

**Purpose:** Apply rigorous first-principles physics verification to the top 3 candidates.

**Output Structure:**
```json
{
  "top_candidate": "Material Name",
  "physics_verification": {
    "check_1_name": "PASS|FAIL",
    "check_1_value": "5.2 MPa√m",
    "check_1_physics": "Fracture toughness exceeds 3 MPa√m minimum for ceramic applications"
  },
  "merit_index_calculation": "R = σ_f·k/(E·α) = 2.1 K·W⁻¹ (thermal shock resistance)",
  "failure_rejection_reasons": [
    "Material_A: Glass transition temp too low for 150°C service (Tg=350K < 0.8×T_service)",
    "Material_B: Processing temp exceeds equipment capability (480K > 450K limit)"
  ],
  "manufacturing_feasibility": "1. Preheat tooling to 200°C\n2. Use carbide cutting tools\n3. Anneal post-process...",
  "safety_margin": "Applied 1.5× safety factor for dynamic loads; yield margin = 67%"
}
```

**Physics Verification Protocol:**

**For POLYMERS:**
- ✅ Service Temp < 0.8 × Tg (viscoelastic creep limit)
- ✅ Processing Temp < HDT (manufacturability)
- ✅ UV stability check (if exposed)
- 📊 Metric to maximize: $T_g - T_{service}$ (thermal headroom)

**For METALS/ALLOYS:**
- ✅ Specific Strength ($\sigma_y/\rho$) vs demand
- ✅ Yield strength with 1.5× safety factor
- ✅ Fatigue limit ≈ 0.3–0.6 × Ultimate Tensile Strength
- 📊 Metric to maximize: $\sigma_y/\rho$ (strength-to-weight)

**For CERAMICS:**
- ✅ Thermal Shock Resistance: $R = \sigma_f k / (E \alpha)$
- ✅ Fracture Toughness ≥ 3 MPa√m for impact resistance
- ✅ Weibull Modulus ≥ 10 for reliability
- 📊 Metric to maximize: $R$ (thermal shock resistance)

**For COMPOSITES:**
- ✅ Fiber orientation matches load direction (0°/90°/±45°)
- ✅ ILSS ≥ 50 MPa minimum for matrix integrity
- ✅ Fiber volume fraction ≥ 50% indicates quality
- 📊 Metric to maximize: $E/\rho$ (specific modulus)

---

## API Endpoint

### POST /api/v1/recommend/dispatcher

**Request:**
```json
{
  "query": "I need a strong, lightweight polymer for 3D printing that can handle 100°C",
  "domain": "Polymer Processing"  // Optional domain specification
}
```

**Response:**
```json
{
  "query": "I need a strong, lightweight polymer for 3D printing that can handle 100°C",
  "routed_category": "Polymers",
  "category_candidates": [
    {
      "id": 42,
      "name": "PEEK",
      "category": "Polymer",
      "glass_transition_temp": 416.15,
      "heat_deflection_temp": 433.15,
      "processing_temp_min_c": 311.0,
      "processing_temp_max_c": 339.0,
      "density": 1320.0
    },
    // ... more candidates
  ],
  "physics_analysis": {
    "top_candidate": "PEEK",
    "physics_verification": {
      "tg_check": "PASS",
      "tg_value": "416.15 K (143°C)",
      "tg_physics": "Service temp (373K) < 0.8×Tg (333K) — safe from creep"
    },
    "merit_index_calculation": "Thermal headroom = Tg - T_service = 43K (excellent)",
    "failure_rejection_reasons": [
      "Polystyrene: Tg=380K, too close to service limit (0.8×Tg=304K)",
      "PLA: Tg=338K, below 373K service requirement"
    ],
    "manufacturing_feasibility": "1. Preheat nozzle to 320-340°C\n2. Use hardened steel nozzle (wears quickly)\n3. Print at 30mm/s max\n4. Anneal at 200°C for 1hr post-print"
  },
  "top_recommendation": { /* PEEK full material data */ },
  "alternative_options": [ /* PEI, PC /* ],
  "total_tokens_used": 1247,
  "pipeline_explanation": "Pipeline Steps:\n✅ Query routed to: Polymers\n✅ Loaded 1621 materials from database\n🔍 SearchPolymers: found 3 candidates\n🔬 Physics verification completed\n✅ Top recommendation: PEEK"
}
```

---

## Implementation Details

### File Structure

```
backend/
├── services/
│   ├── llm.go
│   │   ├── RouteQuery()              [NEW]
│   │   ├── SearchAlloys()            [NEW]
│   │   ├── SearchPolymers()          [NEW]
│   │   ├── SearchCeramics()          [NEW]
│   │   ├── SearchComposites()        [NEW]
│   │   ├── SearchPureMetals()        [NEW]
│   │   ├── ScientificAnalysis()      [NEW]
│   │   └── ... (existing LLM functions)
│   │
│   └── search.go
│       └── GetAllMaterials()         [Already existed in csv_db.go]
│
├── handlers/
│   ├── recommend.go
│   │   ├── Recommend()               [EXISTING]
│   │   └── RecommendWithDispatcher() [NEW]
│   └── ... (other handlers)
│
└── main.go
    ├── Route: POST /api/v1/recommend              [EXISTING]
    └── Route: POST /api/v1/recommend/dispatcher   [NEW]
```

### LLM Prompts

#### RouteQuery System Prompt

```
Classification keywords per category:
- Polymers: plastic, polymer, 3D print, resin, ABS, PEEK, PLA, flexible
- Alloys: alloy, steel, aluminum, temper, 6061, 7075, yield strength
- Pure_Metals: pure metal, copper, tungsten, elemental
- Ceramics: ceramic, oxide, carbide, Al2O3, SiC, thermal shock
- Composites: composite, fiber, CFRP, GFRP, laminate
```

#### ScientificAnalysis System Prompt

Physics verification rules by material type (see Section 3 above).

---

## Usage Examples

### Example 1: Aerospace Alloy Selection

**Query:**
```
"I'm designing a lightweight bracket for an aircraft fuselage. 
It needs high strength, low density, and good fatigue resistance. 
We'll use CNC machining and need parts by next week."
```

**Execution:**
1. **RouteQuery** → "Alloys" (high confidence: 0.92)
2. **SearchAlloys** filters for:
   - Yield strength > 300 MPa (structural requirement)
   - Density < 3.0 kg/m³ (weight limit)
   - Melting point < 1500 K (CNC processability)
3. **ScientificAnalysis** verifies:
   - 7075-T6: Specific strength = 300 × 10⁶ Pa / 2810 kg/m³ = 106 kN·m/kg ✅
   - Fatigue limit checked against ultimate tensile strength
   - 1.5× safety factor applied
4. **Result**: 7075-T6 recommended + 6061-T6 alternative

---

### Example 2: 3D Printing Material Selection

**Query:**
```
"Need a durable polymer for 3D printing enclosures. 
Service temperature up to 80°C, must resist UV, good dimensional stability."
```

**Execution:**
1. **RouteQuery** → "Polymers" (confidence: 0.95)
2. **SearchPolymers** filters for:
   - Glass transition temp > 353 K (80°C service)
   - HDT > 353 K (no deformation)
   - Processing temp < 520 K (FDM printer limit)
   - max_crystallinity > 0.3 (dimensional stability)
3. **ScientificAnalysis** verifies:
   - PEEK: Tg = 416 K, Service = 353 K
   - Thermal headroom = 0.8 × 416 - 353 = 80 K ✅ (safe margin)
   - UV resistance: PEEK has stabilizers ✅
4. **Result**: PEEK recommended + PEI, Polycarbonate alternatives

---

### Example 3: Ceramic Cutting Tool Selection

**Query:**
```
"High-speed cutting tool for cast iron machining. 
Needs extreme hardness and won't see temperatures above 1000°C."
```

**Execution:**
1. **RouteQuery** → "Ceramics" (confidence: 0.89)
2. **SearchCeramics** filters for:
   - Hardness Vickers > 1200 HV
   - Melting point > 1800 K (process capability)
   - Fracture toughness > 3 MPa√m (avoid chipping)
3. **ScientificAnalysis** computes thermal shock resistance:
   - $R = \sigma_f \cdot k / (E \cdot \alpha)$
   - Ranks by R value for tool life prediction
4. **Result**: SiC recommended + Al2O3, Si₃N₄ alternatives

---

## Error Handling & Fallbacks

### Scenario 1: RouteQuery LLM Failure

```go
// If RouteQuery fails, fallback to ExtractIntent()
routedCategory, routeTokens, err := services.RouteQuery(ctx, req.Query)
if err != nil || routedCategory == "" {
    log.Printf("⚠️  RouteQuery failed, using ExtractIntent fallback")
    intent, tokens, _ := services.ExtractIntent(ctx, req.Query)
    routedCategory = intent.Category
    totalTokensUsed += tokens
}
```

### Scenario 2: No Candidates Found

```go
if len(candidates) == 0 {
    return DispatcherResponse{
        Query: req.Query,
        RoutedCategory: routedCategory,
        CategoryCandidates: []models.Material{},
        PipelineExplanation: "No materials found in category: " + routedCategory,
    }
}
```

### Scenario 3: ScientificAnalysis LLM Failure

```go
analysis, analysisTokens, err := services.ScientificAnalysis(ctx, query, category, candidates)
if err != nil {
    log.Printf("⚠️  ScientificAnalysis failed: %v", err)
    // Use first candidate as fallback recommendation
    analysis.TopCandidate = candidates[0].Name
}
```

---

## Performance Considerations

### Token Usage Estimates

| Operation | Tokens | Time (est.) |
|-----------|--------|------------|
| RouteQuery | 200-300 | 1-2s |
| ExtractIntent | 300-400 | 1-2s |
| ScientificAnalysis (3 materials) | 1200-1500 | 3-5s |
| **Total per request** | **1700-2200** | **5-9s** |

### Optimization Strategies

1. **Category-specific material pre-filtering** (reduces ScientificAnalysis payload)
2. **Token-optimized prompts** (concise, no redundancy)
3. **Parallel LLM calls** (RouteQuery + ExtractIntent simultaneously)
4. **Cached category-specific CSVs** (faster search than full DB query)

---

## Testing

### Unit Test Scenarios

```go
func TestRouteQuery_Polymers(t *testing.T) {
    ctx := context.Background()
    query := "I need a plastic for 3D printing"
    
    category, _, err := RouteQuery(ctx, query)
    
    if category != "Polymers" {
        t.Errorf("expected Polymers, got %s", category)
    }
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
}

func TestSearchPolymers_GlassTransitionFilter(t *testing.T) {
    materials := []Material{
        {Name: "PEEK", Category: "Polymer", GlassTransitionTemp: ptr(416.15)},
        {Name: "PLA", Category: "Polymer", GlassTransitionTemp: ptr(338.15)},
    }
    constraints := map[string]interface{}{
        "min_glass_transition_temp": 380.0,
    }
    
    result := SearchPolymers(context.Background(), constraints, materials, 3)
    
    if len(result) != 1 || result[0].Name != "PEEK" {
        t.Errorf("expected [PEEK], got %v", result)
    }
}

func TestScientificAnalysis_PhysicsVerification(t *testing.T) {
    ctx := context.Background()
    candidates := []Material{
        {ID: 1, Name: "PEEK", Category: "Polymer", GlassTransitionTemp: ptr(416.15)},
    }
    
    analysis, _, _ := ScientificAnalysis(ctx, "3D printing at 100C", "Polymers", candidates)
    
    if analysis.TopCandidate != "PEEK" {
        t.Errorf("expected PEEK as top candidate")
    }
}
```

---

## Integration with Existing System

The dispatcher is **additive** — it doesn't break existing endpoints:

- **Existing:** `POST /api/v1/recommend` continues working as-is
- **New:** `POST /api/v1/recommend/dispatcher` provides enhanced routing

Both endpoints share the same LLM infrastructure and database, but the dispatcher adds:
1. LLM-powered category routing (vs. manual domain selection)
2. Category-specific search filters
3. Physics-driven verification pipeline

---

## Future Enhancements

1. **Multi-attribute Optimization:** Pareto frontier for conflicting requirements
2. **Cost Optimization:** Integrate material cost data into merit index
3. **Supply Chain Risk:** Check material availability and lead times
4. **Environmental Impact:** LCA score in physics verification
5. **Machine Learning:** Learn category boundaries from historical recommendations

---

## References

- Material Physics: ASM Handbook Volume 15 (Casting)
- Composite Mechanics: Jones, *Mechanics of Composite Materials* (2nd Ed.)
- Thermal Analysis: Incropera & DeWitt, *Heat Transfer* Fundamentals
- LLM Integration: OpenAI API & Google Gemini docs

