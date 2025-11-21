package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TriviaAPIClient integrates with triviaapi.com (needs API key env TRIVIA_API_KEY).
type TriviaAPIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewTriviaAPIClient(baseURL, apiKey string, httpClient *http.Client) *TriviaAPIClient {
	if baseURL == "" {
		baseURL = "https://triviaapi.com/api"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &TriviaAPIClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey, // actual secret stored in env `TRIVIA_API_KEY`.
		httpClient: httpClient,
	}
}

type TriviaAPIQuestion struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Question   string   `json:"question"`
	Difficulty string   `json:"difficulty"`
	Type       string   `json:"type"`
	Correct    string   `json:"correctAnswer"`
	Incorrect  []string `json:"incorrectAnswers"`
}

func (c *TriviaAPIClient) Fetch(ctx context.Context, amount int, category, difficulty string) ([]TriviaAPIQuestion, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/questions?limit=%d&difficulty=%s&category=%s", c.baseURL, amount, difficulty, category), nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("triviaapi non-200: %d", resp.StatusCode)
	}

	var payload []TriviaAPIQuestion
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}
