package main

import (
	// "os"
	"context"
	"log"
	"time"
	"github.com/joho/godotenv"
	"github.com/gokatarajesh/quiz-platform/internal/app"
	"github.com/gokatarajesh/quiz-platform/internal/config"
)

func main() {
	
	err := godotenv.Load("configs/.env")
	if err != nil {
		log.Fatalf("failed to load .env file: %v", err)
	}
	// log.Println("GRACEFUL_SHUTDOWN_SECONDS:", os.Getenv("GRACEFUL_SHUTDOWN_SECONDS"))
	// log.Println("QUESTION_FETCH_TIMEOUT_SECONDS:", os.Getenv("QUESTION_FETCH_TIMEOUT_SECONDS"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := config.Load(ctx)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	appCtx := context.Background()
	instance, err := app.New(appCtx, cfg)
	if err != nil {
		log.Fatalf("failed to build app: %v", err)
	}

	if err := instance.Run(appCtx); err != nil {
		log.Fatalf("runtime error: %v", err)
	}
}
