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

	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second) // covers 120s client timeout
	defer cancel()

	// ── Step 1: Load FULL database into context ─────────────────────────────
	allMaterials := services.GetAllMaterials()
	if len(allMaterials) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Materials catalog is empty"})
		return
	}

	// ── Step 2 (NEW): Long-Context Native Analysis ──────────────────────────
	// We pass the explicit user Domain selection to segregate the dataset and prevent token explosions
	llmResponse, totalTokens, err := services.LongContextAnalyze(ctx, req.Query, req.Domain, allMaterials)
	if err != nil {
		log.Printf("ERROR: LongContextAnalyze: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI analysis failed: " + err.Error()})
		return
	}

	// ── Step 3: Map recommended IDs back to Material Structs ────────────────
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
		ExtractedIntent: models.IntentJSON{}, // Blank, bypassed
		Recommendations: recommendations,
		Report:          llmResponse.Report,
		TokensUsed:      totalTokens,
	}

	c.JSON(http.StatusOK, resp)
}
