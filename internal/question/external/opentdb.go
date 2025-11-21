package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// OpenTDBClient fetches questions from the Open Trivia DB (no API key).
type OpenTDBClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewOpenTDBClient(baseURL string, httpClient *http.Client) *OpenTDBClient {
	if baseURL == "" {
		baseURL = "https://opentdb.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &OpenTDBClient{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

type OpenTDBQuestion struct {
	Category        string   `json:"category"`
	Type            string   `json:"type"`
	Difficulty      string   `json:"difficulty"`
	Question        string   `json:"question"`
	CorrectAnswer   string   `json:"correct_answer"`
	IncorrectAnswer []string `json:"incorrect_answers"`
}

type openTDBResponse struct {
	ResponseCode int               `json:"response_code"`
	Results      []OpenTDBQuestion `json:"results"`
}

func (c *OpenTDBClient) Fetch(ctx context.Context, amount int, difficulty, qType string) ([]OpenTDBQuestion, error) {
	values := url.Values{}
	values.Set("amount", fmt.Sprint(amount))
	if difficulty != "" {
		values.Set("difficulty", difficulty)
	}
	if qType != "" {
		values.Set("type", qType)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api.php?%s", c.baseURL, values.Encode()), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opentdb non-200: %d", resp.StatusCode)
	}

	var payload openTDBResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.ResponseCode != 0 {
		return nil, fmt.Errorf("opentdb response code %d", payload.ResponseCode)
	}
	return payload.Results, nil
}
