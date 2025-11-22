package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/question"
)

// Config holds connection details for the AI generator service.
type Config struct {
	GeneratorURL string
	GeneratorKey string
	Timeout      time.Duration
}

// Generator implements question.AIGenerator.
type Generator struct {
	httpClient  *http.Client
	config      Config
	logger      zerolog.Logger
	generateURL string
	enqueueURL  string
}

func NewGenerator(cfg Config, logger zerolog.Logger) *Generator {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	base := strings.TrimSuffix(cfg.GeneratorURL, "/")

	return &Generator{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		config:      cfg,
		logger:      logger.With().Str("component", "ai_generator").Logger(),
		generateURL: base + "/generate",
		enqueueURL:  base + "/enqueue",
	}
}

// GeneratePack synchronously requests AI questions and optionally verifies them.
func (g *Generator) GeneratePack(ctx context.Context, req question.AIGenerateRequest) ([]question.Question, error) {
	if g.config.GeneratorURL == "" {
		return nil, fmt.Errorf("generator endpoint not configured")
	}

	payload := generatorRequest{
		Category:         req.Category,
		Difficulty:       req.Difficulty,
		Count:            req.Count,
		Seed:             req.Seed,
		DifficultyCounts: req.DifficultyCounts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.generateURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if g.config.GeneratorKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+g.config.GeneratorKey)
	}

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("generator returned status %d", resp.StatusCode)
	}

	var genResp generatorResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, fmt.Errorf("decode generator payload: %w", err)
	}

	questions := make([]question.Question, 0, len(genResp.Questions))
	for _, q := range genResp.Questions {
		questions = append(questions, normalizeAIQuestion(q))
	}

	if len(questions) == 0 {
		return nil, fmt.Errorf("generator returned empty question set")
	}

	return questions, nil
}

// EnqueuePack notifies the async generator service to prep future packs.
func (g *Generator) EnqueuePack(ctx context.Context, req question.AIGenerateRequest) error {
	if g.config.GeneratorURL == "" {
		return nil
	}

	payload := generatorRequest{
		Category:         req.Category,
		Difficulty:       req.Difficulty,
		Count:            req.Count,
		Seed:             req.Seed,
		DifficultyCounts: req.DifficultyCounts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.enqueueURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if g.config.GeneratorKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+g.config.GeneratorKey)
	}

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("enqueue returned status %d", resp.StatusCode)
	}
	return nil
}

func normalizeAIQuestion(q aiQuestion) question.Question {
	options := q.Options
	if len(options) == 0 {
		options = []string{q.Answer}
	}
	
	// Ensure answer present in options
	found := false
	for _, opt := range options {
		if strings.EqualFold(opt, q.Answer) {
			found = true
			break
		}
	}
	if !found && q.Answer != "" {
		options = append(options, q.Answer)
	}

	id := q.ID
	if id == "" {
		id = uuid.NewString()
	}

	return question.Question{
		ID:      id,
		Prompt:  q.Prompt,
		Options: options,
		Answer:  q.Answer,
		Source:  "ai",
		// Type, Difficulty, Category removed - not needed
	}
}

type generatorRequest struct {
	Category         string         `json:"category"`
	Difficulty       string         `json:"difficulty"`
	Count            int            `json:"count"`
	Seed             string         `json:"seed"`
	DifficultyCounts map[string]int `json:"difficulty_counts"`
}

type aiQuestion struct {
	ID         string   `json:"id"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer"`
	// Type, Difficulty, Category removed - not needed from Gemini
	// Server will infer Type from options count and set defaults
}

type generatorResponse struct {
	Questions []aiQuestion `json:"questions"`
}
