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
		Temperature     float64 `json:"temperature"`
		MaxOutputTokens int     `json:"maxOutputTokens"`
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

func callGemini(ctx context.Context, prompt string, temperature float64, maxTokens int) (string, int, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" || strings.Contains(apiKey, "Dummy") {
		log.Println("⚠️  API_KEY is missing/dummy. Using MOCK AI response.")

		// Very basic heuristics for demo
		if strings.Contains(prompt, "Virtual Materials Scientist") {
			mockJSON := `{
  "recommended_ids": [1, 2, 3],
  "report": "## 🏆 Recommendation\n\n**Titanium Alloy Ti-6Al-4V** is the top recommendation for this application.\n\n## 📊 Properties Retrieved\n| Property | Ti-6Al-4V | Unit |\n|---|---|---|\n| Density | 4.43 | g/cm³ |\n\n## 🔬 Scientific Explanation\nTitanium alloys offer excellent solid-solution strengthening.\n\n## ⚠️ Engineering Trade-offs\n- High machining cost\n- Susceptible to galling\n\n*(Note: This is a demo mode AI response)*"
}`
			return mockJSON, 100, nil
		} else if strings.Contains(prompt, "Category") || strings.Contains(prompt, "filters") {
			return `{"category": "Metal", "filters": {}}`, 20, nil
		} else {
			return `{"refined_properties": {"density": 6.5, "yield_strength": 600}, "scientific_explanation": "Demo Prediction", "phase_diagram_notes": "Multiphase demo", "confidence": "Medium"}`, 80, nil
		}
	}

	// Detect Provider: Google AI Studio keys start with "AIza"
	if strings.HasPrefix(apiKey, "AIza") {
		return callGoogleAI(ctx, apiKey, prompt, temperature, maxTokens)
	}

	// Default: OpenRouter (OpenAI-compatible)
	payload := openRouterRequest{
		Model: "google/gemini-2.0-flash-exp",
		Messages: []openRouterMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, fmt.Errorf("marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterBaseURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "http://localhost:5173")
	req.Header.Set("X-Title", "Smart Alloy Selector")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("openrouter HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result openRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("decode error: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", 0, fmt.Errorf("empty openrouter response")
	}

	text := result.Choices[0].Message.Content
	tokens := result.Usage.TotalTokens
	return text, tokens, nil
}

// ──────────────────────────────────────────────────────────────────────────
//  1. Intent Extraction
//
// ──────────────────────────────────────────────────────────────────────────
const intentSystemPrompt = `You are a Virtual Materials Scientist.

Your job is to extract structured constraints from a user query.

IMPORTANT:
- Return ONLY valid JSON
- NO explanation
- NO markdown
- NO extra text

---

LOGIC:

1. Infer material class (Metal, Polymer, Ceramic, Composite)
2. Based on class, extract ONLY relevant properties:

- Polymer → Tg, density, strength
- Metal → yield_strength, melting_point, density
- Ceramic → melting_point, hardness
- Composite → strength, density

3. If temperature is given:
- For polymers → map to Tg constraint
- For metals/ceramics → map to melting_point

---

OUTPUT FORMAT:

{
  "filters": {
    "<property>": {"min": number|null, "max": number|null}
  },
  "category": "<Metal|Polymer|Ceramic|Composite|null>",
  "sort_by": "<property|null>",
  "sort_dir": "<ASC|DESC>"
}

Return ONLY JSON.`

// ExtractIntent parses a natural language query into structured filters.
func ExtractIntent(ctx context.Context, query string) (models.IntentJSON, int, error) {
	prompt := intentSystemPrompt + "\n\nQuery: " + query

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 512)
	if err != nil {
		return models.IntentJSON{}, 0, fmt.Errorf("intent extraction LLM call: %w", err)
	}

	// Strip markdown fences if present
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var intent models.IntentJSON
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		log.Printf("WARN: Failed to parse intent JSON: %v\nRaw: %s", err, raw)
		// Return empty intent — search will fall back to a general query
		return models.IntentJSON{}, tokens, nil
	}

	log.Printf("Intent extracted: category=%q sort_by=%q filters=%d", intent.Category, intent.SortBy, len(intent.Filters))
	return intent, tokens, nil
}

// ──────────────────────────────────────────────────────────────────────────
//  Long-Context AI Engine (Replaces RAG Intent Extraction filter)
// ──────────────────────────────────────────────────────────────────────────

const longContextSystemPrompt = `You are a Virtual Materials Scientist.

Select top 3 materials from given catalog.

IMPORTANT:
- Follow engineering reasoning internally
- Return ONLY valid JSON
- NO explanation outside JSON

---

RULES:

1. Identify material class(es)
2. Evaluate ONLY relevant properties:

- Polymer → Tg, density
- Metal → strength, melting_point
- Ceramic → temperature resistance, brittleness
- Composite → strength-to-weight

3. Do NOT apply irrelevant properties
4. Reject materials violating constraints
5. Consider manufacturability and trade-offs

---

OUTPUT:

{
  "recommended_ids": [1,2,3],
  "report": "## 🏆 Recommendation\n..."
}

Ensure valid JSON. Escape all quotes inside report.`

type LongContextLLMResponse struct {
	RecommendedIDs []int  `json:"recommended_ids"`
	Report         string `json:"report"`
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

	// Safety cap: even if domain is large, cap to 4000
	if len(filtered) > 4000 {
		return filtered[:4000]
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

	raw, tokens, err := callGemini(ctx, prompt, 0.1, 2000)
	if err != nil {
		return LongContextLLMResponse{}, 0, fmt.Errorf("long-context LLM call: %w", err)
	}

	// Extremely robust JSON block extraction
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start != -1 && end != -1 && end > start {
		raw = raw[start : end+1]
	} else {
		log.Printf("WARN: No JSON braces found in raw LLM output: %s", raw)
	}

	var parsed LongContextLLMResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		log.Printf("WARN: LongContext JSON Parse failed: %v\nRaw: %s", err, raw)
		return LongContextLLMResponse{Report: "LLM responded with invalid JSON format. See logs."}, tokens, nil
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

	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var out PredictorLLMOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return PredictorLLMOutput{}, tokens, fmt.Errorf("failed to parse predictor LLM output: %w\nRaw:\n%s", err, raw)
	}

	return out, tokens, nil
}

// callGoogleAI handles direct calls to Google's Generative Language API
func callGoogleAI(ctx context.Context, apiKey string, prompt string, temperature float64, maxTokens int) (string, int, error) {
	baseURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent"
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
			Temperature     float64 `json:"temperature"`
			MaxOutputTokens int     `json:"maxOutputTokens"`
		}{
			Temperature:     temperature,
			MaxOutputTokens: maxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("google ai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("google ai HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result googleAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("google ai decode error: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", 0, fmt.Errorf("empty google ai response")
	}

	return result.Candidates[0].Content.Parts[0].Text, result.UsageMetadata.TotalTokenCount, nil
}
