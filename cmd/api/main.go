package main

import (
	"context"
	"os"
	"log"
	"time"
	"github.com/joho/godotenv"
	"github.com/gokatarajesh/quiz-platform/internal/app"
	"github.com/gokatarajesh/quiz-platform/internal/config"
)

func main() {
	if os.Getenv("APP_ENV") != "production" {
		if err := godotenv.Load("configs/.env"); err != nil {
			log.Printf("Warning: could not load .env file: %v", err)
		}
	}
	
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
