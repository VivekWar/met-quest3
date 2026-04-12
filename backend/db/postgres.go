package db

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the shared global connection pool.
var Pool *pgxpool.Pool

// Connect initialises the pgxpool using DATABASE_URL from environment.
func Connect() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" || strings.Contains(dsn, "dummy") {
		log.Println("⚠️  DATABASE_URL missing or dummy. Running in MOCK MODE without Postgres.")
		Pool = nil
		return nil
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Printf("⚠️  Invalid DATABASE_URL: %v. Running in MOCK MODE.", err)
		return nil
	}

	// Pool settings
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Printf("⚠️  Failed to create pg connection pool: %v. Running in MOCK MODE.", err)
		return nil
	}

	// Verify connectivity (Increased timeout for Neon cloud cold-start)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		log.Printf("⚠️  Database ping failed: %v. Running in MOCK MODE.", err)
		return nil
	}

	Pool = pool
	log.Println("✅  Connected to PostgreSQL (pgxpool)")
	return nil
}

// Close gracefully shuts down the pool.
func Close() {
	if Pool != nil {
		Pool.Close()
		log.Println("PostgreSQL pool closed")
	}
}
