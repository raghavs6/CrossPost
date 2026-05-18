package main

import (
	"log"

	"github.com/hibiken/asynq"
	"github.com/joho/godotenv"
	"github.com/raghavs6/CrossPost/internal/config"
	"github.com/raghavs6/CrossPost/internal/db"
	"github.com/raghavs6/CrossPost/internal/publisher"
	"github.com/raghavs6/CrossPost/internal/queue"
)

func main() {
	if err := godotenv.Overload(); err != nil {
		log.Println("No .env file found, reading config from environment")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Database connected and migrations applied")

	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr()},
		asynq.Config{Concurrency: 5},
	)

	mux := asynq.NewServeMux()
	processor := queue.NewPublishProcessor(database, publisher.New(database))
	processor.Register(mux)

	log.Println("Worker starting")
	if err := server.Run(mux); err != nil {
		log.Fatalf("Worker stopped: %v", err)
	}
}
