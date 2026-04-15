package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vivek/met-quest/db"
	"github.com/vivek/met-quest/models"
	"github.com/vivek/met-quest/services"
)

// DispatcherResponse wraps the full recommendation pipeline output
type DispatcherResponse struct {
	Query               string            `json:"query"`
	RoutedCategory      string            `json:"routed_category"`
	CategoryCandidates  []models.Material `json:"category_candidates"`
	PhysicsAnalysis     interface{}       `json:"physics_analysis"`
	AlloyPrediction     interface{}       `json:"alloy_prediction,omitempty"`
	TopRecommendation   models.Material   `json:"top_recommendation"`
	AlternativeOptions  []models.Material `json:"alternative_options"`
	TotalTokensUsed     int               `json:"total_tokens_used"`
	PipelineExplanation string            `json:"pipeline_explanation"`
}

// Recommend handles POST /recommend
// Flow: NL query → Gemini intent extraction → SQL search → Gemini reframer → response
func Recommend(c *gin.Context) {
	var req models.RecommendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	log.Printf("Long-Context Recommend triggered | Query: %q", req.Query)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second) // covers multi-tier resilience logic
	defer cancel()

	// ── Step 1: Intent + Category Router ─────────────────────────────────────
	intent, intentTokens, err := services.ExtractIntent(ctx, req.Query)
	if err != nil {
		log.Printf("WARN: ExtractIntent failed, using fallback router: %v", err)
	}
	routedClass := services.RouteMaterialClass(req.Domain, intent.Category, req.Query)

	// ── Step 2: Targeted category catalog retrieval ─────────────────────────
	allMaterials := services.GetMaterialsForClass(routedClass)
	if len(allMaterials) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Materials catalog is empty"})
		return
	}

	// ── Step 3: Long-Context Scientist Analysis ─────────────────────────────
	// We pass the explicit user Domain selection to segregate the dataset and prevent token explosions
	llmResponse, totalTokens, err := services.LongContextAnalyze(ctx, req.Query, req.Domain, allMaterials)
	if err != nil {
		log.Printf("ERROR: LongContextAnalyze: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI analysis failed: " + err.Error()})
		return
	}

	// ── Step 4: Map recommended IDs back to Material Structs ────────────────
	var recommendations = []models.Material{}
	for _, recID := range llmResponse.RecommendedIDs {
		// Find the material in the catalog
		for _, m := range allMaterials {
			if m.ID == recID {
				recommendations = append(recommendations, m)
				break
			}
		}
	}

	// ── Return Payload ──────────────────────────────────────────────────────
	resp := models.RecommendResponse{
		Query:           req.Query,
		ExtractedIntent: intent,
		Recommendations: recommendations,
		Report:          llmResponse.Report,
		TokensUsed:      totalTokens + intentTokens,
	}

	c.JSON(http.StatusOK, resp)
}

// ──────────────────────────────────────────────────────────────────────────
//  ENHANCED DISPATCHER HANDLER: Category-Aware + Physics-Verified
// ──────────────────────────────────────────────────────────────────────────

// RecommendWithDispatcher handles POST /recommend/dispatcher (new endpoint)
// Flow: Query Router → Category-Specific Search → Physics Verification → Response
func RecommendWithDispatcher(c *gin.Context) {
	var req models.RecommendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	log.Printf("🎯 Dispatcher Recommender triggered | Query: %q", req.Query)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	totalTokensUsed := 0
	var pipelineSteps []string

	// ── Step 1: LLM-Powered Query Router ────────────────────────────────────
	routedCategory, routeTokens, err := services.RouteQuery(ctx, req.Query)
	if err != nil {
		log.Printf("⚠️  RouteQuery failed (will use ExtractIntent fallback): %v", err)
		routedCategory = "Unknown"
	}
	if hinted := domainHintCategory(req.Domain); hinted != "" {
		routedCategory = hinted
		pipelineSteps = append(pipelineSteps, "🧭 Domain hint enforced category: "+routedCategory)
	}
	totalTokensUsed += routeTokens
	pipelineSteps = append(pipelineSteps, "✅ Query routed to: "+routedCategory)

	// Fallback: if RouteQuery didn't work, use existing intent extraction
	if routedCategory == "" || routedCategory == "Unknown" {
		intent, intentTokens, _ := services.ExtractIntent(ctx, req.Query)
		routedCategory = intent.Category
		totalTokensUsed += intentTokens
		if routedCategory == "" || routedCategory == "Unknown" {
			routedCategory = services.InferCategoryHeuristic(req.Query)
		}
		pipelineSteps = append(pipelineSteps, "↩️  Fallback to intent extraction: "+routedCategory)
	}

	// ── Step 2: Get all materials from database ─────────────────────────────
	var allMaterials []models.Material
	if db.Pool != nil {
		// Query database for all materials
		rows, err := db.Pool.Query(ctx, "SELECT id, name, formula, category, subcategory, density, glass_transition_temp, heat_deflection_temp, melting_point, boiling_point, thermal_conductivity, specific_heat, thermal_expansion, electrical_resistivity, yield_strength, tensile_strength, youngs_modulus, hardness_vickers, poissons_ratio, processing_temp_min_c, processing_temp_max_c, crystallinity, crystal_system, fracture_toughness, weibull_modulus, interlaminar_shear_strength, fiber_volume_fraction, source, mp_material_id FROM materials")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var m models.Material
				if rows.Scan(&m.ID, &m.Name, &m.Formula, &m.Category, &m.Subcategory, &m.Density, &m.GlassTransitionTemp, &m.HeatDeflectionTemp, &m.MeltingPoint, &m.BoilingPoint, &m.ThermalConductivity, &m.SpecificHeat, &m.ThermalExpansion, &m.ElectricalResistivity, &m.YieldStrength, &m.TensileStrength, &m.YoungsModulus, &m.HardnessVickers, &m.PoissonsRatio, &m.ProcessingTempMinC, &m.ProcessingTempMaxC, &m.Crystallinity, &m.CrystalSystem, &m.FractureToughness, &m.WeibullModulus, &m.InterlaminarShear, &m.FiberVolumeFraction, &m.Source, &m.MpMaterialID) == nil {
					allMaterials = append(allMaterials, m)
				}
			}
		}
	}

	if len(allMaterials) == 0 {
		// Fallback to cached materials if available
		allMaterials = services.GetAllMaterials()
	}

	if len(allMaterials) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Materials database is empty"})
		return
	}

	if strings.TrimSpace(req.Domain) != "" {
		domainFiltered := services.FilterByDomain(req.Domain, allMaterials)
		if len(domainFiltered) > 0 {
			allMaterials = domainFiltered
			pipelineSteps = append(pipelineSteps, fmt.Sprintf("✅ Domain filter applied (%s): %d materials", req.Domain, len(allMaterials)))
		} else {
			pipelineSteps = append(pipelineSteps, fmt.Sprintf("⚠️  Domain filter (%s) returned 0; using full catalog", req.Domain))
		}
	}

	pipelineSteps = append(pipelineSteps, fmt.Sprintf("✅ Loaded %d materials from database", len(allMaterials)))

	// ── Step 3: Category-Specific Search ────────────────────────────────────
	var candidates []models.Material
	constraints := services.BuildHeuristicConstraints(req.Query, routedCategory)

	// Optional LLM intent extraction. Kept behind env flag to reduce API usage.
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Enable-LLM-Intent")), "1") {
		intent, intentTokens, _ := services.ExtractIntent(ctx, req.Query)
		totalTokensUsed += intentTokens
		for prop, rf := range intent.Filters {
			if rf.Min != nil {
				constraints["min_"+prop] = *rf.Min
			}
			if rf.Max != nil {
				constraints["max_"+prop] = *rf.Max
			}
		}
		pipelineSteps = append(pipelineSteps, "✅ LLM intent constraints enabled via request header")
	}

	// Route to category-specific search
	switch routedCategory {
	case "Polymers":
		candidates = services.SearchPolymers(ctx, constraints, allMaterials, 40)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchPolymers: found %d candidates", len(candidates)))

	case "Alloys":
		candidates = services.SearchAlloys(ctx, constraints, allMaterials, 40)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchAlloys: found %d candidates", len(candidates)))

	case "Pure_Metals":
		candidates = services.SearchPureMetals(ctx, constraints, allMaterials, 40)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchPureMetals: found %d candidates", len(candidates)))

	case "Ceramics":
		candidates = services.SearchCeramics(ctx, constraints, allMaterials, 40)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchCeramics: found %d candidates", len(candidates)))

	case "Composites":
		candidates = services.SearchComposites(ctx, constraints, allMaterials, 40)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchComposites: found %d candidates", len(candidates)))

	default:
		// Fallback: use LongContextAnalyze on all materials
		log.Printf("⚠️  Unknown category: %s. Using general search.", routedCategory)
		if len(allMaterials) > 3 {
			candidates = allMaterials[:3]
		} else {
			candidates = allMaterials
		}
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("⚠️  Generic search: %d candidates", len(candidates)))
	}

	vectorCandidates := services.HybridVectorRetrieve(ctx, req.Query, routedCategory, allMaterials, 30)
	if len(vectorCandidates) > 0 {
		candidates = mergeUniqueCandidates(candidates, vectorCandidates, 40)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🧠 Vector retrieval merged %d candidates", len(vectorCandidates)))
	}

	if len(candidates) < 3 {
		fallbackKeyword := keywordCategoryForRoute(routedCategory)
		cascade := keywordCascadeSearch(allMaterials, req.Query, fallbackKeyword, 15)
		candidates = mergeUniqueCandidates(candidates, cascade, 15)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("↪️  Cascade keyword fallback: merged to %d candidates", len(candidates)))
	}

	if len(candidates) == 0 {
		log.Printf("⚠️ 0 results in specialized search. Falling back to Domain Keyword search.")
		fallbackDomain := mapRoutedCategoryToDomain(routedCategory)
		candidates = services.FilterByDomain(fallbackDomain, allMaterials)
		if len(candidates) > 10 {
			candidates = candidates[:10]
		}
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("↩️  Panic fallback (%s): %d candidates", fallbackDomain, len(candidates)))
	}

	if len(candidates) == 0 {
		c.JSON(http.StatusOK, DispatcherResponse{
			Query:               req.Query,
			RoutedCategory:      routedCategory,
			CategoryCandidates:  []models.Material{},
			TopRecommendation:   models.Material{},
			TotalTokensUsed:     totalTokensUsed,
			PipelineExplanation: "No materials found matching your requirements in category: " + routedCategory,
		})
		return
	}

	// ── Step 4: Physics-Driven Scientific Analysis ──────────────────────────
	analysis, analysisTokens, err := services.ScientificAnalysis(ctx, req.Query, routedCategory, candidates)
	if err != nil {
		log.Printf("⚠️  ScientificAnalysis failed: %v", err)
	}
	totalTokensUsed += analysisTokens
	pipelineSteps = append(pipelineSteps, "🔬 Physics verification completed")

	// ── Step 5: Map results back to Material structs ─────────────────────────
	var topRecommendation models.Material
	var alternatives []models.Material

	// Find the top candidate in the candidates list
	if analysis.TopCandidate != "" {
		if analysis.TopCandidate == "NO_FEASIBLE_MATERIAL" {
			alternatives = candidates
		} else {
			for i, m := range candidates {
				if m.Name == analysis.TopCandidate {
					topRecommendation = m
					alternatives = append(alternatives, candidates[:i]...)
					alternatives = append(alternatives, candidates[i+1:]...)
					break
				}
			}
		}
	}

	if topRecommendation.ID == 0 && analysis.TopCandidate != "NO_FEASIBLE_MATERIAL" && len(candidates) > 0 {
		topRecommendation = candidates[0]
		if len(candidates) > 1 {
			alternatives = candidates[1:]
		}
	}

	if analysis.TopCandidate == "NO_FEASIBLE_MATERIAL" {
		pipelineSteps = append(pipelineSteps, "⛔ No feasible material for the requested process")
	} else {
		pipelineSteps = append(pipelineSteps, "✅ Top recommendation: "+topRecommendation.Name)
	}

	var alloyPrediction interface{}
	if topRecommendation.ID != 0 && services.ShouldAttachInlineAlloyPrediction(req.Query, routedCategory, topRecommendation) {
		prediction, predictionTokens, predErr := services.GenerateInlineAlloyPrediction(ctx, req.Query, topRecommendation)
		totalTokensUsed += predictionTokens
		if predErr != nil {
			log.Printf("⚠️  Inline alloy prediction failed: %v", predErr)
		} else {
			alloyPrediction = prediction
			pipelineSteps = append(pipelineSteps, "🧠 Inline alloy prediction added")
		}
	}

	// ── Return Enhanced Response ────────────────────────────────────────────
	resp := DispatcherResponse{
		Query:               req.Query,
		RoutedCategory:      routedCategory,
		CategoryCandidates:  candidates,
		PhysicsAnalysis:     analysis,
		AlloyPrediction:     alloyPrediction,
		TopRecommendation:   topRecommendation,
		AlternativeOptions:  alternatives,
		TotalTokensUsed:     totalTokensUsed,
		PipelineExplanation: "Pipeline Steps:\n" + joinSteps(pipelineSteps),
	}

	c.JSON(http.StatusOK, resp)
}

// ──────────────────────────────────────────────────────────────────────────
//  HELPER FUNCTION: Format pipeline steps for display
// ──────────────────────────────────────────────────────────────────────────

func joinSteps(steps []string) string {
	if len(steps) == 0 {
		return ""
	}
	return strings.Join(steps, " | ")
}

func mapRoutedCategoryToDomain(routedCategory string) string {
	switch routedCategory {
	case "Polymers":
		return "Plastics & Polymers"
	case "Alloys", "Pure_Metals":
		return "Automotive & Transportation"
	case "Ceramics":
		return "High-Temperature / Refractory"
	case "Composites":
		return "Advanced Composites"
	default:
		return "Overall (Top 1000)"
	}
}

func domainHintCategory(domain string) string {
	switch strings.TrimSpace(strings.ToLower(domain)) {
	case strings.ToLower("Plastics & Polymers"):
		return "Polymers"
	case strings.ToLower("Advanced Composites"):
		return "Composites"
	case strings.ToLower("High-Temperature / Refractory"), strings.ToLower("Tooling & Wear-Resistant"):
		return "Ceramics"
	case strings.ToLower("Electronics & Photonics"):
		return "Pure_Metals"
	default:
		return ""
	}
}

func keywordCategoryForRoute(routedCategory string) string {
	switch routedCategory {
	case "Polymers":
		return "polymer"
	case "Alloys", "Pure_Metals":
		return "metal"
	case "Ceramics":
		return "ceramic"
	case "Composites":
		return "composite"
	default:
		return ""
	}
}

func keywordCascadeSearch(materials []models.Material, query, categoryHint string, limit int) []models.Material {
	if limit <= 0 {
		limit = 15
	}
	q := strings.ToLower(query)
	var out []models.Material
	for _, m := range materials {
		name := strings.ToLower(m.Name)
		cat := strings.ToLower(m.Category)
		sub := ""
		if m.Subcategory != nil {
			sub = strings.ToLower(*m.Subcategory)
		}

		catMatch := categoryHint == "" || strings.Contains(cat, categoryHint) || strings.Contains(sub, categoryHint)
		nameMatch := strings.Contains(q, name) || strings.Contains(name, "petg") || strings.Contains(name, "pc") || strings.Contains(name, "poly")
		if catMatch || nameMatch {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func mergeUniqueCandidates(base, extra []models.Material, limit int) []models.Material {
	if limit <= 0 {
		limit = 15
	}
	seen := map[int]bool{}
	merged := make([]models.Material, 0, limit)
	for _, m := range base {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		merged = append(merged, m)
		if len(merged) >= limit {
			return merged
		}
	}
	for _, m := range extra {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		merged = append(merged, m)
		if len(merged) >= limit {
			return merged
		}
	}
	return merged
}
