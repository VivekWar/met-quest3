package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vivek/met-quest/models"
)

const openRouterBaseURL = "https://openrouter.ai/api/v1/chat/completions"

// ──────────────────────────────────────────────────────────────────────────
//  AI Provider types
// ──────────────────────────────────────────────────────────────────────────

// OpenRouter (OpenAI-compatible) formats
type openRouterRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// Google AI Studio (Native) formats
type googleAIRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
	GenerationConfig struct {
		Temperature      float64 `json:"temperature"`
		MaxOutputTokens  int     `json:"maxOutputTokens"`
		ResponseMimeType string  `json:"responseMimeType,omitempty"`
	} `json:"generationConfig"`
}

type googleAIResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// ──────────────────────────────────────────────────────────────────────────
//  Core LLM call (Provider-Aware: OpenRouter or Google Native)
// ──────────────────────────────────────────────────────────────────────────
//  Core LLM call (Provider-Aware + High-Availability Fallback)
// ──────────────────────────────────────────────────────────────────────────

func callGemini(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, int, error) {
	googleKey := os.Getenv("GEMINI_API_KEY")
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")

	// 1. Initial Key Validation & Mock Mode
	validGoogle := googleKey != "" && !strings.Contains(googleKey, "Dummy") && !strings.Contains(googleKey, "your_")
	validOR := openRouterKey != "" && !strings.Contains(openRouterKey, "Dummy") && !strings.Contains(openRouterKey, "your_")

	if !validGoogle && !validOR {
		log.Printf("⚠️  No valid API Keys found (G: %v, OR: %v). Using MOCK AI response.", googleKey != "", openRouterKey != "")
		return getMockResponse(prompt)
	}

	// 2. Resilience Hierarchy

	var lastErr error

	// Tier 1: Google Native (Preferred)
	activeGoogleKey := ""
	if strings.HasPrefix(googleKey, "AIza") {
		activeGoogleKey = googleKey
	} else if strings.HasPrefix(openRouterKey, "AIza") {
		activeGoogleKey = openRouterKey
	} else if validGoogle {
		activeGoogleKey = googleKey
	}

	if activeGoogleKey != "" {
		googleModels := []string{
			"gemini-3.1-flash-lite-preview",
			"gemini-3-flash-preview",
			"gemini-2.5-flash",
			"gemini-2.5-pro",
		}
		log.Printf("🛡️  Attempting Google AI Tier (Key: %s)", maskKey(activeGoogleKey))
		for _, model := range googleModels {
			// Try v1beta first
			text, tokens, status, err := callGoogleAI(ctx, activeGoogleKey, model, prompt, temperature, maxTokens, "v1beta")

			// If 404, try stable v1 (some regions/models differ)
			if status == http.StatusNotFound {
				log.Printf("⚠️  Model %s not found on v1beta. Attempting v1 fallback...", model)
				text, tokens, status, err = callGoogleAI(ctx, activeGoogleKey, model, prompt, temperature, maxTokens, "v1")
			}

			if err == nil {
				return text, tokens, nil
			}
			lastErr = err
			// Fallback if status is 4xx/5xx (except 401 Unauthorized which usually means bad key)
			if status != http.StatusUnauthorized && status != 0 {
				log.Printf("⚠️  Google AI %s failed (%d): %v", model, status, err)
				continue
			}
			log.Printf("❌ Google AI Fatal Error (%d): %v", status, err)
			break
		}
	}

	// Tier 2: OpenRouter Fallback
	if validOR && !strings.HasPrefix(openRouterKey, "AIza") {
		log.Printf("🤖 Attempting OpenRouter Fallback (Key: %s)", maskKey(openRouterKey))
		text, tokens, _, err := callOpenRouter(ctx, openRouterKey, prompt, temperature, maxTokens)
		if err == nil {
			return text, tokens, nil
		}
		return "", 0, fmt.Errorf("all LLM providers failed: %w", err)
	}

	// 3. Final Error Assembly
	detailedErr := "no viable AI provider or key available"
	if lastErr != nil {
		detailedErr = lastErr.Error()
	}

	skipReason := ""
	if validOR && strings.HasPrefix(openRouterKey, "AIza") {
		skipReason = " (OpenRouter tier skipped: key starts with 'AIza' — check if you accidentally pasted a Google key into OPENROUTER_API_KEY)"
	}

	return "", 0, fmt.Errorf("%s%s (Keys checked - Google: %v, OpenRouter: %v)", detailedErr, skipReason, activeGoogleKey != "", validOR)
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "...." + key[len(key)-4:]
}

func getMockResponse(prompt string) (string, int, error) {
	if strings.Contains(prompt, "Virtual Materials Scientist") {
		mockJSON := `{"recommended_ids": [1, 2, 3], "report": "## 🏆 Recommendation\n(Mock Mode: AI is currently resting)"}`
		return mockJSON, 100, nil
	} else if strings.Contains(prompt, "Category") || strings.Contains(prompt, "filters") {
		return `{"category": "Metal", "filters": {}}`, 20, nil
	}
	return `{"refined_properties": {"density": 6.5}, "scientific_explanation": "Mock Prediction"}`, 80, nil
}

// callOpenRouter handles calls to the OpenRouter proxy
func callOpenRouter(ctx context.Context, apiKey, prompt string, temperature float64, maxTokens int) (string, int, int, error) {
	payload := openRouterRequest{
		Model: "google/gemini-3-flash-preview",
		Messages: []openRouterMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, openRouterBaseURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "http://localhost:5173")
	req.Header.Set("X-Title", "Smart Alloy Selector")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}

	var result openRouterResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Choices) == 0 {
		return "", 0, resp.StatusCode, fmt.Errorf("empty response")
	}

	return result.Choices[0].Message.Content, result.Usage.TotalTokens, resp.StatusCode, nil
}

// cleanJSON extracts a JSON block from potentially messy LLM output (e.g. markdown fences)
// repairJSON tries to fix common truncation issues in JSON strings
func repairJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// 1. Ensure it starts with {
	if !strings.HasPrefix(raw, "{") {
		idx := strings.Index(raw, "{")
		if idx == -1 {
			return raw
		}
		raw = raw[idx:]
	}

	// 2. Count braces and quotes
	openBraces := strings.Count(raw, "{")
	closeBraces := strings.Count(raw, "}")
	openQuotes := strings.Count(raw, "\"")

	// If quotes are odd, we likely cut off mid-string
	if openQuotes%2 != 0 {
		raw += "\""
	}

	// Close arrays if needed
	if strings.Count(raw, "[") > strings.Count(raw, "]") {
		raw += "]"
	}

	// Close braces
	for openBraces > closeBraces {
		raw += "}"
		closeBraces++
	}

	return raw
}

func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	// Try to find the first '{' and last '}'
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")

	if start != -1 && end != -1 && end > start {
		return raw[start : end+1]
	}
	// Fallback to simpler trimming if braces aren't found cleanly
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

// ──────────────────────────────────────────────────────────────────────────
//  1. Intent Extraction
//
// ──────────────────────────────────────────────────────────────────────────
const intentSystemPrompt = `### ROLE: Principal Materials Systems Architect (Specialist in Requirement Engineering)
### TASK: Structural Intent Decomposition & Constraint Mapping

You are a senior engineer at a top-tier materials laboratory. Your job is to translate messy, non-technical user queries into a precise, searchable JSON schema for a high-fidelity materials database.

### 1. SCIENTIFIC TAXONOMY & METRIC SELECTION
You must dynamically assign the "Search Priority" based on the material class:
- **POLYMERS**: Priority = $T_g$ (Glass Transition), HDT (Heat Deflection), and Viscoelastic Modulus. *Note: Ignore $T_m$ (Melting Point) as a structural limit; it is a processing limit only.*
- **METALS**: Priority = $\sigma_y$ (Yield Strength), $K_{1c}$ (Fracture Toughness), and Fatigue Limit.
- **CERAMICS/GLASS**: Priority = Weibull Modulus, Hardness, and Thermal Shock Resistance ($R$).
- **COMPOSITES**: Priority = Specific Stiffness ($E/\rho$), Interlaminar Shear Strength, and Fiber Volume Fraction.

### 2. PRAGMATIC FEASIBILITY FILTERING
Analyze the "Implicit Manufacturing Context":
- **"Desktop/Standard"**: Flag as "Low-Energy/Low-Temp" processing. Maximum nozzle temp < 270°C, No heated chamber, No CNC coolant.
- **"Industrial/Custom"**: Flag as "High-Performance" processing. Allow PEEK, Ultem, Tool Steels.
- **"Lightweight"**: Map to a search for Performance Indices ($P = \sigma/\rho$ or $P = E/\rho$).

### 3. OUTPUT SCHEMA (STRICT JSON ONLY)
{
	"material_class": "Metal|Polymer|Ceramic|Composite|null",
	"search_filters": {
		"primary_metric": {"field": "string", "min": float|null, "max": float|null, "unit": "SI"},
		"secondary_metrics": [{"field": "string", "min": float|null}]
	},
	"processing_constraints": {
		"method": "string",
		"capability_level": "Hobbyist|Professional|Industrial",
		"thermal_ceiling_kelvin": float|null
	},
	"physics_notes": "A 1-sentence technical justification for these specific limits."
}

Return JSON only. Do not include markdown code fences or extra text.`

// ExtractIntent parses a natural language query into structured filters.
func ExtractIntent(ctx context.Context, query string) (models.IntentJSON, int, error) {
	type intentLLMResponse struct {
		MaterialClass string `json:"material_class"`
		SearchFilters struct {
			PrimaryMetric struct {
				Field string   `json:"field"`
				Min   *float64 `json:"min"`
				Max   *float64 `json:"max"`
				Unit  string   `json:"unit"`
			} `json:"primary_metric"`
			SecondaryMetrics []struct {
				Field string   `json:"field"`
				Min   *float64 `json:"min"`
			} `json:"secondary_metrics"`
		} `json:"search_filters"`
		ProcessingConstraints struct {
			Method               string   `json:"method"`
			CapabilityLevel      string   `json:"capability_level"`
			ThermalCeilingKelvin *float64 `json:"thermal_ceiling_kelvin"`
		} `json:"processing_constraints"`
		PhysicsNotes string `json:"physics_notes"`
	}

	prompt := intentSystemPrompt + "\n\nQuery: " + query

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 512)
	if err != nil {
		return models.IntentJSON{}, 0, fmt.Errorf("intent extraction LLM call: %w", err)
	}

	// Try to clean and repair the JSON
	cleaned := cleanJSON(raw)
	repaired := repairJSON(cleaned)

	var llmIntent intentLLMResponse
	if err := json.Unmarshal([]byte(repaired), &llmIntent); err != nil {
		log.Printf("WARN: Failed to parse intent JSON: %v\nRaw: %s\nRepaired: %s", err, raw, repaired)
		// Return empty intent — search will fall back to a general query
		return models.IntentJSON{}, tokens, nil
	}

	intent := models.IntentJSON{
		Filters:  map[string]models.RangeFilter{},
		Category: llmIntent.MaterialClass,
	}

	if llmIntent.SearchFilters.PrimaryMetric.Field != "" {
		intent.Filters[llmIntent.SearchFilters.PrimaryMetric.Field] = models.RangeFilter{Min: llmIntent.SearchFilters.PrimaryMetric.Min, Max: llmIntent.SearchFilters.PrimaryMetric.Max}
		if llmIntent.SearchFilters.PrimaryMetric.Field == "density" || llmIntent.SearchFilters.PrimaryMetric.Field == "thermal_conductivity" {
			intent.SortBy = llmIntent.SearchFilters.PrimaryMetric.Field
			intent.SortDir = "ASC"
		} else {
			intent.SortBy = llmIntent.SearchFilters.PrimaryMetric.Field
			intent.SortDir = "DESC"
		}
	}

	for _, metric := range llmIntent.SearchFilters.SecondaryMetrics {
		if metric.Field == "" {
			continue
		}
		intent.Filters[metric.Field] = models.RangeFilter{Min: metric.Min}
	}

	if intent.Category == "" || strings.EqualFold(intent.Category, "null") {
		intent.Category = ""
	}

	log.Printf("Intent extracted: category=%q sort_by=%q filters=%d", intent.Category, intent.SortBy, len(intent.Filters))
	return intent, tokens, nil
}

// ──────────────────────────────────────────────────────────────────────────
//  Long-Context AI Engine (Replaces RAG Intent Extraction filter)
// ──────────────────────────────────────────────────────────────────────────

const longContextSystemPrompt = `### ROLE: Chief Materials Scientist & Manufacturing Consultant
### PHILOSOPHY: "Physics is non-negotiable, but Feasibility is the priority."

You are reviewing a catalog of materials retrieved from a RAG system. You must act as a mentor, guiding a junior engineer through a complex selection process.

### STAGE 1: THE ASHBY SCREENING (Logic-Gate)
1. **The 'Hard Limit' Check**: Immediately reject any material where the operating temperature $T_{op} > 0.8 \times T_g$ (for polymers) or $T_{op} > 0.4 \times T_m$ (for metals - Creep concern).
2. **Manufacturing Sanity Check**: If the user is using a "Standard Desktop Printer," and the material is a high-temp thermoplastic (Ultem, PEEK, PPSU), you MUST reject it regardless of its strength. It is a "Paper Tiger"—strong on a datasheet, impossible on their desk.

### STAGE 2: PERFORMANCE INDEX ANALYSIS
Calculate the "Merit Index" internally. If the user wants a "Stiff and Light" part, prioritize materials with the highest $E^{1/2}/\rho$ for plates or $E/\rho$ for rods.

### STAGE 3: THE VETERAN'S NARRATIVE (Output Style)
Provide the response in the following structured format:

1. **The Executive Recommendation**: Identify the "Sweet Spot" material.
2. **The "Physics Why"**: Use deep scientific terminology. Don't say "it's strong"; say "It exhibits superior dimensional stability due to its high $T_g$ and low coefficient of thermal expansion (CTE)."
3. **The Comparative Critique**: Explain why you REJECTED common alternatives. (e.g., "While PLA is easier to print, its low $T_g$ of ~60°C makes it a liability near a high-torque motor.")
4. **Feasibility Audit**: A "Shop Floor" warning. If they choose this, what is the one thing they will struggle with? (e.g., "Hygroscopic nature—must be dried for 4 hours before use.")

### OUTPUT FORMAT (STRICT JSON):
{
  "top_candidate": "Material Name",
  "fundamental_stats": {
    "stat_name": "Value + Unit (Why it matters in this context)"
  },
  "engineering_analysis": "Intensive, first-principles justification.",
  "alternative_rejection": "Why the obvious/cheap choice will fail.",
  "feasibility_warning": "Real-world manufacturing advice."
}

Return JSON only. Ensure strings are escaped and do not include markdown code fences or extra text outside JSON.`

type LongContextLLMResponse struct {
	TopCandidate         string            `json:"top_candidate"`
	FundamentalStats     map[string]string `json:"fundamental_stats"`
	EngineeringAnalysis  string            `json:"engineering_analysis"`
	AlternativeRejection string            `json:"alternative_rejection"`
	FeasibilityWarning   string            `json:"feasibility_warning"`
	RecommendedIDs       []int             `json:"recommended_ids"`
	ReportMarkdown       string            `json:"report_markdown"`
	LegacyReport         string            `json:"report"`
	Report               string            `json:"-"`
}

// FilterByDomain cleanly separates the 8,000+ db into domain buckets.
func FilterByDomain(domain string, all []models.Material) []models.Material {
	if domain == "" || domain == "Overall (Top 1000)" {
		if len(all) > 1000 {
			return all[:1000]
		}
		return all
	}

	var filtered []models.Material
	for _, m := range all {
		cat := m.Category
		sub := ""
		if m.Subcategory != nil {
			sub = *m.Subcategory
		}
		density := 100.0
		if m.Density != nil {
			density = *m.Density
		}
		meltPt := 0.0
		if m.MeltingPoint != nil {
			meltPt = *m.MeltingPoint
		}
		yield := 0.0
		if m.YieldStrength != nil {
			yield = *m.YieldStrength
		}

		match := false
		switch domain {
		case "Aerospace & Aviation":
			match = density < 5.0 || cat == "Superalloy" || sub == "Refractory" || cat == "Composite"
		case "Automotive & Transportation":
			match = (cat == "Metal" && (sub == "Ferrous" || density < 3.0)) || cat == "Polymer" || yield > 200
		case "Marine & Naval":
			match = (cat == "Metal" && (sub == "Non-Ferrous" || sub == "Superalloy")) || cat == "Polymer"
		case "Medical & Biomedical":
			match = cat == "Polymer" || sub == "Bioceramic" || (cat == "Metal" && density < 6.0) || sub == "Ferrous"
		case "Electronics & Photonics":
			match = cat == "Semiconductor" || sub == "Precious" || (m.ElectricalResistivity != nil && *m.ElectricalResistivity < 1e-6)
		case "Construction & Structural":
			match = sub == "Ferrous" || cat == "Ceramic" || yield > 300
		case "High-Temperature / Refractory":
			match = meltPt > 1800 || sub == "Refractory" || sub == "Superalloy" || sub == "Carbide" || sub == "Nitride"
		case "Tooling & Wear-Resistant":
			match = sub == "Carbide" || sub == "Nitride" || sub == "Carbon" || (m.HardnessVickers != nil && *m.HardnessVickers > 500)
		case "Plastics & Polymers":
			match = cat == "Polymer"
		case "Advanced Composites":
			match = cat == "Composite"
		default:
			match = true // Fallback
		}

		if match {
			filtered = append(filtered, m)
		}
	}

	// Safety cap: even if domain is large, cap to 1000 to keep speed high and tokens low
	if len(filtered) > 1000 {
		return filtered[:1000]
	}
	return filtered
}

// LongContextAnalyze bypasses intermediate strict filtering, sending the entire DB natively to the LLM.
func LongContextAnalyze(ctx context.Context, originalQuery string, domain string, allMaterials []models.Material) (LongContextLLMResponse, int, error) {

	// Create a massively stripped down version of materials to save tokens
	type MinMat struct {
		ID int      `json:"id"`
		N  string   `json:"name"`
		C  string   `json:"category"`
		D  *float64 `json:"density,omitempty"`
		MP *float64 `json:"melt_pt,omitempty"`
		YS *float64 `json:"yield_str,omitempty"`
		YM *float64 `json:"youngs_mod,omitempty"`
		R  *float64 `json:"resistivity,omitempty"`
		TC *float64 `json:"therm_cond,omitempty"`
	}

	// 1. Filter raw materials by domain to restrict LLM token flood
	allMaterials = FilterByDomain(domain, allMaterials)

	var minDB []MinMat
	for _, m := range allMaterials {
		minDB = append(minDB, MinMat{
			ID: m.ID, N: m.Name, C: m.Category,
			D: m.Density, MP: m.MeltingPoint, YS: m.YieldStrength,
			YM: m.YoungsModulus, R: m.ElectricalResistivity, TC: m.ThermalConductivity,
		})
	}

	catalogJSON, _ := json.Marshal(minDB)

	prompt := longContextSystemPrompt + fmt.Sprintf(`

---
Original engineer's problem statement: "%s"

CATALOG (All Available Materials - pick ONLY from here):
%s`, originalQuery, string(catalogJSON))

	// Token limit optimized for 1000 materials context: 2000 tokens for detailed report
	raw, tokens, err := callGemini(ctx, prompt, 0.1, 2000)
	if err != nil {
		return LongContextLLMResponse{}, 0, fmt.Errorf("long-context LLM call: %w", err)
	}

	cleaned := cleanJSON(raw)
	repaired := repairJSON(cleaned)

	var parsed LongContextLLMResponse
	if err := json.Unmarshal([]byte(repaired), &parsed); err != nil {
		log.Printf("WARN: LongContext JSON Parse failed: %v\nRaw: %s\nRepaired: %s", err, raw, repaired)
		return LongContextLLMResponse{Report: "LLM responded with invalid JSON format. See logs."}, tokens, nil
	}

	if parsed.ReportMarkdown != "" {
		parsed.Report = parsed.ReportMarkdown
	} else if parsed.LegacyReport != "" {
		parsed.Report = parsed.LegacyReport
	} else {
		var b strings.Builder
		if parsed.TopCandidate != "" {
			b.WriteString("## 🔬 Engineering Analysis Report\n\n")
			b.WriteString("### 1. Primary Recommendation: ")
			b.WriteString(parsed.TopCandidate)
			b.WriteString("\n")
		}
		if len(parsed.FundamentalStats) > 0 {
			b.WriteString("\n### 2. Fundamental Stats\n")
			for k, v := range parsed.FundamentalStats {
				b.WriteString("- ")
				b.WriteString(k)
				b.WriteString(": ")
				b.WriteString(v)
				b.WriteString("\n")
			}
		}
		if parsed.EngineeringAnalysis != "" {
			b.WriteString("\n### 3. Engineering Analysis\n")
			b.WriteString(parsed.EngineeringAnalysis)
			b.WriteString("\n")
		}
		if parsed.AlternativeRejection != "" {
			b.WriteString("\n### 4. Alternative Rejection\n")
			b.WriteString(parsed.AlternativeRejection)
			b.WriteString("\n")
		}
		if parsed.FeasibilityWarning != "" {
			b.WriteString("\n### 5. Feasibility Warning\n")
			b.WriteString(parsed.FeasibilityWarning)
			b.WriteString("\n")
		}
		parsed.Report = strings.TrimSpace(b.String())
	}

	if parsed.RecommendedIDs == nil {
		parsed.RecommendedIDs = []int{}
	}

	return parsed, tokens, nil
}

// ──────────────────────────────────────────────────────────────────────────
//  3. Alloy Predictor — LLM-enhanced property prediction
// ──────────────────────────────────────────────────────────────────────────

// PredictorLLMInput holds baseline (rule-of-mixtures) results for LLM refinement.
type PredictorLLMInput struct {
	Composition  map[string]float64 `json:"composition_weight_percent"`
	Baseline     map[string]float64 `json:"rule_of_mixtures_baseline"`
	ElementProps []ElementProp      `json:"element_base_properties"`
}

// ElementProp holds element data looked up from DB.
type ElementProp struct {
	Symbol                string   `json:"symbol"`
	WeightPercent         float64  `json:"weight_percent"`
	Density               *float64 `json:"density,omitempty"`
	MeltingPoint          *float64 `json:"melting_point,omitempty"`
	ThermalConductivity   *float64 `json:"thermal_conductivity,omitempty"`
	ElectricalResistivity *float64 `json:"electrical_resistivity,omitempty"`
	YieldStrength         *float64 `json:"yield_strength,omitempty"`
	YoungsModulus         *float64 `json:"youngs_modulus,omitempty"`
}

// PredictorLLMOutput is the structured response from Gemini's predictor prompt.
type PredictorLLMOutput struct {
	RefinedProperties struct {
		Density               *float64 `json:"density"`
		MeltingPoint          *float64 `json:"melting_point"`
		ThermalConductivity   *float64 `json:"thermal_conductivity"`
		ElectricalResistivity *float64 `json:"electrical_resistivity"`
		YieldStrength         *float64 `json:"yield_strength"`
		YoungsModulus         *float64 `json:"youngs_modulus"`
	} `json:"refined_properties"`
	ScientificExplanation string `json:"scientific_explanation"`
	PhaseDiagramNotes     string `json:"phase_diagram_notes"`
	Confidence            string `json:"confidence"` // "High" | "Medium" | "Low"
}

const predictorSystemPrompt = `You are a computational materials scientist specializing in alloy thermodynamics and CALPHAD methods.

You will receive:
1. A custom alloy composition (weight percentages)
2. A rule-of-mixtures baseline (linear weighted average of element properties)
3. Individual element properties from a materials database

Your task is to REFINE the baseline predictions by applying your knowledge of:
- Non-linear mixing effects (Vegard's law deviations)
- Solid-solution strengthening
- Intermetallic compound formation
- Eutectic, peritectic, or other phase transformations
- Microstructural effects on mechanical properties
- Real thermodynamic behaviour (CALPHAD-style reasoning)

Return ONLY a valid JSON object with this exact schema:
{
  "refined_properties": {
    "density": <number_or_null>,
    "melting_point": <number_in_Kelvin_or_null>,
    "thermal_conductivity": <number_W_per_mK_or_null>,
    "electrical_resistivity": <number_in_ohm_m_or_null>,
    "yield_strength": <number_in_MPa_or_null>,
    "youngs_modulus": <number_in_GPa_or_null>
  },
  "scientific_explanation": "<markdown string: explain each deviation from baseline, mention phase diagram behaviour, dominant strengthening mechanism, etc. 4-6 sentences.>",
  "phase_diagram_notes": "<1-2 sentences about the phase diagram at this composition: e.g. single-phase FCC solid solution, two-phase alpha+beta region, near eutectic, etc.>",
  "confidence": "<High|Medium|Low — High if this is a well-studied alloy system, Medium if extrapolating from known data, Low if highly exotic composition>"
}

Do NOT include any text outside the JSON.`

// RefinePrediction sends the baseline + context to Gemini for thermodynamic refinement.
func RefinePrediction(ctx context.Context, input PredictorLLMInput) (PredictorLLMOutput, int, error) {
	inputJSON, _ := json.MarshalIndent(input, "", "  ")
	prompt := predictorSystemPrompt + "\n\nInput:\n" + string(inputJSON)

	raw, tokens, err := callGemini(ctx, prompt, 0.2, 1000)
	if err != nil {
		return PredictorLLMOutput{}, 0, fmt.Errorf("predictor LLM call: %w", err)
	}

	raw = cleanJSON(raw)

	var out PredictorLLMOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return PredictorLLMOutput{}, tokens, fmt.Errorf("failed to parse predictor LLM output: %w\nRaw:\n%s", err, raw)
	}

	return out, tokens, nil
}

// callGoogleAI handles direct calls to Google's Generative Language API
func callGoogleAI(ctx context.Context, apiKey string, model string, prompt string, temperature float64, maxTokens int, apiVer string) (string, int, int, error) {
	baseURL := fmt.Sprintf("https://generativelanguage.googleapis.com/%s/models/%s:generateContent", apiVer, model)
	url := fmt.Sprintf("%s?key=%s", baseURL, apiKey)

	payload := googleAIRequest{
		Contents: []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}{
			{
				Parts: []struct {
					Text string `json:"text"`
				}{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: struct {
			Temperature      float64 `json:"temperature"`
			MaxOutputTokens  int     `json:"maxOutputTokens"`
			ResponseMimeType string  `json:"responseMimeType,omitempty"`
		}{
			Temperature:      temperature,
			MaxOutputTokens:  maxTokens,
			ResponseMimeType: "application/json",
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}

	var result googleAIResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", 0, resp.StatusCode, fmt.Errorf("empty response")
	}

	return result.Candidates[0].Content.Parts[0].Text, result.UsageMetadata.TotalTokenCount, resp.StatusCode, nil
}
