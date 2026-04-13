package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vivek/met-quest/models"
	"github.com/vivek/met-quest/services"
)

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
