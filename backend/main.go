package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/vivek/met-quest/db"
	"github.com/vivek/met-quest/handlers"
	"github.com/vivek/met-quest/services"
)

func main() {
	// ── Load environment variables ─────────────────────────────────────
	if err := godotenv.Load("../.env"); err != nil {
		log.Println("No ../.env file found — using system environment variables")
	}

	// ── Connect to PostgreSQL ──────────────────────────────────────────
	if err := db.Connect(); err != nil {
		log.Printf("DB connection error: %v", err)
	}

	// ── Load Material Catalog ──────────────────────────────────────────
	// Always load CSV into memory as it acts as the high-speed catalog for the AI
	if err := services.LoadCSVDB(); err != nil {
		log.Printf("⚠️  CSV Loader warning: %v", err)
		// If Postgres is also missing, then we fatal
		if db.Pool == nil {
			log.Fatalf("Fatal: No database available (Postgres or CSV)")
		}
	}
	defer db.Close()

	// ── Set Gin mode ───────────────────────────────────────────────────
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.DebugMode)
	}

	// ── Create router ──────────────────────────────────────────────────
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// CORS middleware
	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	r.Use(corsMiddleware(allowedOrigins))

	// ── Routes ─────────────────────────────────────────────────────────
	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "Smart Alloy Selector API",
			"version": "1.0.0",
		})
	})

	// API v1
	v1 := r.Group("/api/v1")
	{
		// POST /api/v1/recommend — NL query → top 3 material recommendations
		v1.POST("/recommend", handlers.Recommend)

		// POST /api/v1/predict — custom alloy composition → LLM-enhanced prediction
		v1.POST("/predict", handlers.Predict)
	}

	// ── Start server ───────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀  Smart Alloy Selector backend starting on port %s", port)
	log.Printf("    POST /api/v1/recommend  — Material recommendation")
	log.Printf("    POST /api/v1/predict    — Custom alloy prediction")
	log.Printf("    GET  /health            — Liveness check")

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────
//  CORS middleware
// ─────────────────────────────────────────────────────────────────────────

func corsMiddleware(allowedOrigins string) gin.HandlerFunc {
	origins := map[string]bool{}
	for _, o := range strings.Split(allowedOrigins, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			origins[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// Allow if in whitelist, or allow all in debug mode
		if origins[origin] || gin.Mode() == gin.DebugMode || len(origins) == 0 {
			c.Header("Access-Control-Allow-Origin", origin)
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
