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
		ResponseSchema   any     `json:"responseSchema,omitempty"`
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
				if !isCompleteJSONObject(text) {
					err = fmt.Errorf("model %s returned incomplete/invalid JSON payload", model)
				} else {
					return text, tokens, nil
				}
			}
			lastErr = err
			// Only treat auth failures as fatal; timeout/network issues (status=0)
			// should continue to next model in the fallback chain.
			if status == http.StatusUnauthorized {
				log.Printf("❌ Google AI Fatal Error (%d): %v", status, err)
				break
			}
			log.Printf("⚠️  Google AI %s failed (%d): %v", model, status, err)
			continue
		}
	}

	// Tier 2: OpenRouter Fallback
	if validOR && !strings.HasPrefix(openRouterKey, "AIza") {
		log.Printf("🤖 Attempting OpenRouter Fallback (Key: %s)", maskKey(openRouterKey))
		text, tokens, _, err := callOpenRouter(ctx, openRouterKey, prompt, temperature, maxTokens)
		if err == nil {
			if !isCompleteJSONObject(text) {
				return "", 0, fmt.Errorf("openrouter returned incomplete/invalid JSON payload")
			}
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

func isCompleteJSONObject(raw string) bool {
	cleaned := cleanJSON(raw)
	if cleaned == "" {
		return false
	}
	trimmed := strings.TrimSpace(cleaned)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return false
	}
	return json.Valid([]byte(trimmed))
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "...." + key[len(key)-4:]
}

func getMockResponse(prompt string) (string, int, error) {
	if strings.Contains(prompt, "Virtual Materials Scientist") {
		mockJSON := `{"recommended_ids": [1, 2, 3]Once, "report": "## 🏆 Recommendation\n(Mock Mode: AI is currently resting)"}`
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

	client := &http.Client{Timeout: 60 * time.Second}
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
const intentSystemPrompt = `### ROLE: Principal Materials Systems Architect
### TASK: Universal Engineering Intent Extraction

Analyze the query to build a "Search & Constraint" profile. You must categorize the request and identify the specific physical limits of the user's environment.

### 1. CATEGORY-SPECIFIC PHYSICS (The "North Star"):
- **POLYMERS**: Priority = Glass Transition ($T_g$), HDT, and Chemical Compatibility.
- **METALS**: Priority = Yield Strength ($\sigma_y$), Thermal Conductivity ($k$), and Corrosion Potential.
- **CERAMICS**: Priority = Thermal Shock Resistance ($R$), Hardness, and Weibull Modulus.
- **COMPOSITES**: Priority = Specific Modulus ($E/\rho$), Interlaminar Shear, and Anisotropy.

### 2. THE HARDWARE CEILING (Processing Constraint):
Identify the "Tool limit" to prevent "Absurd Recommendations":
- **3D Printing (FDM)**: Lock nozzle temp < 270°C (Hobby) or 450°C (Industrial).
- **CNC/Machining**: Identify material hardness limits (e.g., "Cannot machine hardened D2 steel with basic end mills").
- **Casting/Foundry**: Identify melting point ($T_m$) vs. crucible/furnace limits.

### OUTPUT SCHEMA (STRICT JSON ONLY):
{
	"inferred_category": "Metal|Polymer|Ceramic|Composite|null",
	"process_lock": "string",
	"hardware_limits": {
		"thermal_ceiling_c": float|null,
		"max_hardness_vickers": float|null
	},
	"search_parameters": {
		"primary_metric": {"field": "string", "min": float|null, "unit": "SI"},
		"environment": ["UV_exposure", "Cryogenic", "Vacuum", "High_Vibration"]
	},
	"merit_index": "e.g., Maximize sigma/rho or E/k"
}

Return JSON only. Do not include markdown code fences or extra text.`

// ExtractIntent parses a natural language query into structured filters.
func ExtractIntent(ctx context.Context, query string) (models.IntentJSON, int, error) {
	type intentLLMResponse struct {
		InferredCategory string `json:"inferred_category"`
		ProcessLock      string `json:"process_lock"`
		HardwareLimits   struct {
			ThermalCeilingC    *float64 `json:"thermal_ceiling_c"`
			MaxHardnessVickers *float64 `json:"max_hardness_vickers"`
		} `json:"hardware_limits"`
		SearchParameters struct {
			PrimaryMetric struct {
				Field string   `json:"field"`
				Min   *float64 `json:"min"`
				Unit  string   `json:"unit"`
			} `json:"primary_metric"`
			Environment []string `json:"environment"`
		} `json:"search_parameters"`
		MeritIndex string `json:"merit_index"`
	}

	prompt := intentSystemPrompt + "\n\nQuery: " + query

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 512)
	if err != nil {
		return models.IntentJSON{}, 0, fmt.Errorf("intent extraction LLM call: %w", err)
	}

	// Extract JSON object only; do not attempt truncation repair.
	cleaned := cleanJSON(raw)

	var llmIntent intentLLMResponse
	if err := json.Unmarshal([]byte(cleaned), &llmIntent); err != nil {
		log.Printf("WARN: Failed to parse intent JSON: %v\nRaw: %s\nCleaned: %s", err, raw, cleaned)
		// Return empty intent — search will fall back to a general query
		return models.IntentJSON{}, tokens, nil
	}

	intent := models.IntentJSON{
		Filters:  map[string]models.RangeFilter{},
		Category: llmIntent.InferredCategory,
	}

	if llmIntent.SearchParameters.PrimaryMetric.Field != "" {
		intent.Filters[llmIntent.SearchParameters.PrimaryMetric.Field] = models.RangeFilter{Min: llmIntent.SearchParameters.PrimaryMetric.Min}
	}

	if llmIntent.HardwareLimits.MaxHardnessVickers != nil {
		intent.Filters["hardness_vickers"] = models.RangeFilter{Max: llmIntent.HardwareLimits.MaxHardnessVickers}
	}

	if strings.Contains(strings.ToLower(llmIntent.ProcessLock), "fdm") || strings.Contains(strings.ToLower(llmIntent.ProcessLock), "hobby") {
		// Do not aggressively remap category; preserve LLM category unless absent.
		if intent.Category == "" || strings.EqualFold(intent.Category, "null") {
			intent.Category = "Polymer"
		}
		intent.Filters["melting_point"] = models.RangeFilter{Max: func() *float64 {
			if llmIntent.HardwareLimits.ThermalCeilingC != nil {
				v := *llmIntent.HardwareLimits.ThermalCeilingC + 273.15
				return &v
			}
			v := 543.15
			return &v
		}()}
	}

	if strings.Contains(strings.ToLower(llmIntent.MeritIndex), "sigma/rho") {
		intent.SortBy = "yield_strength"
		intent.SortDir = "DESC"
	} else if strings.Contains(strings.ToLower(llmIntent.MeritIndex), "e/k") {
		intent.SortBy = "youngs_modulus"
		intent.SortDir = "DESC"
	} else if intent.SortBy == "" {
		intent.SortBy = "density"
		intent.SortDir = "ASC"
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
### PHILOSOPHY: "Properties are secondary to Processability."

Evaluate the retrieved catalog entries. You must act as a mentor, rejecting materials that look good on paper but fail the "Shop Floor" reality check.

### EVALUATION PROTOCOL BY CLASS:

1. **IF POLYMER**:
	- Check Service Temp vs. $T_g$. If $T_{service} > 0.8 \times T_g$, flag for **Viscoelastic Creep**.
	- Check "Printability/Formability." Reject High-Temp resins (Ultem/PEEK) for hobbyist setups.
2. **IF METAL**:
	- Check **Specific Strength** ($\sigma_y/\rho$).
	- Check Machinability/Weldability. If the user is a hobbyist, reject Titanium or Superalloys in favor of 6061-Al or 1018-Steel.
3. **IF CERAMIC**:
	- Check **Thermal Shock Resistance** ($R = \frac{\sigma_f k}{E \alpha}$).
	- Reject if the application requires high toughness (impact) unless it's a toughened ceramic (e.g., ZTA).
4. **IF COMPOSITE**:
	- Check fiber orientation. If the load is multi-axial, warn about **Transverse Failure**.

### OUTPUT FORMAT (STRICT JSON ONLY):
{
  "recommendation": "Material Name",
  "fundamental_physics": {
	 "key_metric_1": "Value + why it wins in this specific physics domain",
	 "key_metric_2": "Value + why it ensures feasibility"
  },
  "selection_narrative": "A first-principles explanation of the choice. Use terms like 'dislocation density,' 'chain entanglement,' or 'stress intensity factor.'",
  "rejection_logic": "Explicitly state why the 'Absurd Choice' (e.g. Metal for a printer) or 'Paper Tiger' (e.g. PLA for heat) was discarded.",
  "manufacturing_advice": "Critical settings for success (e.g., 'Preheat the stock', 'Anneal post-process', 'Use carbide bits')."
}

Return JSON only. Ensure strings are escaped and do not include markdown code fences or extra text outside JSON.`

type LongContextLLMResponse struct {
	Recommendation          string            `json:"recommendation"`
	FundamentalPhysics      map[string]string `json:"fundamental_physics"`
	SelectionNarrative      string            `json:"selection_narrative"`
	RejectionLogic          string            `json:"rejection_logic"`
	ManufacturingAdvice     string            `json:"manufacturing_advice"`
	TechnicalStats          map[string]string `json:"technical_stats"`
	EngineeringRationale    string            `json:"engineering_rationale"`
	ComparativeAnalysis     string            `json:"comparative_analysis"`
	ManufacturingDirectives string            `json:"manufacturing_directives"`
	TopCandidate            string            `json:"top_candidate"`
	FundamentalStats        map[string]string `json:"fundamental_stats"`
	EngineeringAnalysis     string            `json:"engineering_analysis"`
	AlternativeRejection    string            `json:"alternative_rejection"`
	FeasibilityWarning      string            `json:"feasibility_warning"`
	RecommendedIDs          []int             `json:"recommended_ids"`
	ReportMarkdown          string            `json:"report_markdown"`
	LegacyReport            string            `json:"report"`
	Report                  string            `json:"-"`
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
		ID  int      `json:"id"`
		N   string   `json:"name"`
		C   string   `json:"category"`
		D   *float64 `json:"density,omitempty"`
		Tg  *float64 `json:"tg_k,omitempty"`
		HDT *float64 `json:"hdt_k,omitempty"`
		MP  *float64 `json:"melt_pt,omitempty"`
		YS  *float64 `json:"yield_str,omitempty"`
		YM  *float64 `json:"youngs_mod,omitempty"`
		R   *float64 `json:"resistivity,omitempty"`
		TC  *float64 `json:"therm_cond,omitempty"`
	}

	// 1. Filter raw materials by domain to restrict LLM token flood
	allMaterials = FilterByDomain(domain, allMaterials)

	var minDB []MinMat
	for _, m := range allMaterials {
		minDB = append(minDB, MinMat{
			ID: m.ID, N: m.Name, C: m.Category,
			D: m.Density, Tg: m.GlassTransitionTemp, HDT: m.HeatDeflectionTemp,
			MP: m.MeltingPoint, YS: m.YieldStrength,
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

	var parsed LongContextLLMResponse
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		log.Printf("WARN: LongContext JSON Parse failed: %v\nRaw: %s\nCleaned: %s", err, raw, cleaned)
		return LongContextLLMResponse{Report: "LLM responded with invalid JSON format. See logs."}, tokens, nil
	}

	if len(parsed.RecommendedIDs) == 0 {
		candidateName := parsed.Recommendation
		if candidateName == "" {
			candidateName = parsed.TopCandidate
		}
		if candidateName != "" {
			for _, m := range allMaterials {
				if strings.EqualFold(m.Name, candidateName) || strings.Contains(strings.ToLower(m.Name), strings.ToLower(candidateName)) {
					parsed.RecommendedIDs = []int{m.ID}
					break
				}
			}
		}
	}

	if parsed.ReportMarkdown != "" {
		parsed.Report = parsed.ReportMarkdown
	} else if parsed.LegacyReport != "" {
		parsed.Report = parsed.LegacyReport
	} else {
		var b strings.Builder
		candidateName := parsed.Recommendation
		if candidateName == "" {
			candidateName = parsed.TopCandidate
		}
		if candidateName != "" {
			b.WriteString("## 🔬 Engineering Analysis Report\n\n")
			b.WriteString("### 1. Primary Recommendation: ")
			b.WriteString(candidateName)
			b.WriteString("\n")
		}
		stats := parsed.FundamentalPhysics
		if len(stats) == 0 {
			stats = parsed.TechnicalStats
		}
		if len(stats) == 0 {
			stats = parsed.FundamentalStats
		}
		if len(stats) > 0 {
			b.WriteString("\n### 2. Technical Stats\n")
			for k, v := range stats {
				b.WriteString("- ")
				b.WriteString(k)
				b.WriteString(": ")
				b.WriteString(v)
				b.WriteString("\n")
			}
		}
		analysis := parsed.SelectionNarrative
		if analysis == "" {
			analysis = parsed.EngineeringRationale
		}
		if analysis == "" {
			analysis = parsed.EngineeringAnalysis
		}
		if analysis != "" {
			b.WriteString("\n### 3. Engineering Analysis\n")
			b.WriteString(analysis)
			b.WriteString("\n")
		}
		comp := parsed.RejectionLogic
		if comp == "" {
			comp = parsed.ComparativeAnalysis
		}
		if comp == "" {
			comp = parsed.AlternativeRejection
		}
		if comp != "" {
			b.WriteString("\n### 4. Comparative Analysis\n")
			b.WriteString(comp)
			b.WriteString("\n")
		}
		directives := parsed.ManufacturingAdvice
		if directives == "" {
			directives = parsed.ManufacturingDirectives
		}
		if directives == "" {
			directives = parsed.FeasibilityWarning
		}
		if directives != "" {
			b.WriteString("\n### 5. Manufacturing Directives\n")
			b.WriteString(directives)
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
			ResponseSchema   any     `json:"responseSchema,omitempty"`
		}{
			Temperature:      temperature,
			MaxOutputTokens:  maxTokens,
			ResponseMimeType: "application/json",
			ResponseSchema: map[string]any{
				"type": "OBJECT",
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
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

// ──────────────────────────────────────────────────────────────────────────
//  DISPATCHER LOGIC: LLM-Powered Material Category Router
// ──────────────────────────────────────────────────────────────────────────

const routeQuerySystemPrompt = `### ROLE: Material Classification Expert
### TASK: Route user query to specific material category

Analyze the user's query and categorize it into ONE of these buckets:
- Polymers (plastics, resins, rubbers)
- Alloys (aluminum alloys, steel alloys, superalloys)
- Pure_Metals (pure metals like copper, titanium, nickel)
- Ceramics (oxides, nitrides, silicates)
- Composites (fiber-reinforced, laminates)

### CLASSIFICATION RULES:
1. **Polymers**: Keywords: "plastic", "polymer", "3D print", "resin", "rubber", "flexible", "lightweight", "ABS", "PEEK", "PLA", "Nylon"
2. **Alloys**: Keywords: "alloy", "steel", "aluminum", "temper", "grade", "6061", "7075", "304 stainless", "yield strength", "fatigue"
3. **Pure_Metals**: Keywords: "pure metal", "copper", "titanium", "nickel", "tungsten", "pure aluminum", "elemental"
4. **Ceramics**: Keywords: "ceramic", "oxide", "carbide", "nitride", "thermal shock", "hardness", "Al2O3", "SiC", "high temperature"
5. **Composites**: Keywords: "composite", "fiber", "laminate", "carbon fiber", "CFRP", "GFRP", "interlaminar", "anisotropic"

### OUTPUT SCHEMA (STRICT JSON ONLY):
{
  "category": "Polymers|Alloys|Pure_Metals|Ceramics|Composites",
  "confidence": 0.0-1.0,
  "reasoning": "brief explanation of classification"
}

Return JSON only. Do not include markdown code fences or extra text.`

type RouteQueryResponse struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

func normalizeRoutedCategory(category string) string {
	switch strings.TrimSpace(strings.ToLower(category)) {
	case "polymer", "polymers":
		return "Polymers"
	case "alloy", "alloys", "metal", "metals":
		return "Alloys"
	case "pure_metal", "pure_metals", "pure metal", "pure metals":
		return "Pure_Metals"
	case "ceramic", "ceramics":
		return "Ceramics"
	case "composite", "composites":
		return "Composites"
	default:
		return ""
	}
}

// InferCategoryHeuristic provides deterministic routing when LLM routing is unavailable.
func InferCategoryHeuristic(query string) string {
	q := strings.ToLower(query)

	if strings.Contains(q, "composite") || strings.Contains(q, "cfrp") || strings.Contains(q, "gfrp") || strings.Contains(q, "interlaminar") || strings.Contains(q, "fiber") || strings.Contains(q, "laminate") {
		return "Composites"
	}
	if strings.Contains(q, "polymer") || strings.Contains(q, "plastic") || strings.Contains(q, "resin") || strings.Contains(q, "rubber") || strings.Contains(q, "3d print") || strings.Contains(q, "pla") || strings.Contains(q, "peek") || strings.Contains(q, "nylon") {
		return "Polymers"
	}
	if strings.Contains(q, "ceramic") || strings.Contains(q, "carbide") || strings.Contains(q, "nitride") || strings.Contains(q, "oxide") || strings.Contains(q, "thermal shock") || strings.Contains(q, "al2o3") || strings.Contains(q, "sic") {
		return "Ceramics"
	}
	if strings.Contains(q, "pure metal") || strings.Contains(q, "elemental") || strings.Contains(q, "copper") || strings.Contains(q, "tungsten") || strings.Contains(q, "nickel") || strings.Contains(q, "titanium") {
		return "Pure_Metals"
	}
	if strings.Contains(q, "alloy") || strings.Contains(q, "steel") || strings.Contains(q, "stainless") || strings.Contains(q, "6061") || strings.Contains(q, "7075") || strings.Contains(q, "inconel") || strings.Contains(q, "grade") || strings.Contains(q, "temper") {
		return "Alloys"
	}

	return "Alloys"
}

// RouteQuery uses an LLM call to categorize the user's request into one of 5 material buckets.
func RouteQuery(ctx context.Context, query string) (string, int, error) {
	prompt := routeQuerySystemPrompt + "\n\nUser Query: " + query

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 256)
	if err != nil {
		return "", 0, fmt.Errorf("route query LLM call: %w", err)
	}

	cleaned := cleanJSON(raw)

	var route RouteQueryResponse
	if err := json.Unmarshal([]byte(cleaned), &route); err != nil {
		log.Printf("WARN: Route query parsing failed: %v\nRaw: %s", err, raw)
		fallback := InferCategoryHeuristic(query)
		log.Printf("🎯 Heuristic fallback routing used: %s", fallback)
		return fallback, tokens, nil
	}

	normalized := normalizeRoutedCategory(route.Category)
	if normalized == "" {
		normalized = InferCategoryHeuristic(query)
		log.Printf("🎯 Invalid/empty LLM category %q. Heuristic fallback: %s", route.Category, normalized)
	}

	log.Printf("🎯 Query routed to: %s (confidence: %.2f)", normalized, route.Confidence)
	return normalized, tokens, nil
}

// ──────────────────────────────────────────────────────────────────────────
//  SPECIALIZED SEARCH FUNCTIONS: Category-Specific Filtering
// ──────────────────────────────────────────────────────────────────────────

// SearchAlloys searches alloy-specific columns: Yield_Strength, Temper, Fatigue_Limit, Corrosion_Rating
func SearchAlloys(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 10 {
		limit = 3
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include metals that are alloys (have yield strength or specific subcategories)
		if m.Category != "Metal" {
			continue
		}

		// Priority check: Yield strength (core alloy property)
		if minYield, ok := constraints["min_yield_strength"].(float64); ok {
			if m.YieldStrength == nil || *m.YieldStrength < minYield {
				continue
			}
		}

		// Thermal ceiling check (processability)
		if maxMelt, ok := constraints["max_melting_point"].(float64); ok {
			if m.MeltingPoint == nil || *m.MeltingPoint > maxMelt {
				continue
			}
		}

		// Corrosion resistance approximation (electrical resistivity proxy)
		if minResist, ok := constraints["min_corrosion_resistance"].(float64); ok {
			if m.ElectricalResistivity == nil || *m.ElectricalResistivity < minResist {
				continue
			}
		}

		// Fatigue proxy: Young's modulus (higher stiffness = better fatigue resistance)
		if minModulus, ok := constraints["min_youngs_modulus"].(float64); ok {
			if m.YoungsModulus == nil || *m.YoungsModulus < minModulus {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	// Sort by yield strength (descending) for alloys
	if len(filtered) > 1 {
		for i := 0; i < len(filtered)-1; i++ {
			for j := i + 1; j < len(filtered); j++ {
				y1 := 0.0
				if filtered[i].YieldStrength != nil {
					y1 = *filtered[i].YieldStrength
				}
				y2 := 0.0
				if filtered[j].YieldStrength != nil {
					y2 = *filtered[j].YieldStrength
				}
				if y2 > y1 {
					filtered[i], filtered[j] = filtered[j], filtered[i]
				}
			}
		}
	}

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchPolymers searches polymer-specific columns: Glass_Transition_Temp, HDT, Processing_Temp, Crystallinity
func SearchPolymers(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 10 {
		limit = 3
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include polymers
		if m.Category != "Polymer" {
			continue
		}

		// Glass transition temperature (primary polymer property)
		if minTg, ok := constraints["min_glass_transition_temp"].(float64); ok {
			if m.GlassTransitionTemp == nil || *m.GlassTransitionTemp < minTg {
				continue
			}
		}
		if maxTg, ok := constraints["max_glass_transition_temp"].(float64); ok {
			if m.GlassTransitionTemp == nil || *m.GlassTransitionTemp > maxTg {
				continue
			}
		}

		// Heat deflection temperature (processability/service temp)
		if minHDT, ok := constraints["min_hdt"].(float64); ok {
			if m.HeatDeflectionTemp == nil || *m.HeatDeflectionTemp < minHDT {
				continue
			}
		}

		// Processing temperature ceiling (manufacturability)
		if maxProcTemp, ok := constraints["max_processing_temp"].(float64); ok {
			if m.ProcessingTempMaxC == nil || *m.ProcessingTempMaxC > maxProcTemp {
				continue
			}
		}

		// Crystallinity check (affects stiffness and thermal properties)
		if minCryst, ok := constraints["min_crystallinity"].(float64); ok {
			if m.Crystallinity == nil || *m.Crystallinity < minCryst {
				continue
			}
		}

		// Density check (lightweight requirement)
		if maxDensity, ok := constraints["max_density"].(float64); ok {
			if m.Density == nil || *m.Density > maxDensity {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	// Sort by glass transition temp (descending) for polymers
	if len(filtered) > 1 {
		for i := 0; i < len(filtered)-1; i++ {
			for j := i + 1; j < len(filtered); j++ {
				tg1 := 0.0
				if filtered[i].GlassTransitionTemp != nil {
					tg1 = *filtered[i].GlassTransitionTemp
				}
				tg2 := 0.0
				if filtered[j].GlassTransitionTemp != nil {
					tg2 = *filtered[j].GlassTransitionTemp
				}
				if tg2 > tg1 {
					filtered[i], filtered[j] = filtered[j], filtered[i]
				}
			}
		}
	}

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchCeramics searches ceramic-specific columns: Hardness_Vickers, Thermal_Shock_Resistance, Fracture_Toughness
func SearchCeramics(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 10 {
		limit = 3
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include ceramics
		if m.Category != "Ceramic" {
			continue
		}

		// Hardness check (primary ceramic property)
		if minHardness, ok := constraints["min_hardness_vickers"].(float64); ok {
			if m.HardnessVickers == nil || *m.HardnessVickers < minHardness {
				continue
			}
		}

		// Fracture toughness (thermal shock resistance proxy)
		if minToughness, ok := constraints["min_fracture_toughness"].(float64); ok {
			if m.FractureToughness == nil || *m.FractureToughness < minToughness {
				continue
			}
		}

		// Melting point (high-temp capability)
		if minMeltPt, ok := constraints["min_melting_point"].(float64); ok {
			if m.MeltingPoint == nil || *m.MeltingPoint < minMeltPt {
				continue
			}
		}

		// Thermal conductivity (heat dissipation)
		if minTC, ok := constraints["min_thermal_conductivity"].(float64); ok {
			if m.ThermalConductivity == nil || *m.ThermalConductivity < minTC {
				continue
			}
		}

		// Young's modulus (stiffness)
		if minModulus, ok := constraints["min_youngs_modulus"].(float64); ok {
			if m.YoungsModulus == nil || *m.YoungsModulus < minModulus {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	// Sort by hardness (descending) for ceramics
	if len(filtered) > 1 {
		for i := 0; i < len(filtered)-1; i++ {
			for j := i + 1; j < len(filtered); j++ {
				h1 := 0.0
				if filtered[i].HardnessVickers != nil {
					h1 = *filtered[i].HardnessVickers
				}
				h2 := 0.0
				if filtered[j].HardnessVickers != nil {
					h2 = *filtered[j].HardnessVickers
				}
				if h2 > h1 {
					filtered[i], filtered[j] = filtered[j], filtered[i]
				}
			}
		}
	}

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchComposites searches composite-specific columns: Interlaminar_Shear_Strength, Fiber_Volume_Fraction, Anisotropy
func SearchComposites(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 10 {
		limit = 3
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include composites
		if m.Category != "Composite" {
			continue
		}

		// Interlaminar shear strength (critical for composite integrity)
		if minILSS, ok := constraints["min_ilss"].(float64); ok {
			if m.InterlaminarShear == nil || *m.InterlaminarShear < minILSS {
				continue
			}
		}

		// Fiber volume fraction (composite quality indicator)
		if minFibreFrac, ok := constraints["min_fiber_volume_fraction"].(float64); ok {
			if m.FiberVolumeFraction == nil || *m.FiberVolumeFraction < minFibreFrac {
				continue
			}
		}

		// Young's modulus (stiffness)
		if minModulus, ok := constraints["min_youngs_modulus"].(float64); ok {
			if m.YoungsModulus == nil || *m.YoungsModulus < minModulus {
				continue
			}
		}

		// Density (weight constraint)
		if maxDensity, ok := constraints["max_density"].(float64); ok {
			if m.Density == nil || *m.Density > maxDensity {
				continue
			}
		}

		// Thermal conductivity (performance requirement)
		if minTC, ok := constraints["min_thermal_conductivity"].(float64); ok {
			if m.ThermalConductivity == nil || *m.ThermalConductivity < minTC {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	// Sort by interlaminar shear strength (descending) for composites
	if len(filtered) > 1 {
		for i := 0; i < len(filtered)-1; i++ {
			for j := i + 1; j < len(filtered); j++ {
				ilss1 := 0.0
				if filtered[i].InterlaminarShear != nil {
					ilss1 = *filtered[i].InterlaminarShear
				}
				ilss2 := 0.0
				if filtered[j].InterlaminarShear != nil {
					ilss2 = *filtered[j].InterlaminarShear
				}
				if ilss2 > ilss1 {
					filtered[i], filtered[j] = filtered[j], filtered[i]
				}
			}
		}
	}

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchPureMetals searches pure metals with elemental purity focus
func SearchPureMetals(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 10 {
		limit = 3
	}

	var filtered []models.Material

	for _, m := range materials {
		// Include metals that are pure or have high purity subcategories
		if m.Category != "Metal" {
			continue
		}

		// Skip alloys (only want pure metals)
		if m.Subcategory != nil && *m.Subcategory == "Ferrous" {
			continue // Exclude steel alloys
		}

		// Electrical conductivity check (indicator of purity)
		if maxResist, ok := constraints["max_electrical_resistivity"].(float64); ok {
			if m.ElectricalResistivity == nil || *m.ElectricalResistivity > maxResist {
				continue
			}
		}

		// Melting point check
		if minMelt, ok := constraints["min_melting_point"].(float64); ok {
			if m.MeltingPoint == nil || *m.MeltingPoint < minMelt {
				continue
			}
		}

		// Thermal conductivity (pure metals usually have higher TC)
		if minTC, ok := constraints["min_thermal_conductivity"].(float64); ok {
			if m.ThermalConductivity == nil || *m.ThermalConductivity < minTC {
				continue
			}
		}

		// Density range check
		if minDensity, ok := constraints["min_density"].(float64); ok {
			if m.Density == nil || *m.Density < minDensity {
				continue
			}
		}
		if maxDensity, ok := constraints["max_density"].(float64); ok {
			if m.Density == nil || *m.Density > maxDensity {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	// Sort by thermal conductivity (descending) for pure metals
	if len(filtered) > 1 {
		for i := 0; i < len(filtered)-1; i++ {
			for j := i + 1; j < len(filtered); j++ {
				tc1 := 0.0
				if filtered[i].ThermalConductivity != nil {
					tc1 = *filtered[i].ThermalConductivity
				}
				tc2 := 0.0
				if filtered[j].ThermalConductivity != nil {
					tc2 = *filtered[j].ThermalConductivity
				}
				if tc2 > tc1 {
					filtered[i], filtered[j] = filtered[j], filtered[i]
				}
			}
		}
	}

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// ──────────────────────────────────────────────────────────────────────────
//  SCIENTIFIC ANALYSIS: First-Principles Physics Verification
// ──────────────────────────────────────────────────────────────────────────

const scientificAnalysisSystemPrompt = `### ROLE: Principal Physicist & Materials Engineer
### TASK: Apply first-principles physics verification to candidate materials

For the given user requirement and top 3 candidate materials, perform rigorous engineering checks:

### PHYSICS VERIFICATION PROTOCOL:

**For POLYMERS:**
- Check: Service Temp < 0.8 × Tg (avoid viscoelastic creep)
- Check: Processing Temp < material's HDT (manufacturability)
- Check: If UV-exposed, reject polymers without stabilizers
- Metric to maximize: Tg - Service_Temp (thermal headroom)

**For METALS/ALLOYS:**
- Check: Specific Strength (σ_y/ρ) vs application demand
- Check: Yield Strength margin (apply 1.5× safety factor for dynamic loads)
- Check: Fatigue limit ≈ 0.3-0.6 × Ultimate Tensile Strength
- Metric to maximize: σ_y/ρ (strength-to-weight ratio)

**For CERAMICS:**
- Check: Thermal Shock Resistance (R = σ_f × k / E / α)
- Check: Fracture Toughness ≥ 3 MPa√m for impact resistance
- Check: Weibull Modulus ≥ 10 for reliability
- Metric to maximize: R (thermal shock resistance)

**For COMPOSITES:**
- Check: Fiber orientation (0°/90°/±45°) matches load direction
- Check: ILSS (Interlaminar Shear Strength) ≥ 50 MPa minimum
- Check: Fiber volume fraction ≥ 50% indicates quality
- Metric to maximize: E/ρ (specific modulus)

### OUTPUT SCHEMA (STRICT JSON ONLY):
{
  "top_candidate": "Material Name",
  "physics_verification": {
    "check_1_name": "PASS|FAIL",
    "check_1_value": "computed or measured value with unit",
    "check_1_physics": "First principles explanation"
  },
  "merit_index_calculation": "Formula and result for the key metric",
  "failure_rejection_reasons": ["material_1: reason 1", "material_2: reason 2", ...],
  "manufacturing_feasibility": "Step-by-step manufacturing instructions",
  "safety_margin": "Computed safety factor and assessment"
}

Return JSON only. Ensure strings are escaped and do not include markdown code fences.`

type PhysicsVerification struct {
	CheckName string `json:"check_name"`
	Status    string `json:"status"` // PASS or FAIL
	Value     string `json:"value"`
	Physics   string `json:"physics"`
}

type ScientificAnalysisResponse struct {
	TopCandidate             string            `json:"top_candidate"`
	PhysicsVerification      map[string]string `json:"physics_verification"`
	MeritIndexCalculation    string            `json:"merit_index_calculation"`
	FailureRejectionReasons  []string          `json:"failure_rejection_reasons"`
	ManufacturingFeasibility string            `json:"manufacturing_feasibility"`
	SafetyMargin             string            `json:"safety_margin"`
}

// ScientificAnalysis applies first-principles physics checks to the top 3 candidates
func ScientificAnalysis(ctx context.Context, query string, category string, topCandidates []models.Material) (ScientificAnalysisResponse, int, error) {
	if len(topCandidates) == 0 {
		return ScientificAnalysisResponse{}, 0, fmt.Errorf("no candidates provided for analysis")
	}

	// Build compact material representations for the LLM
	type AnalysisMat struct {
		Name      string   `json:"name"`
		Category  string   `json:"category"`
		Density   *float64 `json:"density_kg_m3,omitempty"`
		Tg        *float64 `json:"tg_kelvin,omitempty"`
		HDT       *float64 `json:"hdt_kelvin,omitempty"`
		YieldStr  *float64 `json:"yield_strength_pa,omitempty"`
		YoungsMod *float64 `json:"youngs_modulus_pa,omitempty"`
		ThermalC  *float64 `json:"thermal_conductivity_w_mk,omitempty"`
		Hardness  *float64 `json:"hardness_vickers,omitempty"`
		Toughness *float64 `json:"fracture_toughness_mpa_m,omitempty"`
		ILSS      *float64 `json:"ilss_mpa,omitempty"`
		FibreFrac *float64 `json:"fiber_volume_fraction,omitempty"`
	}

	var analysisMats []AnalysisMat
	for _, m := range topCandidates {
		analysisMats = append(analysisMats, AnalysisMat{
			Name:      m.Name,
			Category:  m.Category,
			Density:   m.Density,
			Tg:        m.GlassTransitionTemp,
			HDT:       m.HeatDeflectionTemp,
			YieldStr:  m.YieldStrength,
			YoungsMod: m.YoungsModulus,
			ThermalC:  m.ThermalConductivity,
			Hardness:  m.HardnessVickers,
			Toughness: m.FractureToughness,
			ILSS:      m.InterlaminarShear,
			FibreFrac: m.FiberVolumeFraction,
		})
	}

	catalogJSON, _ := json.Marshal(analysisMats)

	prompt := scientificAnalysisSystemPrompt + fmt.Sprintf(`

Material Category: %s
User Requirement Query: "%s"

Three Candidate Materials:
%s

Please analyze these candidates using first-principles physics and provide comprehensive verification report.`, category, query, string(catalogJSON))

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 1500)
	if err != nil {
		return ScientificAnalysisResponse{}, 0, fmt.Errorf("scientific analysis LLM call: %w", err)
	}

	cleaned := cleanJSON(raw)

	var analysis ScientificAnalysisResponse
	if err := json.Unmarshal([]byte(cleaned), &analysis); err != nil {
		log.Printf("WARN: Scientific analysis JSON parse failed: %v\nRaw: %s", err, raw)
		// Return partial response with top candidate name
		analysis.TopCandidate = topCandidates[0].Name
		analysis.PhysicsVerification = make(map[string]string)
		return analysis, tokens, nil
	}

	return analysis, tokens, nil
}
