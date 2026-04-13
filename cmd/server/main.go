package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/db"
	"github.com/raghavs6/CrossPost/internal/handler"
)

func main() {
	// Load .env in development. In production (ECS), env vars are already set.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading config from environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	_, err = db.Connect(cfg)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Database connected and migrations applied")

	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", os.Getenv("FRONTEND_URL")},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", handler.HealthHandler)

	addr := ":" + cfg.ServerPort
	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
