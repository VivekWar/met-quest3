package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vivek/met-quest/models"
	"github.com/vivek/met-quest/services"
)

// Predict handles POST /predict
// Flow: composition → DB element lookup (Phase 1) → Gemini thermodynamic refinement (Phase 2)
func Predict(c *gin.Context) {
	var req models.PredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request: " + err.Error(),
		})
		return
	}

	if len(req.Composition) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "composition must contain at least one element",
		})
		return
	}

	if len(req.Composition) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "composition may contain at most 10 elements",
		})
		return
	}

	ctx := c.Request.Context()

	log.Printf("Predict request: %v", req.Composition)

	resp, err := services.PredictAlloyProperties(ctx, req.Composition)
	if err != nil {
		log.Printf("ERROR: PredictAlloyProperties: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}
