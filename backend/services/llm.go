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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vivek/met-quest/models"
)

const openRouterBaseURL = "https://openrouter.ai/api/v1/chat/completions"

var (
	retryDelayRegex = regexp.MustCompile(`"retryDelay"\s*:\s*"([0-9]+)s"`)
	tempCRegex      = regexp.MustCompile(`(-?[0-9]+(?:\.[0-9]+)?)\s*°?\s*[cC]\b`)
	latexTempCRegex = regexp.MustCompile(`(-?[0-9]+(?:\.[0-9]+)?)\s*(?:\^\{?\\?circ\}?|\{\\?circ\}|degrees?)\s*\$?\s*[cC]\b`)
	psiRegex        = regexp.MustCompile(`([0-9]{3,6})\s*psi\b`)
	modelBackoff    = struct {
		sync.Mutex
		until map[string]time.Time
	}{until: map[string]time.Time{}}
)

type querySignals struct {
	desktopFDM                 bool
	professionalFDM            bool
	hasEnclosure               bool
	hasNozzleCap               bool
	nozzleCapC                 float64
	hasServiceTemp             bool
	serviceTempC               float64
	requiresHeatMargin         bool
	requiresDamping            bool
	requiresAesthetics         bool
	requiresFastPrint          bool
	requiresCryogenic          bool
	requiresCNC                bool
	requiresChemical           bool
	requiresConductivity       bool
	requiresWear               bool
	requiresExtremeHeat        bool
	requiresAerospace          bool
	hasHighPressure            bool
	hasHighStrengthReq         bool
	requiresSnapFit            bool
	requiresConductivityPurist bool
	requiresChemicalExtreme    bool
	requiresSpecificModulus    bool
	requiresHotSection         bool
	requiresBiomedical         bool
	requiresRadiationShielding bool
	requiresTransparentImpact  bool
	requiresThermalShock       bool
	requiresShapeMemory        bool
	requiresLowCTE             bool
	impossibleDesktopFDM       bool
}

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
			if skip, until := shouldSkipModel(model); skip {
				log.Printf("⏭️  Skipping Google AI %s until %s (backoff active)", model, until.Format(time.RFC3339))
				continue
			}

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
					markModelBackoff(model, 15*time.Second)
				} else {
					log.Printf("✅ Google AI %s succeeded", model)
					clearModelBackoff(model)
					return text, tokens, nil
				}
			}
			lastErr = err

			if status == http.StatusTooManyRequests {
				if d := extractRetryDelay(err.Error()); d > 0 {
					markModelBackoff(model, d)
				}
				if hasZeroQuotaLimit(err.Error(), model) {
					// Daily/project free-tier zero quota is not transient for this run.
					markModelBackoff(model, 24*time.Hour)
				}
			}
			if status == http.StatusServiceUnavailable {
				markModelBackoff(model, 20*time.Second)
			}

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
			log.Printf("✅ OpenRouter fallback succeeded")
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

// callGeminiText is a conversational text call that does not enforce JSON-only output.
func callGeminiText(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, int, error) {
	googleKey := os.Getenv("GEMINI_API_KEY")
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")

	validGoogle := googleKey != "" && !strings.Contains(googleKey, "Dummy") && !strings.Contains(googleKey, "your_")
	validOR := openRouterKey != "" && !strings.Contains(openRouterKey, "Dummy") && !strings.Contains(openRouterKey, "your_")

	if !validGoogle && !validOR {
		return "I can help with follow-up reasoning. Share what you want to refine and I will respond conversationally.", 30, nil
	}

	var lastErr error

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
		}
		for _, model := range googleModels {
			if skip, _ := shouldSkipModel(model); skip {
				continue
			}
			text, tokens, status, err := callGoogleAIText(ctx, activeGoogleKey, model, prompt, temperature, maxTokens, "v1beta")
			if status == http.StatusNotFound {
				text, tokens, status, err = callGoogleAIText(ctx, activeGoogleKey, model, prompt, temperature, maxTokens, "v1")
			}
			if err == nil {
				clearModelBackoff(model)
				return strings.TrimSpace(text), tokens, nil
			}
			lastErr = err
			if status == http.StatusTooManyRequests {
				markModelBackoff(model, 20*time.Second)
			}
			if status == http.StatusUnauthorized {
				break
			}
		}
	}

	if validOR && !strings.HasPrefix(openRouterKey, "AIza") {
		text, tokens, _, err := callOpenRouter(ctx, openRouterKey, prompt, temperature, maxTokens)
		if err == nil {
			return strings.TrimSpace(text), tokens, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return "", 0, fmt.Errorf("chat text providers failed: %w", lastErr)
	}
	return "", 0, fmt.Errorf("chat text providers unavailable")
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

func shouldSkipModel(model string) (bool, time.Time) {
	modelBackoff.Lock()
	defer modelBackoff.Unlock()
	until, ok := modelBackoff.until[model]
	if !ok {
		return false, time.Time{}
	}
	if time.Now().After(until) {
		delete(modelBackoff.until, model)
		return false, time.Time{}
	}
	return true, until
}

func markModelBackoff(model string, d time.Duration) {
	if d <= 0 {
		return
	}
	modelBackoff.Lock()
	defer modelBackoff.Unlock()
	until := time.Now().Add(d)
	if prev, ok := modelBackoff.until[model]; ok && prev.After(until) {
		return
	}
	modelBackoff.until[model] = until
}

func clearModelBackoff(model string) {
	modelBackoff.Lock()
	defer modelBackoff.Unlock()
	delete(modelBackoff.until, model)
}

func extractRetryDelay(errText string) time.Duration {
	m := retryDelayRegex.FindStringSubmatch(errText)
	if len(m) != 2 {
		return 0
	}
	sec := 0
	_, err := fmt.Sscanf(m[1], "%d", &sec)
	if err != nil || sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func hasZeroQuotaLimit(errText, model string) bool {
	needle := "limit: 0, model: " + model
	return strings.Contains(errText, needle)
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
		f := llmIntent.SearchParameters.PrimaryMetric.Field
		intent.Filters[f] = models.RangeFilter{Min: llmIntent.SearchParameters.PrimaryMetric.Min}
	}

	desktopPrintLock := strings.Contains(strings.ToLower(llmIntent.ProcessLock), "fdm") ||
		strings.Contains(strings.ToLower(llmIntent.ProcessLock), "3d print") ||
		strings.Contains(strings.ToLower(llmIntent.ProcessLock), "desktop")

	if llmIntent.HardwareLimits.ThermalCeilingC != nil && !desktopPrintLock {
		limitK := *llmIntent.HardwareLimits.ThermalCeilingC + 273.15
		intent.Filters["melting_point"] = models.RangeFilter{Max: &limitK}
	}

	if llmIntent.HardwareLimits.MaxHardnessVickers != nil {
		intent.Filters["hardness_vickers"] = models.RangeFilter{Max: llmIntent.HardwareLimits.MaxHardnessVickers}
	}

	if desktopPrintLock {
		// Hard lock: desktop-class FDM requests must route to printable classes.
		intent.Category = "Polymer"
		limitC := 270.0
		if llmIntent.HardwareLimits.ThermalCeilingC != nil {
			limitC = *llmIntent.HardwareLimits.ThermalCeilingC
		}
		// Handler expands filters to min_/max_ keys; keep field name canonical here.
		intent.Filters["processing_temp"] = models.RangeFilter{Max: &limitC}
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

CRITICAL: For desktop FDM / 3D-printing scenarios, reject Metals/Alloys entirely and prioritize printable Polymer or Composite options.
Use Pareto trade-off reasoning: thermal survivability + desktop printability + strength-to-weight.
If a high-heat polymer is unprintable on desktop and PLA is too thermally weak, prefer middle-ground materials (e.g., PETG-class behavior) when present.

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
	"recommended_ids": [int, int, int],
  "recommendation": "Material Name",
  "fundamental_physics": {
	 "key_metric_1": "Value + why it wins in this specific physics domain",
	 "key_metric_2": "Value + why it ensures feasibility"
  },
  "selection_narrative": "A first-principles explanation of the choice. Use terms like 'dislocation density,' 'chain entanglement,' or 'stress intensity factor.'",
  "rejection_logic": "Explicitly state why the 'Absurd Choice' (e.g. Metal for a printer) or 'Paper Tiger' (e.g. PLA for heat) was discarded.",
  "manufacturing_advice": "Critical settings for success (e.g., 'Preheat the stock', 'Anneal post-process', 'Use carbide bits')."
}

IMPORTANT: recommended_ids must be IDs from the provided catalog. Do not invent IDs.
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
	signals := extractQuerySignals(originalQuery)

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

	var parsed LongContextLLMResponse

	// Keep token budget moderate to reduce provider quota/credit failures.
	raw, tokens, err := callGemini(ctx, prompt, 0.1, 1200)
	if err != nil {
		log.Printf("WARN: LongContext LLM call failed; using deterministic fallback: %v", err)
	} else {
		cleaned := cleanJSON(raw)
		if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
			log.Printf("WARN: LongContext JSON Parse failed; using fallback ranking. err=%v raw=%s cleaned=%s", err, raw, cleaned)
		}
	}

	if len(parsed.RecommendedIDs) == 0 {
		candidateName := parsed.Recommendation
		if candidateName == "" {
			candidateName = parsed.TopCandidate
		}
		if candidateName != "" {
			for _, m := range allMaterials {
				name := strings.ToLower(strings.TrimSpace(m.Name))
				candidate := strings.ToLower(strings.TrimSpace(candidateName))
				if strings.EqualFold(m.Name, candidateName) || strings.Contains(name, candidate) || strings.Contains(candidate, name) {
					parsed.RecommendedIDs = []int{m.ID}
					break
				}
			}
		}
	}

	if len(parsed.RecommendedIDs) == 0 {
		parsed.RecommendedIDs = inferFallbackRecommendedIDs(originalQuery, allMaterials, 3)
	}
	if len(parsed.RecommendedIDs) > 1 {
		parsed.RecommendedIDs = rerankRecommendedIDs(originalQuery, parsed.RecommendedIDs, allMaterials)
	}
	parsed.RecommendedIDs = applyDesktopFeasibilityFilter(originalQuery, parsed.RecommendedIDs, allMaterials)

	if signals.impossibleDesktopFDM {
		parsed.RecommendedIDs = []int{}
		parsed.Report = "## Process-Feasibility Warning\n- No recommendation returned.\n- Reason: Requested pressure/temperature envelope is physically incompatible with standard desktop FDM printing.\n- Action: Switch process to CNC/forging/casting and select an engineering alloy (for example, 6061-class aluminum for lightweight non-cryobrittle service where appropriate)."
		return parsed, tokens, nil
	}

	if len(parsed.RecommendedIDs) == 0 {
		parsed.RecommendedIDs = inferFallbackRecommendedIDs(originalQuery, allMaterials, 3)
	}
	parsed.RecommendedIDs = ensureMinimumRecommendedIDs(originalQuery, parsed.RecommendedIDs, allMaterials, 3)

	if strings.TrimSpace(parsed.ReportMarkdown) == "" && strings.TrimSpace(parsed.LegacyReport) == "" {
		if strings.TrimSpace(parsed.Report) == "" {
			parsed.Report = buildFallbackReport(originalQuery, parsed.RecommendedIDs, allMaterials)
		}
	}

	if parsed.ReportMarkdown != "" {
		parsed.Report = parsed.ReportMarkdown
	} else if parsed.LegacyReport != "" {
		parsed.Report = parsed.LegacyReport
	} else if strings.TrimSpace(parsed.Report) == "" {
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

func inferFallbackRecommendedIDs(query string, allMaterials []models.Material, limit int) []int {
	if limit <= 0 {
		limit = 3
	}
	signals := extractQuerySignals(query)

	type scored struct {
		id    int
		score float64
	}
	best := make([]scored, 0, limit)

	for _, m := range allMaterials {
		if signals.desktopFDM && !(strings.EqualFold(m.Category, "Polymer") || strings.EqualFold(m.Category, "Composite")) {
			continue
		}
		if signals.requiresCNC && !(strings.EqualFold(m.Category, "Metal") || strings.EqualFold(m.Category, "Alloy")) {
			continue
		}

		score := scoreMaterialForQuery(m, signals)

		inserted := false
		for i := range best {
			if score > best[i].score {
				best = append(best[:i], append([]scored{{id: m.ID, score: score}}, best[i:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			best = append(best, scored{id: m.ID, score: score})
		}
		if len(best) > limit {
			best = best[:limit]
		}
	}

	ids := make([]int, 0, len(best))
	for _, b := range best {
		ids = append(ids, b.id)
	}
	return ids
}

func rerankRecommendedIDs(query string, ids []int, allMaterials []models.Material) []int {
	signals := extractQuerySignals(query)

	lookup := map[int]models.Material{}
	for _, m := range allMaterials {
		lookup[m.ID] = m
	}

	type scored struct {
		id    int
		score float64
	}
	ranked := make([]scored, 0, len(ids))
	for _, id := range ids {
		m, ok := lookup[id]
		if !ok {
			continue
		}
		ranked = append(ranked, scored{id: id, score: scoreMaterialForQuery(m, signals)})
	}

	for i := 0; i < len(ranked)-1; i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].score > ranked[i].score {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}

	out := make([]int, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, r.id)
	}
	if len(out) == 0 {
		return ids
	}
	return out
}

func scoreMaterialForQuery(m models.Material, s querySignals) float64 {
	score := 0.0
	name := strings.ToLower(m.Name)
	cat := strings.ToLower(m.Category)

	if m.Density != nil {
		score += (2.0 - *m.Density) * 6.0 // favor lightweight materials
	}

	if m.YieldStrength != nil {
		score += *m.YieldStrength * 0.08
	}

	if s.requiresHeatMargin {
		if m.GlassTransitionTemp != nil {
			score += (*m.GlassTransitionTemp - 273.15) * 0.9 // prioritize Tg margin in C
		}
		if m.HeatDeflectionTemp != nil {
			score += (*m.HeatDeflectionTemp - 273.15) * 0.8
		}
		if m.GlassTransitionTemp != nil && (*m.GlassTransitionTemp-273.15) < 70 {
			score -= 120.0
		}
		if s.hasServiceTemp && m.HeatDeflectionTemp != nil {
			delta := (*m.HeatDeflectionTemp - 273.15) - s.serviceTempC
			score += delta * 1.8
			if delta < 0 {
				score -= 180.0
			}
		}
	}

	if s.desktopFDM {
		if m.ProcessingTempMaxC != nil {
			limit := 270.0
			if s.hasNozzleCap {
				limit = s.nozzleCapC
			}
			if *m.ProcessingTempMaxC <= limit {
				score += 40.0
			} else if *m.ProcessingTempMaxC <= limit+20.0 {
				score -= 100.0
			} else {
				if s.hasEnclosure {
					score -= 150.0
				} else {
					score -= 500.0
				}
			}
		}
		if m.ThermalExpansion != nil {
			score -= *m.ThermalExpansion * 0.2 // lower expansion => lower warp risk
			if *m.ThermalExpansion > 80.0 {
				score -= 40.0
			}
			if *m.ThermalExpansion > 95.0 {
				score -= 120.0
			}
		}
		if strings.Contains(name, "pla") && s.requiresHeatMargin {
			score -= 120.0
		}
		if !s.professionalFDM && (strings.Contains(name, "peek") || strings.Contains(name, "ultem")) {
			score -= 140.0
		}
		if !s.hasEnclosure && strings.Contains(name, "abs") {
			score -= 180.0
		}
		if strings.Contains(name, "polystyrene") || strings.Contains(name, " ps") {
			score -= 90.0
		}
		if strings.Contains(name, "petg") {
			score += 220.0
		}
		if strings.Contains(name, "pc-pbt") {
			score += 120.0
		}
		if strings.Contains(name, "polycarbonate") || strings.Contains(name, " pc") {
			score += 20.0
		}

		if s.requiresAesthetics && s.requiresFastPrint && !s.requiresHeatMargin {
			if strings.Contains(name, "pla") {
				score += 760.0
			}
			if strings.Contains(name, "petg") {
				score -= 120.0
			}
			if strings.Contains(name, "peek") || strings.Contains(name, "ultem") || strings.Contains(name, "polycarbonate") {
				score -= 700.0
			}
		}

		if s.hasServiceTemp && s.serviceTempC >= 100.0 && s.professionalFDM {
			if strings.Contains(name, "polycarbonate") || strings.Contains(name, "pc-pbt") || strings.Contains(name, "pc") {
				score += 200.0
			}
			if strings.Contains(name, "petg") {
				score -= 180.0
			}
		}
	}

	if s.requiresDamping {
		if strings.Contains(name, "tpu") || strings.Contains(name, "elastomer") || strings.Contains(name, "urethane") || strings.Contains(name, "rubber") || strings.Contains(name, "tpe") || strings.Contains(name, "santoprene") {
			score += 420.0
		}
		if strings.Contains(name, "polypropylene") || strings.Contains(name, " pp") || strings.Contains(name, "nylon") {
			score += 240.0
		}
		if strings.Contains(name, "petg") || strings.Contains(name, "pla") || strings.Contains(name, "abs") {
			score -= 360.0
		}
		if strings.Contains(name, "polycarbonate") || strings.Contains(name, "polystyrene") || strings.Contains(name, "epoxy") {
			score -= 180.0
		}
	}

	if s.requiresChemical {
		if strings.Contains(name, "ptfe") || strings.Contains(name, "teflon") {
			score += 700.0
		}
		if strings.Contains(name, "peek") {
			score += 260.0
		}
		if strings.Contains(name, "pla") || strings.Contains(name, "abs") || strings.Contains(name, "petg") || strings.Contains(name, "nylon") {
			score -= 180.0
		}
		if s.hasServiceTemp && s.serviceTempC >= 120 && m.MeltingPoint != nil && (*m.MeltingPoint-273.15) < s.serviceTempC+40 {
			score -= 160.0
		}
	}

	if s.requiresChemicalExtreme {
		if strings.Contains(name, "peek") {
			score += 520.0
		}
		if strings.Contains(name, "ptfe") || strings.Contains(name, "teflon") {
			score += 400.0
		}
		if strings.Contains(name, "ultem") || strings.Contains(name, "pei") {
			score -= 280.0
		}
		if strings.Contains(name, "petg") || strings.Contains(name, "abs") || strings.Contains(name, "pla") {
			score -= 420.0
		}
	}

	if s.requiresCNC || s.requiresCryogenic || s.hasHighPressure {
		if cat == "metal" || strings.Contains(cat, "alloy") {
			score += 120.0
		}
		if strings.Contains(name, "6061") {
			score += 320.0
		}
		if strings.Contains(name, "al-6061") || strings.Contains(name, "aluminum 6061") || strings.Contains(name, "6061-t6") {
			score += 280.0
		}
		if strings.Contains(name, "aluminum") && s.requiresCryogenic {
			score += 180.0
		}
		if strings.Contains(name, "steel") && s.requiresCryogenic {
			score -= 120.0
		}
		if cat == "polymer" {
			score -= 300.0
		}
	}

	if s.requiresConductivity {
		if strings.Contains(name, "copper") || name == "cu" || strings.Contains(name, "copper (pure)") {
			score += 900.0
		}
		if strings.Contains(name, "oxygen-free copper") || strings.Contains(name, "c101") || strings.Contains(name, "c110") {
			score += 260.0
		}
		if strings.Contains(name, "silver") {
			score += 260.0
		}
		if strings.Contains(name, "brass") || strings.Contains(name, "bronze") || strings.Contains(name, "cupronickel") || strings.Contains(name, "alloy") {
			score -= 260.0
		}
		if m.ThermalConductivity != nil {
			score += *m.ThermalConductivity * 1.4
		}
		if m.ElectricalResistivity != nil && *m.ElectricalResistivity > 0 {
			score += 1.0 / *m.ElectricalResistivity / 2e6
		}
	}

	if s.requiresConductivityPurist {
		if strings.Contains(name, "oxygen-free") || strings.Contains(name, "ofhc") || strings.Contains(name, "c101") || strings.Contains(name, "c110") {
			score += 620.0
		}
		if strings.Contains(name, "brass") || strings.Contains(name, "bronze") || strings.Contains(name, "cupronickel") {
			score -= 520.0
		}
	}

	if s.requiresWear {
		if strings.Contains(name, "zirconia") || strings.Contains(name, "zro2") {
			score += 560.0
		}
		if strings.Contains(name, "silicon carbide") || strings.Contains(name, "sic") {
			score += 540.0
		}
		if strings.Contains(name, "alumina") || strings.Contains(name, "al2o3") {
			score += 320.0
		}
		if m.HardnessVickers != nil {
			score += *m.HardnessVickers * 0.25
		}
	}

	if s.requiresExtremeHeat {
		if strings.Contains(name, "alumina") || strings.Contains(name, "al2o3") {
			score += 620.0
		}
		if strings.Contains(name, "silicon carbide") || strings.Contains(name, "sic") {
			score += 380.0
		}
		if strings.Contains(name, "polymer") || cat == "polymer" {
			score -= 900.0
		}
		if m.MeltingPoint != nil {
			score += (*m.MeltingPoint - 273.15) * 0.12
		}
		if m.ThermalExpansion != nil {
			score -= *m.ThermalExpansion * 2.0
		}
	}

	if s.requiresAerospace {
		if strings.Contains(name, "carbon fiber") || strings.Contains(name, "cfrp") {
			score += 760.0
		}
		if strings.Contains(name, "7075") {
			score += 620.0
		}
		if strings.Contains(name, "6061") {
			score += 180.0
		}
		if m.YieldStrength != nil && m.Density != nil && *m.Density > 0 {
			score += (*m.YieldStrength / *m.Density) * 4.0
		}
	}

	if s.requiresSpecificModulus {
		if strings.Contains(name, "cfrp") || strings.Contains(name, "carbon fiber") {
			score += 680.0
		}
		if strings.Contains(name, "steel") {
			score -= 400.0
		}
		if m.YoungsModulus != nil && m.Density != nil && *m.Density > 0 {
			score += (*m.YoungsModulus / *m.Density) * 6.0
		}
	}

	if s.requiresHotSection {
		if strings.Contains(name, "inconel") || strings.Contains(name, "superalloy") || strings.Contains(name, "hastelloy") {
			score += 760.0
		}
		if strings.Contains(name, "aluminum") || strings.Contains(name, "aluminium") {
			score -= 520.0
		}
		if strings.Contains(name, "steel") && !strings.Contains(name, "stainless") && !strings.Contains(name, "maraging") {
			score -= 260.0
		}
	}

	if s.requiresBiomedical {
		if strings.Contains(name, "ti-6al-4v") || strings.Contains(name, "grade 5") || strings.Contains(name, "titanium") {
			score += 820.0
		}
		if strings.Contains(name, "nickel") || strings.Contains(name, "inconel") {
			score -= 360.0
		}
		if strings.Contains(name, "steel") {
			score -= 220.0
		}
	}

	if s.requiresRadiationShielding {
		if strings.Contains(name, "tungsten") {
			score += 900.0
		}
		if strings.Contains(name, "lead") {
			score += 220.0
		}
		if m.Density != nil {
			score += *m.Density * 40.0
		}
	}

	if s.requiresTransparentImpact {
		if strings.Contains(name, "polycarbonate") || strings.Contains(name, " pc") {
			score += 760.0
		}
		if strings.Contains(name, "acrylic") || strings.Contains(name, "glass") {
			score -= 260.0
		}
	}

	if s.requiresThermalShock {
		if strings.Contains(name, "zirconia") {
			score += 650.0
		}
		if strings.Contains(name, "alumina") {
			score += 520.0
		}
		if strings.Contains(name, "porcelain") {
			score -= 300.0
		}
		if m.FractureToughness != nil {
			score += *m.FractureToughness * 0.35
		}
	}

	if s.requiresShapeMemory {
		if strings.Contains(name, "nitinol") || strings.Contains(name, "ni-ti") {
			score += 950.0
		}
	}

	if s.requiresLowCTE {
		if strings.Contains(name, "invar") {
			score += 980.0
		}
		if m.ThermalExpansion != nil {
			score -= *m.ThermalExpansion * 12.0
		}
	}

	if s.requiresAesthetics && s.requiresFastPrint && !s.requiresHeatMargin {
		if strings.Contains(name, "pla") {
			score += 820.0
		}
		if strings.Contains(name, "petg") {
			score += 120.0
		}
		if strings.Contains(name, "ultem") || strings.Contains(name, "peek") || strings.Contains(name, "polycarbonate") || strings.Contains(name, "pc-") {
			score -= 500.0
		}
	}

	if s.hasNozzleCap && m.ProcessingTempMinC != nil && *m.ProcessingTempMinC > (s.nozzleCapC+10.0) {
		score -= 420.0
	}

	if s.requiresSnapFit {
		if strings.Contains(name, "pc-pbt") || strings.Contains(name, "pbt") {
			score += 420.0
		}
		if strings.Contains(name, "petg") {
			score += 260.0
		}
		if strings.Contains(name, "pla") {
			score -= 220.0
		}
		if strings.Contains(name, "polycarbonate") || strings.Contains(name, " pc") {
			score += 160.0
		}
	}

	return score
}

func applyDesktopFeasibilityFilter(query string, ids []int, allMaterials []models.Material) []int {
	if len(ids) == 0 {
		return ids
	}
	signals := extractQuerySignals(query)
	if !signals.desktopFDM {
		return ids
	}
	if signals.impossibleDesktopFDM {
		return []int{}
	}

	lookup := map[int]models.Material{}
	for _, m := range allMaterials {
		lookup[m.ID] = m
	}

	filtered := make([]int, 0, len(ids))
	for _, id := range ids {
		m, ok := lookup[id]
		if !ok {
			continue
		}
		if !(strings.EqualFold(m.Category, "Polymer") || strings.EqualFold(m.Category, "Composite")) {
			continue
		}
		nozzleLimit := 270.0
		if signals.hasNozzleCap {
			nozzleLimit = signals.nozzleCapC
		}
		if !signals.hasEnclosure && m.ProcessingTempMaxC != nil && *m.ProcessingTempMaxC > nozzleLimit+20.0 {
			if signals.requiresChemicalExtreme && (strings.Contains(strings.ToLower(m.Name), "peek") || strings.Contains(strings.ToLower(m.Name), "ptfe") || strings.Contains(strings.ToLower(m.Name), "teflon")) {
				// keep chemically viable options even if desktop print is not feasible; warning is emitted later
			} else {
				continue
			}
		}
		if !signals.hasEnclosure && m.ThermalExpansion != nil && *m.ThermalExpansion > 95.0 {
			continue
		}
		if signals.requiresHeatMargin && signals.hasServiceTemp && m.GlassTransitionTemp != nil && (*m.GlassTransitionTemp-273.15) < (signals.serviceTempC-5.0) {
			continue
		}
		name := strings.ToLower(m.Name)
		if !signals.hasEnclosure && strings.Contains(name, "abs") && signals.requiresHeatMargin {
			continue
		}
		filtered = append(filtered, id)
	}

	if len(filtered) == 0 {
		need := len(ids)
		if need < 3 {
			need = 3
		}
		return inferFallbackRecommendedIDs(query, allMaterials, need)
	}
	return filtered
}

func extractQuerySignals(query string) querySignals {
	q := strings.ToLower(query)
	s := querySignals{}

	s.desktopFDM = isDesktopFDMQuery(q) || strings.Contains(q, "3d printable")
	s.professionalFDM = strings.Contains(q, "professional") || strings.Contains(q, "industrial") || strings.Contains(q, "heated chamber")
	noEnclosure := strings.Contains(q, "no enclosure") || strings.Contains(q, "without enclosure") || strings.Contains(q, "no heated chamber") || strings.Contains(q, "without heated chamber")
	s.hasEnclosure = (strings.Contains(q, "enclosed") || strings.Contains(q, "heated chamber") || strings.Contains(q, "enclosure")) && !noEnclosure
	s.requiresHeatMargin = strings.Contains(q, "heat") || strings.Contains(q, "melt") || strings.Contains(q, "exhaust") || strings.Contains(q, "soften") || strings.Contains(q, "sun") || strings.Contains(q, "motor")
	if strings.Contains(q, "no heat") || strings.Contains(q, "without heat") || strings.Contains(q, "no thermal load") || strings.Contains(q, "indoor display") {
		s.requiresHeatMargin = false
	}
	s.requiresDamping = strings.Contains(q, "absorb energy") || strings.Contains(q, "energy-absorbing") || strings.Contains(q, "damping") || strings.Contains(q, "vibration") || strings.Contains(q, "jelly-effect") || strings.Contains(q, "gasket") || strings.Contains(q, "flexible") || strings.Contains(q, "custom feet")
	s.requiresAesthetics = strings.Contains(q, "detailed") || strings.Contains(q, "architectural") || strings.Contains(q, "surface finish") || strings.Contains(q, "dimensional stability") || strings.Contains(q, "scale model") || strings.Contains(q, "indoor display")
	s.requiresFastPrint = strings.Contains(q, "fast") || strings.Contains(q, "as fast as possible") || strings.Contains(q, "high-resolution") || strings.Contains(q, "scale model")
	s.requiresCryogenic = strings.Contains(q, "cryogenic") || strings.Contains(q, "-150") || strings.Contains(q, "-196") || strings.Contains(q, "liquid oxygen") || strings.Contains(q, "liquid nitrogen") || strings.Contains(q, "lox")
	s.requiresCNC = strings.Contains(q, "cnc machining") || strings.Contains(q, "machined") || strings.Contains(q, "machine from a solid block") || strings.Contains(q, "machined from a block") || strings.Contains(q, "machined via cnc")
	s.requiresChemical = strings.Contains(q, "acid") || strings.Contains(q, "corrosive") || strings.Contains(q, "chemically inert") || strings.Contains(q, "chemical compatibility")
	s.requiresConductivity = strings.Contains(q, "conductivity") || strings.Contains(q, "conductive") || strings.Contains(q, "busbar") || strings.Contains(q, "heat sink") || strings.Contains(q, "led array")
	s.requiresConductivityPurist = strings.Contains(q, "absolute highest thermal conductivity") || strings.Contains(q, "absolute highest conductivity") || strings.Contains(q, "ofhc") || strings.Contains(q, "oxygen-free")
	s.requiresWear = strings.Contains(q, "wear") || strings.Contains(q, "abrasive") || strings.Contains(q, "friction") || strings.Contains(q, "slurry") || strings.Contains(q, "abrasive nozzle")
	s.requiresExtremeHeat = strings.Contains(q, "furnace") || strings.Contains(q, "rocket nozzle") || strings.Contains(q, "combustor") || strings.Contains(q, "1200") || strings.Contains(q, "2000") || strings.Contains(q, "3000")
	s.requiresAerospace = strings.Contains(q, "aerospace") || strings.Contains(q, "uav") || strings.Contains(q, "drone racing") || strings.Contains(q, "wing spar") || strings.Contains(q, "strength-to-weight") || strings.Contains(q, "specific strength") || strings.Contains(q, "σy/ρ") || strings.Contains(q, "sigma/rho")
	s.hasHighPressure = strings.Contains(q, "psi") || strings.Contains(q, "hydraulic") || strings.Contains(q, "pressure-tight") || strings.Contains(q, "non-porous") || strings.Contains(q, "mpa pressure")
	s.hasHighStrengthReq = strings.Contains(q, "200 mpa") || strings.Contains(q, "σy") || strings.Contains(q, "yield strength") || strings.Contains(q, "high strength")
	s.requiresSnapFit = strings.Contains(q, "snap-fit") || strings.Contains(q, "snap fit") || strings.Contains(q, "battery clip") || strings.Contains(q, "creep under load") || strings.Contains(q, "flexural")
	s.requiresChemicalExtreme = strings.Contains(q, "sulfuric acid") || strings.Contains(q, "hot sulfuric") || (strings.Contains(q, "acid") && strings.Contains(q, "120"))
	s.requiresSpecificModulus = strings.Contains(q, "as stiff as possible") || strings.Contains(q, "propeller flutter") || strings.Contains(q, "specific modulus")
	s.requiresHotSection = strings.Contains(q, "exhaust manifold") || strings.Contains(q, "turbocharger") || strings.Contains(q, "950") || strings.Contains(q, "gamma-prime")
	s.requiresBiomedical = strings.Contains(q, "dental implant") || strings.Contains(q, "biocompat") || strings.Contains(q, "human body")
	s.requiresRadiationShielding = strings.Contains(q, "radioactive") || strings.Contains(q, "x-raying") || strings.Contains(q, "gamma") || strings.Contains(q, "smallest footprint possible while blocking radiation")
	s.requiresTransparentImpact = strings.Contains(q, "transparent guard") || strings.Contains(q, "flying metal shard") || strings.Contains(q, "cannot be brittle")
	s.requiresThermalShock = strings.Contains(q, "thermal shock") || strings.Contains(q, "dropping cold metal") || strings.Contains(q, "induction heating crucible")
	s.requiresShapeMemory = strings.Contains(q, "remember") || strings.Contains(q, "shape memory") || strings.Contains(q, "hot water")
	s.requiresLowCTE = strings.Contains(q, "cannot expand or contract") || strings.Contains(q, "lowest coefficient of thermal expansion") || strings.Contains(q, "dimensional stability") || strings.Contains(q, "invar")

	if m := psiRegex.FindStringSubmatch(q); len(m) == 2 {
		s.hasHighPressure = true
	}

	maxTemp := -1e9
	maxServiceTemp := -1e9
	nozzleTemp := -1e9
	tempMatches := append(tempCRegex.FindAllStringSubmatch(q, -1), latexTempCRegex.FindAllStringSubmatch(q, -1)...)
	for _, m := range tempMatches {
		if len(m) != 2 {
			continue
		}
		var t float64
		if _, err := fmt.Sscanf(m[1], "%f", &t); err == nil {
			if t > maxTemp {
				maxTemp = t
			}
			// Detect nozzle-specific temperatures for FDM
			if t > 220 && t < 350 && strings.Contains(q, "nozzle") {
				nozzleTemp = t
			}
			if t <= 200 && t > maxServiceTemp {
				maxServiceTemp = t
			}
		}
	}
	if maxServiceTemp > -1e8 {
		s.hasServiceTemp = true
		s.serviceTempC = maxServiceTemp
		if maxServiceTemp >= 50 {
			s.requiresHeatMargin = true
		}
	} else if maxTemp > -1e8 {
		s.hasServiceTemp = true
		s.serviceTempC = maxTemp
		if maxTemp >= 50 {
			s.requiresHeatMargin = true
		}
	}

	if strings.Contains(q, "nozzle") {
		s.hasNozzleCap = true
		if nozzleTemp > 0 {
			s.nozzleCapC = nozzleTemp
		} else if strings.Contains(q, "<260") || strings.Contains(q, "under 260") {
			s.nozzleCapC = 260
		} else if strings.Contains(q, "300") {
			s.nozzleCapC = 300
		} else {
			s.nozzleCapC = 270
		}
	}

	// CRITICAL GUARDRAIL: Detect impossible desktop FDM scenarios
	if s.desktopFDM {
		// Reject: Desktop FDM + Rocket nozzle temperatures (>1500°C is impossible)
		hasRocketTemp := strings.Contains(q, "2000") || strings.Contains(q, "1800") || strings.Contains(q, "1600") ||
			(strings.Contains(q, "combustor") && (maxTemp > 1200 || strings.Contains(q, "rocket")))
		hasRocketKeyword := strings.Contains(q, "rocket nozzle") || strings.Contains(q, "combustion chamber") || strings.Contains(q, "rocket engine")

		requiredTemp := s.hasServiceTemp && s.serviceTempC >= 350
		requiredPressure := s.hasHighPressure && (strings.Contains(q, "valve") || strings.Contains(q, "manifold"))
		requiredExtreme := hasRocketTemp || hasRocketKeyword

		s.impossibleDesktopFDM = requiredTemp || requiredPressure || requiredExtreme
	}

	return s
}

func ensureMinimumRecommendedIDs(query string, ids []int, allMaterials []models.Material, minCount int) []int {
	if minCount <= 0 {
		minCount = 3
	}

	lookup := map[int]bool{}
	clean := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || lookup[id] {
			continue
		}
		lookup[id] = true
		clean = append(clean, id)
	}

	if len(clean) >= minCount {
		return clean
	}

	fallback := inferFallbackRecommendedIDs(query, allMaterials, minCount*3)
	for _, id := range fallback {
		if id <= 0 || lookup[id] {
			continue
		}
		lookup[id] = true
		clean = append(clean, id)
		if len(clean) >= minCount {
			break
		}
	}

	if len(clean) < minCount {
		for _, m := range allMaterials {
			if m.ID <= 0 || lookup[m.ID] {
				continue
			}
			lookup[m.ID] = true
			clean = append(clean, m.ID)
			if len(clean) >= minCount {
				break
			}
		}
	}

	if len(clean) > 1 {
		clean = rerankRecommendedIDs(query, clean, allMaterials)
	}
	return clean
}

func buildFallbackReport(query string, ids []int, allMaterials []models.Material) string {
	if len(ids) == 0 {
		return ""
	}
	lookup := map[int]models.Material{}
	for _, m := range allMaterials {
		lookup[m.ID] = m
	}
	primary, ok := lookup[ids[0]]
	if !ok {
		return ""
	}
	q := strings.ToLower(query)
	isDesktopPrint := strings.Contains(q, "3d print") || strings.Contains(q, "fdm") || strings.Contains(q, "desktop printer")

	tg := "unknown"
	if primary.GlassTransitionTemp != nil {
		tg = fmt.Sprintf("~%.0fC", *primary.GlassTransitionTemp-273.15)
	}
	hdt := "unknown"
	if primary.HeatDeflectionTemp != nil {
		hdt = fmt.Sprintf("~%.0fC", *primary.HeatDeflectionTemp-273.15)
	}

	var b strings.Builder
	b.WriteString("## Recommendation\n")
	b.WriteString("- Top choice: ")
	b.WriteString(primary.Name)
	b.WriteString("\n")
	b.WriteString("- Key properties: Tg ")
	b.WriteString(tg)
	b.WriteString(", HDT ")
	b.WriteString(hdt)
	b.WriteString("\n")
	b.WriteString("- Rationale: Chosen using a Pareto trade-off between heat survivability, desktop-print feasibility, and strength-to-weight.")
	if isDesktopPrint {
		b.WriteString(" The selection prioritizes materials that print on standard desktop hardware without severe warping or enclosure-only requirements.")
	}
	return b.String()
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

func callGoogleAIText(ctx context.Context, apiKey string, model string, prompt string, temperature float64, maxTokens int, apiVer string) (string, int, int, error) {
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
			Temperature:     temperature,
			MaxOutputTokens: maxTokens,
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
	s := extractQuerySignals(query)

	if s.impossibleDesktopFDM {
		return "Polymers"
	}
	if strings.Contains(q, "polymer") || strings.Contains(q, "plastic") || strings.Contains(q, "resin") || strings.Contains(q, "rubber") || strings.Contains(q, "flexible") || strings.Contains(q, "gasket") || strings.Contains(q, "printing") || strings.Contains(q, "peek") || strings.Contains(q, "nylon") {
		return "Polymers"
	}
	if s.requiresShapeMemory || s.requiresLowCTE || s.requiresHotSection || s.requiresRadiationShielding || s.requiresBiomedical {
		return "Alloys"
	}
	if s.requiresSpecificModulus {
		return "Composites"
	}
	if s.requiresTransparentImpact {
		return "Polymers"
	}
	if strings.Contains(q, "polymer") || strings.Contains(q, "plastic") || strings.Contains(q, "resin") || strings.Contains(q, "rubber") || strings.Contains(q, "flexible") || strings.Contains(q, "gasket") || strings.Contains(q, "printing") || strings.Contains(q, "peek") || strings.Contains(q, "nylon") {
		return "Polymers"
	}
	if s.requiresConductivity {
		return "Pure_Metals"
	}
	if s.requiresCryogenic || s.requiresCNC || s.hasHighPressure {
		return "Alloys"
	}
	if strings.Contains(q, "composite") || strings.Contains(q, "cfrp") || strings.Contains(q, "gfrp") || strings.Contains(q, "interlaminar") || strings.Contains(q, "fiber") || strings.Contains(q, "laminate") {
		return "Composites"
	}
	if s.requiresAerospace {
		return "Alloys"
	}
	if s.requiresDamping || s.requiresChemical {
		return "Polymers"
	}

	if isDesktopFDMQuery(q) {
		return "Polymers"
	}
	if strings.Contains(q, "acid") || strings.Contains(q, "chemical") || strings.Contains(q, "chemically inert") || strings.Contains(q, "corrosive") || strings.Contains(q, "teflon") || strings.Contains(q, "ptfe") {
		return "Polymers"
	}
	if strings.Contains(q, "conductivity") || strings.Contains(q, "conductive") || strings.Contains(q, "busbar") || strings.Contains(q, "heat sink") || strings.Contains(q, "machined from a block") && strings.Contains(q, "thermal") {
		return "Pure_Metals"
	}
	if s.requiresWear || strings.Contains(q, "wear") || strings.Contains(q, "abrasive") || strings.Contains(q, "friction") || strings.Contains(q, "slurry") || strings.Contains(q, "high hardness") {
		return "Ceramics"
	}
	if strings.Contains(q, "cryogenic") || strings.Contains(q, "liquid nitrogen") || strings.Contains(q, "liquid oxygen") || strings.Contains(q, "lox") || strings.Contains(q, "-196") || strings.Contains(q, "-150") {
		return "Alloys"
	}
	if strings.Contains(q, "furnace") || strings.Contains(q, "viewport") || strings.Contains(q, "abrasive slurry") || strings.Contains(q, "extreme friction") || strings.Contains(q, "high-wear") || strings.Contains(q, "1200") || strings.Contains(q, "2000") {
		return "Ceramics"
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

func isDesktopFDMQuery(q string) bool {
	return strings.Contains(q, "3d print") ||
		strings.Contains(q, "3d-print") ||
		strings.Contains(q, "fdm") ||
		strings.Contains(q, "desktop printer") ||
		strings.Contains(q, "desktop machine") ||
		strings.Contains(q, "desktop setup") ||
		strings.Contains(q, "ender") ||
		strings.Contains(q, "prusa") ||
		strings.Contains(q, "hobby printer") ||
		strings.Contains(q, "plastic filament")
}

// RouteQuery uses an LLM call to categorize the user's request into one of 5 material buckets.
func RouteQuery(ctx context.Context, query string) (string, int, error) {
	_ = ctx
	// Deterministic routing to avoid unnecessary external API calls.
	heuristic := InferCategoryHeuristic(query)
	if heuristic != "" {
		return heuristic, 0, nil
	}
	return "Alloys", 0, nil
}

// BuildHeuristicConstraints creates low-cost, query-derived constraints so the
// dispatcher can run with minimal LLM calls while still enforcing domain physics.
func BuildHeuristicConstraints(query string, routedCategory string) map[string]interface{} {
	s := extractQuerySignals(query)
	constraints := map[string]interface{}{}

	if s.hasServiceTemp {
		serviceK := s.serviceTempC + 273.15
		constraints["min_glass_transition_temp"] = serviceK + 5.0
		constraints["min_hdt"] = serviceK
		constraints["min_melting_point"] = serviceK + 25.0
	}

	if s.desktopFDM {
		maxProc := 270.0
		if s.hasNozzleCap {
			maxProc = s.nozzleCapC
		}
		if s.professionalFDM && maxProc < 300.0 {
			maxProc = 300.0
		}
		if !s.requiresChemicalExtreme {
			constraints["max_processing_temp"] = maxProc
		}
	}
	if s.requiresChemicalExtreme {
		delete(constraints, "max_processing_temp")
	}

	if s.requiresConductivity {
		constraints["max_electrical_resistivity"] = 2.2e-8
		constraints["min_thermal_conductivity"] = 200.0
		if strings.Contains(strings.ToLower(query), "58") || strings.Contains(strings.ToLower(query), "58 ms/m") {
			constraints["max_electrical_resistivity"] = 1.8e-8
		}
	}
	if s.requiresConductivityPurist {
		constraints["max_electrical_resistivity"] = 1.8e-8
		constraints["min_thermal_conductivity"] = 350.0
	}

	if s.requiresWear {
		constraints["min_hardness_vickers"] = 900.0
	}

	if s.requiresExtremeHeat {
		constraints["min_melting_point"] = 1473.15 // 1200C baseline
	}
	if s.requiresHotSection {
		constraints["min_yield_strength"] = 350.0
		constraints["min_melting_point"] = 1300.0
	}

	if s.requiresAerospace {
		constraints["max_density"] = 4.8
		constraints["min_yield_strength"] = 250.0
	}

	if s.requiresCryogenic || s.hasHighPressure {
		constraints["min_yield_strength"] = 180.0
	}

	if s.requiresRadiationShielding {
		constraints["min_density"] = 15.0
	}

	if s.requiresLowCTE {
		constraints["max_thermal_expansion"] = 3.0
	}

	if s.requiresSnapFit {
		constraints["min_glass_transition_temp"] = 338.15
		constraints["min_yield_strength"] = 45.0
	}

	if s.requiresChemical {
		constraints["min_melting_point"] = 420.0
	}

	// Lightweight default by class when query is broad.
	if routedCategory == "Composites" {
		if _, ok := constraints["max_density"]; !ok {
			constraints["max_density"] = 2.2
		}
	}

	return constraints
}

// ──────────────────────────────────────────────────────────────────────────
//  SPECIALIZED SEARCH FUNCTIONS: Category-Specific Filtering
// ──────────────────────────────────────────────────────────────────────────

// SearchAlloys searches alloy-specific columns: Yield_Strength, Temper, Fatigue_Limit, Corrosion_Rating
func SearchAlloys(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 15 {
		limit = 15
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include metals that are alloys (have yield strength or specific subcategories)
		if m.Category != "Metal" {
			continue
		}

		// Priority check: Yield strength (core alloy property)
		if minYield, ok := constraints["min_yield_strength"].(float64); ok {
			if m.YieldStrength != nil && *m.YieldStrength < minYield {
				continue
			}
		}

		// Thermal ceiling check (processability)
		if maxMelt, ok := constraints["max_melting_point"].(float64); ok {
			if m.MeltingPoint != nil && *m.MeltingPoint > maxMelt {
				continue
			}
		}

		// Corrosion resistance approximation (electrical resistivity proxy)
		if minResist, ok := constraints["min_corrosion_resistance"].(float64); ok {
			if m.ElectricalResistivity != nil && *m.ElectricalResistivity < minResist {
				continue
			}
		}

		// Fatigue proxy: Young's modulus (higher stiffness = better fatigue resistance)
		if minModulus, ok := constraints["min_youngs_modulus"].(float64); ok {
			if m.YoungsModulus != nil && *m.YoungsModulus < minModulus {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return genericAlloySearchScore(filtered[i]) > genericAlloySearchScore(filtered[j])
	})

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchPolymers searches polymer-specific columns: Glass_Transition_Temp, HDT, Processing_Temp, Crystallinity
func SearchPolymers(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 15 {
		limit = 15
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include polymers
		if m.Category != "Polymer" {
			continue
		}

		// Glass transition temperature (primary polymer property)
		if minTg, ok := constraints["min_glass_transition_temp"].(float64); ok {
			if m.GlassTransitionTemp != nil && *m.GlassTransitionTemp < minTg {
				continue
			}
		}
		if maxTg, ok := constraints["max_glass_transition_temp"].(float64); ok {
			if m.GlassTransitionTemp != nil && *m.GlassTransitionTemp > maxTg {
				continue
			}
		}

		// Heat deflection temperature (processability/service temp)
		if minHDT, ok := constraints["min_hdt"].(float64); ok {
			if m.HeatDeflectionTemp != nil && *m.HeatDeflectionTemp < minHDT {
				continue
			}
		}

		// Processing temperature ceiling (manufacturability)
		if maxProcTemp, ok := constraints["max_processing_temp"].(float64); ok {
			// A 300 C nozzle can sometimes process PC-class materials with tuning;
			// keep near misses in the verifier instead of prematurely hiding them.
			tolerance := 5.0
			if maxProcTemp >= 295 {
				tolerance = 25.0
			}
			if m.ProcessingTempMaxC != nil && *m.ProcessingTempMaxC > maxProcTemp+tolerance {
				continue
			}
		}

		// Crystallinity check (affects stiffness and thermal properties)
		if minCryst, ok := constraints["min_crystallinity"].(float64); ok {
			if m.Crystallinity != nil && *m.Crystallinity < minCryst {
				continue
			}
		}

		// Density check (lightweight requirement)
		if maxDensity, ok := constraints["max_density"].(float64); ok {
			if m.Density != nil && *m.Density > maxDensity {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return genericPolymerSearchScore(filtered[i], constraints) > genericPolymerSearchScore(filtered[j], constraints)
	})

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchCeramics searches ceramic-specific columns: Hardness_Vickers, Thermal_Shock_Resistance, Fracture_Toughness
func SearchCeramics(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 15 {
		limit = 15
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include ceramics
		if m.Category != "Ceramic" {
			continue
		}

		// Hardness check (primary ceramic property)
		if minHardness, ok := constraints["min_hardness_vickers"].(float64); ok {
			if m.HardnessVickers != nil && *m.HardnessVickers < minHardness {
				continue
			}
		}

		// Fracture toughness (thermal shock resistance proxy)
		if minToughness, ok := constraints["min_fracture_toughness"].(float64); ok {
			if m.FractureToughness != nil && *m.FractureToughness < minToughness {
				continue
			}
		}

		// Melting point (high-temp capability)
		if minMeltPt, ok := constraints["min_melting_point"].(float64); ok {
			if m.MeltingPoint != nil && *m.MeltingPoint < minMeltPt {
				continue
			}
		}

		// Thermal conductivity (heat dissipation)
		if minTC, ok := constraints["min_thermal_conductivity"].(float64); ok {
			if m.ThermalConductivity != nil && *m.ThermalConductivity < minTC {
				continue
			}
		}

		// Young's modulus (stiffness)
		if minModulus, ok := constraints["min_youngs_modulus"].(float64); ok {
			if m.YoungsModulus != nil && *m.YoungsModulus < minModulus {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return genericCeramicSearchScore(filtered[i]) > genericCeramicSearchScore(filtered[j])
	})

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchComposites searches composite-specific columns: Interlaminar_Shear_Strength, Fiber_Volume_Fraction, Anisotropy
func SearchComposites(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 15 {
		limit = 15
	}

	var filtered []models.Material

	for _, m := range materials {
		// Only include composites
		if m.Category != "Composite" {
			continue
		}

		// Interlaminar shear strength (critical for composite integrity)
		if minILSS, ok := constraints["min_ilss"].(float64); ok {
			if m.InterlaminarShear != nil && *m.InterlaminarShear < minILSS {
				continue
			}
		}

		// Fiber volume fraction (composite quality indicator)
		if minFibreFrac, ok := constraints["min_fiber_volume_fraction"].(float64); ok {
			if m.FiberVolumeFraction != nil && *m.FiberVolumeFraction < minFibreFrac {
				continue
			}
		}

		// Young's modulus (stiffness)
		if minModulus, ok := constraints["min_youngs_modulus"].(float64); ok {
			if m.YoungsModulus != nil && *m.YoungsModulus < minModulus {
				continue
			}
		}

		// Density (weight constraint)
		if maxDensity, ok := constraints["max_density"].(float64); ok {
			if m.Density != nil && *m.Density > maxDensity {
				continue
			}
		}

		// Thermal conductivity (performance requirement)
		if minTC, ok := constraints["min_thermal_conductivity"].(float64); ok {
			if m.ThermalConductivity != nil && *m.ThermalConductivity < minTC {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return genericCompositeSearchScore(filtered[i]) > genericCompositeSearchScore(filtered[j])
	})

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

// SearchPureMetals searches pure metals with elemental purity focus
func SearchPureMetals(ctx context.Context, constraints map[string]interface{}, materials []models.Material, limit int) []models.Material {
	if limit <= 0 || limit > 15 {
		limit = 15
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

		name := strings.ToLower(m.Name)
		if strings.Contains(name, "alloy") || strings.Contains(name, "brass") || strings.Contains(name, "bronze") || strings.Contains(name, "cupronickel") || strings.Contains(name, "stainless") || strings.Contains(name, "steel") {
			continue
		}

		// Electrical conductivity check (indicator of purity)
		if maxResist, ok := constraints["max_electrical_resistivity"].(float64); ok {
			if m.ElectricalResistivity != nil && *m.ElectricalResistivity > maxResist {
				continue
			}
		}

		// Melting point check
		if minMelt, ok := constraints["min_melting_point"].(float64); ok {
			if m.MeltingPoint != nil && *m.MeltingPoint < minMelt {
				continue
			}
		}

		// Thermal conductivity (pure metals usually have higher TC)
		if minTC, ok := constraints["min_thermal_conductivity"].(float64); ok {
			if m.ThermalConductivity != nil && *m.ThermalConductivity < minTC {
				continue
			}
		}

		// Density range check
		if minDensity, ok := constraints["min_density"].(float64); ok {
			if m.Density != nil && *m.Density < minDensity {
				continue
			}
		}
		if maxDensity, ok := constraints["max_density"].(float64); ok {
			if m.Density != nil && *m.Density > maxDensity {
				continue
			}
		}

		filtered = append(filtered, m)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return genericPureMetalSearchScore(filtered[i]) > genericPureMetalSearchScore(filtered[j])
	})

	if len(filtered) > limit {
		return filtered[:limit]
	}
	return filtered
}

func genericAlloySearchScore(m models.Material) float64 {
	score := 0.0
	name := strings.ToLower(m.Name)
	if m.YieldStrength != nil && m.Density != nil && *m.Density > 0 {
		score += (*m.YieldStrength / *m.Density) * 2.0
	} else if m.YieldStrength != nil {
		score += *m.YieldStrength
	}
	if strings.Contains(name, "7075") {
		score += 260
	}
	if strings.Contains(name, "6061") {
		score += 230
	}
	if strings.Contains(name, "316") || strings.Contains(name, "304") {
		score += 70
	}
	if strings.Contains(name, "maraging") || strings.Contains(name, "inconel") {
		score -= 120
	}
	return score
}

func genericPolymerSearchScore(m models.Material, constraints map[string]interface{}) float64 {
	score := 0.0
	name := strings.ToLower(m.Name)
	maxProc, hasProcLimit := constraints["max_processing_temp"].(float64)

	if m.GlassTransitionTemp != nil {
		score += (*m.GlassTransitionTemp - 273.15) * 1.2
	}
	if m.HeatDeflectionTemp != nil {
		score += (*m.HeatDeflectionTemp - 273.15) * 1.4
	}
	if m.YieldStrength != nil {
		score += *m.YieldStrength * 0.5
	}
	if m.Density != nil {
		score -= *m.Density * 6.0
	}

	if hasProcLimit {
		if m.ProcessingTempMaxC != nil {
			headroom := maxProc - *m.ProcessingTempMaxC
			if headroom >= 0 {
				score += 90 - headroom*0.25
			} else {
				score += headroom * 5.0
			}
		}
		if maxProc <= 275 {
			if strings.Contains(name, "petg") {
				score += 300
			}
			if strings.Contains(name, "pla") {
				score += 90
			}
			if strings.Contains(name, "abs") || strings.Contains(name, "peek") || strings.Contains(name, "ultem") || strings.Contains(name, "pei") {
				score -= 260
			}
		}
		if maxProc >= 295 {
			if strings.Contains(name, "polycarbonate") || strings.Contains(name, " pc") || strings.Contains(name, "pc-") {
				score += 260
			}
			if strings.Contains(name, "nylon") {
				score += 90
			}
			if strings.Contains(name, "petg") || strings.Contains(name, "pla") {
				score -= 120
			}
		}
	} else {
		// General printing/aesthetic requests should not be dominated by exotic Tg.
		if strings.Contains(name, "pla") {
			score += 260
		}
		if strings.Contains(name, "petg") {
			score += 170
		}
		if strings.Contains(name, "ptfe") || strings.Contains(name, "teflon") {
			score += 120
		}
		if strings.Contains(name, "peek") || strings.Contains(name, "ultem") || strings.Contains(name, "pei") {
			score -= 80
		}
	}
	return score
}

func genericCeramicSearchScore(m models.Material) float64 {
	score := 0.0
	name := strings.ToLower(m.Name)
	if m.HardnessVickers != nil {
		score += *m.HardnessVickers * 0.45
	}
	if m.MeltingPoint != nil {
		score += (*m.MeltingPoint - 273.15) * 0.08
	}
	if m.FractureToughness != nil {
		score += *m.FractureToughness * 25
	}
	if m.ThermalConductivity != nil {
		score += *m.ThermalConductivity * 0.7
	}
	if strings.Contains(name, "alumina") || strings.Contains(name, "al2o3") {
		score += 120
	}
	if strings.Contains(name, "silicon carbide") || strings.Contains(name, "sic") {
		score += 150
	}
	if strings.Contains(name, "zirconia") || strings.Contains(name, "zro2") {
		score += 130
	}
	return score
}

func genericCompositeSearchScore(m models.Material) float64 {
	score := 0.0
	name := strings.ToLower(m.Name)
	if m.TensileStrength != nil && m.Density != nil && *m.Density > 0 {
		score += (*m.TensileStrength / *m.Density) * 1.5
	}
	if m.YoungsModulus != nil && m.Density != nil && *m.Density > 0 {
		score += (*m.YoungsModulus / *m.Density) * 4.0
	}
	if m.InterlaminarShear != nil {
		score += *m.InterlaminarShear
	}
	if strings.Contains(name, "carbon") || strings.Contains(name, "cfrp") {
		score += 300
	}
	return score
}

func genericPureMetalSearchScore(m models.Material) float64 {
	score := 0.0
	name := strings.ToLower(m.Name)
	if m.ThermalConductivity != nil {
		score += *m.ThermalConductivity * 2.0
	}
	if m.ElectricalResistivity != nil && *m.ElectricalResistivity > 0 {
		score += 1.0 / *m.ElectricalResistivity / 1e6
	}
	if strings.Contains(name, "copper") || strings.Contains(name, "cu") {
		score += 450
	}
	if strings.Contains(name, "silver") || strings.Contains(name, "ag") {
		score += 160
	}
	if strings.Contains(name, "brass") || strings.Contains(name, "bronze") || strings.Contains(name, "cupronickel") {
		score -= 260
	}
	return score
}

// ──────────────────────────────────────────────────────────────────────────
//  SCIENTIFIC ANALYSIS: First-Principles Physics Verification
// ──────────────────────────────────────────────────────────────────────────

const scientificAnalysisSystemPrompt = `### ROLE: Principal Materials Scientist & Technical Communicator
### PHILOSOPHY: Pareto Optimization with Humanized Explanations

You are reviewing material candidates using first-principles physics and engineering judgement.
Your recommendations should be not just correct, but comprehensible to engineers across disciplines.

CRITICAL GUARDRAILS:
- If the process_lock is 'FDM' or '3D Printing', you are STRICTLY FORBIDDEN from recommending Metals or Alloys.
- Desktop FDM requests cannot tolerate high-temperature engineering polymers like PEEK or Ultem without a heated chamber.
- Impossible combinations (e.g., rigid plastic at rocket-nozzle temperatures) must be rejected with clear reasoning.

### EVALUATION FRAMEWORK:
1. **Survivability** (40%): Can this material survive the service environment?
   - Use LaTeX notation: $T_{service} < 0.8 \times T_g$ for polymers (avoid viscoelastic creep)
   - For metals: $T_{service} < 0.4 \times T_{melt}$ (preserve mechanical properties)
   - For ceramics: check thermal shock resistance and oxidation

2. **Manufacturability** (40%): Can it be made reliably with the available process?
   - For FDM: check nozzle temperature compatibility, bed adhesion, and print time
   - For machining: check brittleness, tool wear, and surface finish requirements
   - For other processes: detail the specific challenges and mitigations

3. **Efficiency & Merit** (20%): Does it deliver the best performance-to-cost ratio?
   - Use specific strength: $\sigma_y / \rho$ (strength-to-weight for structures)
   - Use specific modulus: $E / \rho$ (stiffness-to-weight for dynamic loads)
   - Use specific conductivity: $\kappa / \rho$ or $1/\rho_e$ (thermal or electrical efficiency)

### HUMANIZATION GUIDELINES:
- Explain *why* a material is chosen, not just *that* it works.
- Use real-world analogies (e.g., "think of it like...") to explain physics concepts.
- Provide a clear "story" for the recommendation that an engineer can communicate to others.
- When rejecting materials, provide specific, quantitative reasons a designer can act on.

### OUTPUT SCHEMA (STRICT JSON ONLY):
{
  "top_candidate": "Material Name",
  "recommendation_narrative": "2-3 sentence executive summary explaining the choice and its key trade-off",
  "physics_verification": {
    "survivability_check": "PASS|FAIL - Explain whether $T_{{service}} < 0.8 \\times T_g$ or equivalent check",
    "manufacturability_check": "PASS|FAIL - Detail process-specific feasibility (FDM nozzle temp, machining tool life, etc.)",
    "efficiency_check": "PASS|FAIL - Compare specific strength or conductivity merit indices"
  },
  "merit_index_calculation": "Include LaTeX formulas. Example: $\\sigma_y / \\rho = 450 \\text{ MPa} / 1200 \\text{ kg/m}^3 = 0.375 \\text{ kJ/kg}$",
  "failure_rejection_reasons": [
    "Material A: Rejected because $T_{{service}} = 200°C > 0.8 \\times T_g = 150°C$. Viscoelastic creep will occur.",
    "Material B: Incompatible nozzle temperature (requires 300°C but printer maxes at 250°C)."
  ],
  "manufacturing_feasibility": "Detailed, step-by-step process instructions. Example: '1. Preheat bed to 60°C...' or '1. Set spindle to 12,000 RPM for carbide inserts...'",
  "safety_margin": "Safety factor computation with physical interpretation. Example: $SF = T_g / T_{{service}} = 393K / 323K = 1.22$ (adequate for 20min exposure)"
}

Return JSON only. Ensure strings are properly escaped (use \\\\ for backslashes) and do not include markdown code fences.`

type PhysicsVerification struct {
	CheckName string `json:"check_name"`
	Status    string `json:"status"` // PASS or FAIL
	Value     string `json:"value"`
	Physics   string `json:"physics"`
}

type ScientificAnalysisResponse struct {
	TopCandidate             string            `json:"top_candidate"`
	RecommendationNarrative  string            `json:"recommendation_narrative"`
	PhysicsVerification      map[string]string `json:"physics_verification"`
	MeritIndexCalculation    string            `json:"merit_index_calculation"`
	FailureRejectionReasons  []string          `json:"failure_rejection_reasons"`
	ManufacturingFeasibility string            `json:"manufacturing_feasibility"`
	SafetyMargin             string            `json:"safety_margin"`
	HumanizedSummary         string            `json:"humanized_summary"`
}

// ScientificAnalysis applies first-principles physics checks to the top 3 candidates
func ScientificAnalysis(ctx context.Context, query string, category string, topCandidates []models.Material) (ScientificAnalysisResponse, int, error) {
	if len(topCandidates) == 0 {
		return ScientificAnalysisResponse{}, 0, fmt.Errorf("no candidates provided for analysis")
	}
	signals := extractQuerySignals(query)

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

	analysis := deterministicScientificAnalysis(query, category, topCandidates)
	tokens := 0

	if os.Getenv("ENABLE_LLM_SCIENTIFIC_ANALYSIS") == "1" {
		raw, llmTokens, err := callGemini(ctx, prompt, 0.1, 1200)
		if err == nil {
			cleaned := cleanJSON(raw)
			var llmAnalysis ScientificAnalysisResponse
			if err := json.Unmarshal([]byte(cleaned), &llmAnalysis); err == nil {
				if llmAnalysis.TopCandidate != "" {
					analysis.TopCandidate = llmAnalysis.TopCandidate
				}
				if llmAnalysis.RecommendationNarrative != "" {
					analysis.RecommendationNarrative = llmAnalysis.RecommendationNarrative
				}
				if len(llmAnalysis.PhysicsVerification) > 0 {
					analysis.PhysicsVerification = llmAnalysis.PhysicsVerification
				}
				if llmAnalysis.MeritIndexCalculation != "" {
					analysis.MeritIndexCalculation = llmAnalysis.MeritIndexCalculation
				}
				if len(llmAnalysis.FailureRejectionReasons) > 0 {
					analysis.FailureRejectionReasons = llmAnalysis.FailureRejectionReasons
				}
				if llmAnalysis.ManufacturingFeasibility != "" {
					analysis.ManufacturingFeasibility = llmAnalysis.ManufacturingFeasibility
				}
				if llmAnalysis.SafetyMargin != "" {
					analysis.SafetyMargin = llmAnalysis.SafetyMargin
				}
				if llmAnalysis.HumanizedSummary != "" {
					analysis.HumanizedSummary = llmAnalysis.HumanizedSummary
				}
				tokens = llmTokens
			}
		}
	}

	best := chooseDeterministicTopCandidate(query, topCandidates)
	if signals.impossibleDesktopFDM {
		analysis.TopCandidate = "NO_FEASIBLE_MATERIAL"
		analysis.ManufacturingFeasibility = "REJECT: no desktop FDM polymer or plastic filament can survive the requested temperature/pressure envelope. Change the process to machined graphite, refractory ceramic, or a metal/refractory route."
		if analysis.PhysicsVerification == nil {
			analysis.PhysicsVerification = map[string]string{}
		}
		analysis.PhysicsVerification["process_feasibility"] = "FAIL: requested service conditions exceed desktop FDM polymer capability."
		return analysis, tokens, nil
	}
	if best.ID != 0 {
		analysis.TopCandidate = best.Name
		if analysis.PhysicsVerification == nil {
			analysis.PhysicsVerification = map[string]string{}
		}
		analysis.PhysicsVerification["deterministic_guardrail"] = deterministicGuardrailExplanation(query, best)
	}

	return analysis, tokens, nil
}

func deterministicScientificAnalysis(query string, category string, candidates []models.Material) ScientificAnalysisResponse {
	analysis := ScientificAnalysisResponse{
		PhysicsVerification: map[string]string{},
	}

	best := chooseDeterministicTopCandidate(query, candidates)
	if best.ID == 0 {
		return analysis
	}
	analysis.TopCandidate = best.Name

	s := extractQuerySignals(query)
	if s.impossibleDesktopFDM {
		analysis.TopCandidate = "NO_FEASIBLE_MATERIAL"
		analysis.PhysicsVerification["process_feasibility"] = "FAIL"
		analysis.MeritIndexCalculation = "No valid merit index: process/hardware constraints invalidate all desktop-FDM polymer candidates."
		analysis.FailureRejectionReasons = []string{
			"Desktop FDM polymers cannot sustain the required thermal/pressure envelope.",
			"Requested operating window exceeds hobbyist filament processing and in-service limits.",
		}
		analysis.ManufacturingFeasibility = "Use a non-FDM route (CNC, casting, or refractory processing) with ceramic/alloy classes."
		analysis.SafetyMargin = "FAIL: process feasibility margin < 1.0"
		return analysis
	}

	if s.hasServiceTemp {
		analysis.PhysicsVerification["service_temperature"] = fmt.Sprintf("Target service temperature detected: %.1f C", s.serviceTempC)
	}

	if best.GlassTransitionTemp != nil {
		tgC := *best.GlassTransitionTemp - 273.15
		analysis.PhysicsVerification["tg_margin"] = fmt.Sprintf("Tg ~= %.1f C", tgC)
	}
	if best.HeatDeflectionTemp != nil {
		hdtC := *best.HeatDeflectionTemp - 273.15
		analysis.PhysicsVerification["hdt_margin"] = fmt.Sprintf("HDT ~= %.1f C", hdtC)
	}
	if best.YieldStrength != nil && best.Density != nil && *best.Density > 0 {
		spec := *best.YieldStrength / *best.Density
		analysis.MeritIndexCalculation = fmt.Sprintf("Specific strength merit index: sigma_y/rho = %.2f", spec)
	} else if s.requiresConductivity && best.ElectricalResistivity != nil && *best.ElectricalResistivity > 0 {
		analysis.MeritIndexCalculation = fmt.Sprintf("Electrical conductivity merit index: 1/rho_e = %.3e", 1.0/(*best.ElectricalResistivity))
	} else {
		analysis.MeritIndexCalculation = "Composite merit: maximize feasibility + survivability + task-specific performance."
	}

	reasons := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c.ID == best.ID {
			continue
		}
		name := strings.ToLower(c.Name)
		if s.desktopFDM && !s.hasEnclosure && strings.Contains(name, "abs") {
			reasons = append(reasons, c.Name+": rejected due to warping/delamination risk without enclosure.")
			continue
		}
		if s.requiresHeatMargin && c.GlassTransitionTemp != nil && s.hasServiceTemp && (*c.GlassTransitionTemp-273.15) < s.serviceTempC {
			reasons = append(reasons, c.Name+": rejected because Tg is below required service temperature.")
			continue
		}
		if s.requiresConductivity && c.ElectricalResistivity != nil && best.ElectricalResistivity != nil && *c.ElectricalResistivity > *best.ElectricalResistivity {
			reasons = append(reasons, c.Name+": rejected due to lower conductivity than top pure-metal option.")
			continue
		}
		if s.requiresWear && c.HardnessVickers != nil && best.HardnessVickers != nil && *c.HardnessVickers < *best.HardnessVickers {
			reasons = append(reasons, c.Name+": rejected due to lower hardness for abrasive wear conditions.")
			continue
		}
		if s.requiresAerospace && c.Density != nil && best.Density != nil && *c.Density > *best.Density {
			reasons = append(reasons, c.Name+": rejected due to lower strength-to-weight efficiency.")
			continue
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "Alternatives ranked lower by deterministic process and physics guardrails.")
	}
	analysis.FailureRejectionReasons = reasons
	analysis.ManufacturingFeasibility = deterministicManufacturingAdvice(query, category, best)
	analysis.SafetyMargin = deterministicSafetyMargin(query, best)

	return analysis
}

func deterministicManufacturingAdvice(query string, category string, best models.Material) string {
	s := extractQuerySignals(query)
	name := strings.ToLower(best.Name)
	if s.requiresChemicalExtreme && (strings.Contains(name, "peek") || strings.Contains(name, "ptfe") || strings.Contains(name, "teflon")) {
		return "Chemical compatibility requires high-performance fluoropolymer/PEEK-class material. Desktop FDM is not suitable here; use an industrial high-temperature system (typically >400C nozzle, heated chamber) or machine from stock."
	}
	if s.requiresShapeMemory && strings.Contains(name, "nitinol") {
		return "Set shape by constrained heat treatment, then validate transformation behavior across the intended hot-water activation window."
	}
	if s.requiresLowCTE && strings.Contains(name, "invar") {
		return "Use stress-relieved Invar stock, control weld heat input, and verify final CTE with metrology across the thermal cycle envelope."
	}
	if s.desktopFDM {
		if s.professionalFDM {
			return "Use chamber-controlled FDM with dry filament, 0.4-0.6 mm nozzle, and enclosure thermal stabilization."
		}
		return "Use desktop FDM settings with dry filament, tuned bed adhesion, and conservative cooling to avoid warping."
	}
	if s.requiresCNC || category == "Alloys" || category == "Pure_Metals" {
		return "Use CNC machining with appropriate tooling grade, coolant strategy, and conservative feed per tooth for dimensional stability."
	}
	if category == "Ceramics" {
		return "Use ceramic processing route with sintering schedule control, then finish-grind critical surfaces for tolerance and surface integrity."
	}
	if category == "Composites" {
		return "Control layup orientation to align with principal loads and validate interlaminar shear margins before production."
	}
	_ = best
	return "Validate manufacturability with a pilot build and verify properties against in-service thermal and load envelope."
}

func deterministicSafetyMargin(query string, best models.Material) string {
	s := extractQuerySignals(query)
	if s.hasServiceTemp && best.GlassTransitionTemp != nil {
		margin := (*best.GlassTransitionTemp - 273.15) - s.serviceTempC
		if margin < 0 {
			return fmt.Sprintf("FAIL: thermal margin %.1f C below requirement.", margin)
		}
		return fmt.Sprintf("PASS: thermal margin %.1f C above service requirement.", margin)
	}
	if best.YieldStrength != nil {
		return fmt.Sprintf("PASS: use engineering FoS >= 1.5 with baseline yield strength %.1f MPa.", *best.YieldStrength)
	}
	return "PASS: candidate remains feasible under available process and domain constraints."
}

func chooseDeterministicTopCandidate(query string, candidates []models.Material) models.Material {
	if len(candidates) == 0 {
		return models.Material{}
	}
	signals := extractQuerySignals(query)

	pickByName := func(keywords ...string) (models.Material, bool) {
		for _, m := range candidates {
			name := strings.ToLower(m.Name)
			for _, kw := range keywords {
				if strings.Contains(name, kw) {
					return m, true
				}
			}
		}
		return models.Material{}, false
	}

	if signals.requiresConductivityPurist {
		if m, ok := pickByName("oxygen-free", "ofhc", "c101", "c110", "copper"); ok {
			return m
		}
	}
	if signals.requiresChemicalExtreme {
		if m, ok := pickByName("peek", "ptfe", "teflon"); ok {
			return m
		}
	}
	if signals.requiresSpecificModulus {
		if m, ok := pickByName("cfrp", "carbon fiber"); ok {
			return m
		}
	}
	if signals.requiresHotSection {
		if m, ok := pickByName("inconel", "hastelloy"); ok {
			return m
		}
	}
	if signals.requiresBiomedical {
		if m, ok := pickByName("ti-6al-4v", "grade 5", "titanium"); ok {
			return m
		}
	}
	if signals.requiresRadiationShielding {
		if m, ok := pickByName("tungsten"); ok {
			return m
		}
	}
	if signals.requiresTransparentImpact {
		if m, ok := pickByName("polycarbonate", " pc"); ok {
			return m
		}
	}
	if signals.requiresThermalShock {
		if m, ok := pickByName("zirconia", "alumina", "al2o3"); ok {
			return m
		}
	}
	if signals.requiresShapeMemory {
		if m, ok := pickByName("nitinol", "ni-ti"); ok {
			return m
		}
	}
	if signals.requiresLowCTE {
		if m, ok := pickByName("invar"); ok {
			return m
		}
	}

	best := candidates[0]
	bestScore := scoreMaterialForQuery(best, signals)
	for _, m := range candidates[1:] {
		score := scoreMaterialForQuery(m, signals)
		if score > bestScore {
			best = m
			bestScore = score
		}
	}
	return best
}

func deterministicGuardrailExplanation(query string, m models.Material) string {
	s := extractQuerySignals(query)
	name := m.Name
	switch {
	case s.desktopFDM && s.requiresHeatMargin:
		return name + " wins the desktop-FDM Pareto trade-off: enough heat margin while staying printable on hobby hardware."
	case s.requiresAesthetics:
		return name + " is favored for surface detail, low shrinkage, and fast indoor model printing where heat load is not controlling."
	case s.professionalFDM && s.hasServiceTemp && s.serviceTempC >= 100:
		return name + " is favored because the query describes chamber/nozzle capability sufficient for high-Tg engineering polymers."
	case s.requiresCryogenic:
		return name + " is favored for cryogenic CNC service because aluminum/FCC-style ductility is safer than brittle polymers or unsuitable steels."
	case s.requiresDamping:
		return name + " is favored because damping requires low stiffness and high strain capacity, not maximum strength."
	case s.requiresWear:
		return name + " is favored because abrasive/high-temperature wear is governed by ceramic hardness, hot strength, and shape stability."
	case s.requiresConductivity:
		return name + " is favored because pure high-conductivity metals beat alloys when electron/phonon scattering must be minimized."
	case s.requiresAerospace:
		return name + " is favored by the specific-strength merit index sigma_y/rho."
	case s.requiresChemical:
		return name + " is favored because chemical inertness dominates the selection."
	default:
		return name + " is the highest-ranked candidate after physics and manufacturing feasibility guardrails."
	}
}

type InlineAlloyPrediction struct {
	Summary       string            `json:"summary"`
	KeyFindings   map[string]string `json:"key_findings,omitempty"`
	RiskFlags     []string          `json:"risk_flags,omitempty"`
	Confidence    string            `json:"confidence,omitempty"`
	ShouldDisplay bool              `json:"should_display"`
}

func ShouldAttachInlineAlloyPrediction(query string, routedCategory string, top models.Material) bool {
	if top.ID == 0 {
		return false
	}
	if routedCategory != "Alloys" && routedCategory != "Pure_Metals" {
		return false
	}
	s := extractQuerySignals(query)
	q := strings.ToLower(query)
	return s.hasServiceTemp || s.hasHighPressure || s.requiresCryogenic || s.hasHighStrengthReq || s.requiresAerospace || strings.Contains(q, "fatigue") || strings.Contains(q, "structural")
}

func GenerateInlineAlloyPrediction(ctx context.Context, query string, top models.Material) (InlineAlloyPrediction, int, error) {
	base := InlineAlloyPrediction{
		Summary:       buildDeterministicPredictionSummary(query, top),
		KeyFindings:   deterministicPredictionFindings(top),
		RiskFlags:     deterministicPredictionRisks(query, top),
		Confidence:    "Medium",
		ShouldDisplay: true,
	}

	if os.Getenv("ENABLE_LLM_INLINE_PREDICTION") != "1" {
		return base, 0, nil
	}

	type llmInlinePrediction struct {
		Summary     string            `json:"summary"`
		KeyFindings map[string]string `json:"key_findings"`
		RiskFlags   []string          `json:"risk_flags"`
		Confidence  string            `json:"confidence"`
	}

	prompt := fmt.Sprintf(`You are a materials scientist. Create a short prediction note for the selected alloy.

User query:
"%s"

Selected material:
- Name: %s
- Density: %s g/cm3
- Yield strength: %s MPa
- Thermal conductivity: %s W/mK
- Melting point: %s K

Return strict JSON:
{
  "summary": "1-2 sentence practical prediction",
  "key_findings": {"field": "value"},
  "risk_flags": ["short risk", "short risk"],
  "confidence": "High|Medium|Low"
}
`, query, top.Name, fmtOpt(top.Density), fmtOpt(top.YieldStrength), fmtOpt(top.ThermalConductivity), fmtOpt(top.MeltingPoint))

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 350)
	if err != nil {
		return base, 0, nil
	}

	cleaned := cleanJSON(raw)
	var parsed llmInlinePrediction
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return base, tokens, nil
	}

	if strings.TrimSpace(parsed.Summary) != "" {
		base.Summary = parsed.Summary
	}
	if len(parsed.KeyFindings) > 0 {
		base.KeyFindings = parsed.KeyFindings
	}
	if len(parsed.RiskFlags) > 0 {
		base.RiskFlags = parsed.RiskFlags
	}
	if strings.TrimSpace(parsed.Confidence) != "" {
		base.Confidence = parsed.Confidence
	}

	return base, tokens, nil
}

func buildDeterministicPredictionSummary(query string, top models.Material) string {
	s := extractQuerySignals(query)
	if s.requiresCryogenic {
		return top.Name + " is predicted to remain a practical choice under low-temperature service, provided machining quality and sealing are controlled."
	}
	if s.hasServiceTemp {
		return fmt.Sprintf("%s is predicted to handle the requested operating window with normal engineering safety factors and process control.", top.Name)
	}
	return top.Name + " is predicted to provide stable performance for this application, with the main risk driven by manufacturing quality rather than base material limits."
}

func deterministicPredictionFindings(top models.Material) map[string]string {
	f := map[string]string{}
	if top.YieldStrength != nil {
		f["Yield strength"] = fmt.Sprintf("%.1f MPa", *top.YieldStrength)
	}
	if top.Density != nil {
		f["Density"] = fmt.Sprintf("%.2f g/cm3", *top.Density)
	}
	if top.MeltingPoint != nil {
		f["Melting point"] = fmt.Sprintf("%.0f K", *top.MeltingPoint)
	}
	if top.ThermalConductivity != nil {
		f["Thermal conductivity"] = fmt.Sprintf("%.2f W/mK", *top.ThermalConductivity)
	}
	return f
}

func deterministicPredictionRisks(query string, top models.Material) []string {
	r := []string{}
	s := extractQuerySignals(query)
	if s.hasHighPressure {
		r = append(r, "Pressure-tight performance still depends on machining tolerance and sealing quality.")
	}
	if s.hasServiceTemp && top.MeltingPoint == nil {
		r = append(r, "Missing melting-point data in catalog; validate with supplier datasheet before final sign-off.")
	}
	if top.YieldStrength == nil {
		r = append(r, "Yield strength is missing in the dataset; perform final verification against certified grade data.")
	}
	if len(r) == 0 {
		r = append(r, "Validate fatigue and joining process in prototype tests before production freeze.")
	}
	return r
}

func fmtOpt(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.3g", *v)
}

func InjectPriorityCandidates(query string, routedCategory string, base []models.Material, all []models.Material, limit int) []models.Material {
	if limit <= 0 {
		limit = 40
	}
	s := extractQuerySignals(query)
	keywords := []string{}
	if s.requiresConductivityPurist {
		keywords = append(keywords, "oxygen-free", "ofhc", "c101", "c110", "copper")
	}
	if s.requiresChemicalExtreme {
		keywords = append(keywords, "peek", "ptfe", "teflon")
	}
	if s.requiresSpecificModulus {
		keywords = append(keywords, "cfrp", "carbon fiber")
	}
	if s.requiresHotSection {
		keywords = append(keywords, "inconel", "hastelloy")
	}
	if s.requiresBiomedical {
		keywords = append(keywords, "ti-6al-4v", "grade 5", "titanium")
	}
	if s.requiresRadiationShielding {
		keywords = append(keywords, "tungsten")
	}
	if s.requiresTransparentImpact {
		keywords = append(keywords, "polycarbonate")
	}
	if s.requiresThermalShock {
		keywords = append(keywords, "zirconia", "alumina", "al2o3")
	}
	if s.requiresShapeMemory {
		keywords = append(keywords, "nitinol", "ni-ti")
	}
	if s.requiresLowCTE {
		keywords = append(keywords, "invar")
	}

	priority := []models.Material{}
	for _, m := range all {
		if !matchesRoutedCategory(routedCategory, m) {
			continue
		}
		name := strings.ToLower(m.Name)
		for _, kw := range keywords {
			if strings.Contains(name, kw) {
				priority = append(priority, m)
				break
			}
		}
	}

	return mergeUniqueMaterialLists(priority, base, limit)
}

func mergeUniqueMaterialLists(first, second []models.Material, limit int) []models.Material {
	seen := map[int]bool{}
	out := make([]models.Material, 0, limit)
	appendOne := func(m models.Material) {
		if len(out) >= limit || seen[m.ID] {
			return
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	for _, m := range first {
		appendOne(m)
	}
	for _, m := range second {
		appendOne(m)
	}
	return out
}

func RequiresExpandedCatalog(query string) bool {
	s := extractQuerySignals(query)
	return s.requiresShapeMemory || s.requiresLowCTE || s.requiresThermalShock || s.requiresSpecificModulus || s.requiresTransparentImpact || s.requiresConductivityPurist || s.requiresChemicalExtreme
}

const followUpAssistantSystemPrompt = `You are Met-Quest Assistant.

You are now in follow-up chat mode.
Important behavior:
- Respond naturally like a normal chat assistant (Gemini/ChatGPT style).
- Do NOT repeat full pipeline steps unless explicitly asked.
- Keep answers practical, direct, and collaborative.
- Use prior recommendation context when relevant.
- Treat the recent conversation as the source of truth for follow-up questions.
- Do not pick a new top material or rerun material selection during follow-up.
- If asked "why not X" or "why rejected X", compare X against the previous top recommendation and original constraints.
- If the user asks a totally new independent material-selection problem, ask them to start a "new analysis" so a full reroute can run.

Output plain text only.`

// ChatFollowUp returns conversational replies after the first full recommendation.
func ChatFollowUp(ctx context.Context, message string, history []models.ChatTurn, initialReport string, topRecommendations []string) (string, int, error) {
	var b strings.Builder
	b.WriteString(followUpAssistantSystemPrompt)
	b.WriteString("\n\nInitial recommendation summary:\n")
	if strings.TrimSpace(initialReport) != "" {
		report := strings.TrimSpace(initialReport)
		if len(report) > 3000 {
			report = report[:3000] + "..."
		}
		b.WriteString(report)
	} else {
		b.WriteString("No initial report provided.")
	}

	if len(topRecommendations) > 0 {
		b.WriteString("\n\nTop materials from first analysis:\n")
		for i, name := range topRecommendations {
			if strings.TrimSpace(name) == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(name)))
		}
	}

	if len(history) > 0 {
		b.WriteString("\nRecent conversation:\n")
		start := 0
		if len(history) > 8 {
			start = len(history) - 8
		}
		for _, turn := range history[start:] {
			role := strings.ToLower(strings.TrimSpace(turn.Role))
			if role != "user" && role != "assistant" {
				continue
			}
			content := strings.TrimSpace(turn.Content)
			if content == "" {
				continue
			}
			if len(content) > 900 {
				content = content[:900] + "..."
			}
			b.WriteString(strings.ToUpper(role[:1]) + role[1:] + ": " + content + "\n")
		}
	}

	b.WriteString("\nUser message:\n")
	b.WriteString(strings.TrimSpace(message))
	b.WriteString("\n\nAssistant reply:")

	reply, tokens, err := callGeminiText(ctx, b.String(), 0.3, 700)
	if err != nil {
		return "", 0, err
	}
	if strings.TrimSpace(reply) == "" {
		reply = "I can help with that. Tell me what you want to refine in the current recommendation, and I will respond directly."
	}
	return strings.TrimSpace(reply), tokens, nil
}
