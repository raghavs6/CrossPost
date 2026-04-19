package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/joho/godotenv"
	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/db"
	"github.com/raghavs6/CrossPost/internal/handler"
	"github.com/raghavs6/CrossPost/internal/middleware"
)

func main() {
	// Load .env in development. In production (ECS), env vars are already set.
	if err := godotenv.Overload(); err != nil {
		log.Println("No .env file found, reading config from environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Store the DB reference — we need to pass it to handlers that need DB access.
	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Database connected and migrations applied")

	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", cfg.FrontendURL},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public routes — no authentication required.
	r.Get("/health", handler.HealthHandler)

	authHandler := handler.NewAuthHandler(cfg, database)
	r.Get("/api/auth/google", authHandler.GoogleLogin)
	r.Get("/api/auth/google/callback", authHandler.GoogleCallback)

	postHandler := handler.NewPostHandler(database)

	// Twitter auth handler is always constructed (ListConnections works without
	// OAuth configured), but OAuth routes are only mounted when all three
	// TWITTER_* env vars are present.
	twitterAuthHandler := handler.NewTwitterAuthHandler(cfg, database)
	if cfg.TwitterEnabled() {
		// Public callback — Twitter redirects here after user consent.
		r.Get("/api/auth/twitter/callback", twitterAuthHandler.TwitterCallback)
	}

	// Protected routes — every request must carry a valid JWT.
	// All routes inside this group are checked by RequireAuth before reaching
	// their handler.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(cfg.JWTSecret))
		r.Post("/api/posts", postHandler.Create)
		r.Get("/api/posts", postHandler.List)
		r.Get("/api/posts/{id}", postHandler.GetByID)
		r.Put("/api/posts/{id}", postHandler.Update)
		r.Delete("/api/posts/{id}", postHandler.Delete)

		// Social account connections — always available so the dashboard can
		// show connected platforms (returns [] when none are linked).
		r.Get("/api/connections", twitterAuthHandler.ListConnections)
		if cfg.TwitterEnabled() {
			r.Get("/api/auth/twitter", twitterAuthHandler.TwitterLogin)
		}
	})

	addr := ":" + cfg.ServerPort
	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
