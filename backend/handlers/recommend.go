package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

	pipelineSteps = append(pipelineSteps, fmt.Sprintf("✅ Loaded %d materials from database", len(allMaterials)))

	// ── Step 3: Category-Specific Search ────────────────────────────────────
	var candidates []models.Material
	constraints := make(map[string]interface{})

	// Extract constraints from the query using the existing intent extraction
	intent, intentTokens, _ := services.ExtractIntent(ctx, req.Query)
	totalTokensUsed += intentTokens

	// Convert intent filters to constraints
	for prop, rf := range intent.Filters {
		if rf.Min != nil {
			constraints["min_"+prop] = *rf.Min
		}
		if rf.Max != nil {
			constraints["max_"+prop] = *rf.Max
		}
	}

	// Route to category-specific search
	switch routedCategory {
	case "Polymers":
		candidates = services.SearchPolymers(ctx, constraints, allMaterials, 3)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchPolymers: found %d candidates", len(candidates)))

	case "Alloys":
		candidates = services.SearchAlloys(ctx, constraints, allMaterials, 3)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchAlloys: found %d candidates", len(candidates)))

	case "Pure_Metals":
		candidates = services.SearchPureMetals(ctx, constraints, allMaterials, 3)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchPureMetals: found %d candidates", len(candidates)))

	case "Ceramics":
		candidates = services.SearchCeramics(ctx, constraints, allMaterials, 3)
		pipelineSteps = append(pipelineSteps, fmt.Sprintf("🔍 SearchCeramics: found %d candidates", len(candidates)))

	case "Composites":
		candidates = services.SearchComposites(ctx, constraints, allMaterials, 3)
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
		for i, m := range candidates {
			if m.Name == analysis.TopCandidate || (i == 0 && topRecommendation.ID == 0) {
				topRecommendation = m
				alternatives = candidates[1:]
				break
			}
		}
	}

	if topRecommendation.ID == 0 && len(candidates) > 0 {
		topRecommendation = candidates[0]
		if len(candidates) > 1 {
			alternatives = candidates[1:]
		}
	}

	pipelineSteps = append(pipelineSteps, "✅ Top recommendation: "+topRecommendation.Name)

	// ── Return Enhanced Response ────────────────────────────────────────────
	resp := DispatcherResponse{
		Query:               req.Query,
		RoutedCategory:      routedCategory,
		CategoryCandidates:  candidates,
		PhysicsAnalysis:     analysis,
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
	result := ""
	for i, step := range steps {
		if i > 0 {
			result += "\n"
		}
		result += step
	}
	return result
}
