package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Max concurrent requests to Gemini to avoid rate limits
const maxConcurrentRequests = 15

var sem = make(chan struct{}, maxConcurrentRequests)

// RateLimiter implements a token bucket rate limiter with request queuing
type RateLimiter struct {
	tokens      chan struct{}     // Token bucket channel
	queue       chan chan struct{} // Request queue
	maxWait     time.Duration     // Maximum wait time for queued requests
	refillRate  time.Duration     // Time between token refills
	stop        chan struct{}     // Stop signal for refill goroutine
	wg          sync.WaitGroup    // Wait group for cleanup
}

// NewRateLimiter creates a new rate limiter with specified RPM and max wait time
func NewRateLimiter(rpm int, maxWaitSeconds int) *RateLimiter {
	if rpm <= 0 {
		rpm = 10 // Default to 10 RPM
	}
	if maxWaitSeconds <= 0 {
		maxWaitSeconds = 30 // Default to 30 seconds
	}

	// Calculate refill rate: 60 seconds / RPM = seconds per token
	refillRate := time.Duration(60/rpm) * time.Second

	rl := &RateLimiter{
		tokens:     make(chan struct{}, rpm), // Bucket size = RPM
		queue:      make(chan chan struct{}, 100), // Queue up to 100 requests
		maxWait:    time.Duration(maxWaitSeconds) * time.Second,
		refillRate: refillRate,
		stop:       make(chan struct{}),
	}

	// Fill bucket initially
	for i := 0; i < rpm; i++ {
		rl.tokens <- struct{}{}
	}

	// Start refill goroutine
	rl.wg.Add(1)
	go rl.refill()

	return rl
}

// refill continuously adds tokens to the bucket at the refill rate
func (rl *RateLimiter) refill() {
	defer rl.wg.Done()
	ticker := time.NewTicker(rl.refillRate)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if there's a queued request first
			select {
			case reqChan := <-rl.queue:
				// Try to get a token for queued request
				select {
				case token := <-rl.tokens:
					// Send token to queued request
					select {
					case reqChan <- token:
						// Successfully served queued request
					default:
						// Request already timed out, put token back
						select {
						case rl.tokens <- token:
						default:
						}
					}
				default:
					// No token available, put request back in queue
					select {
					case rl.queue <- reqChan:
					default:
						// Queue full, request will timeout
					}
				}
			default:
				// No queued requests, add token to bucket
				select {
				case rl.tokens <- struct{}{}:
					// Token added to bucket
				default:
					// Bucket is full, skip
				}
			}
		case <-rl.stop:
			return
		}
	}
}

// Acquire attempts to acquire a token, waiting up to maxWait
// Returns true if token acquired, false if timeout
func (rl *RateLimiter) Acquire(ctx context.Context) bool {
	// Try to get token immediately
	select {
	case <-rl.tokens:
		return true
	default:
		// No token available, queue request
	}

	// Create channel for this request
	reqChan := make(chan struct{}, 1)

	// Add to queue
	select {
	case rl.queue <- reqChan:
		// Queued successfully
	default:
		// Queue full, reject immediately
		return false
	}

	// Wait for token or timeout
	waitCtx, cancel := context.WithTimeout(ctx, rl.maxWait)
	defer cancel()

	select {
	case <-reqChan:
		return true
	case <-waitCtx.Done():
		return false
	}
}

// Stop stops the rate limiter and cleans up resources
func (rl *RateLimiter) Stop() {
	close(rl.stop)
	rl.wg.Wait()
}

type generateRequest struct {
	Category         string         `json:"category"`
	Difficulty       string         `json:"difficulty"` // Legacy: single difficulty
	Count            int            `json:"count"`
	Seed             string         `json:"seed"`
	DifficultyCounts map[string]int `json:"difficulty_counts"` // New: mixed difficulties
}

type generateResponse struct {
	Questions []questionPayload `json:"questions"`
}

type questionPayload struct {
	ID         string   `json:"id"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer"`
	// Type, Difficulty, Category removed - not needed from Gemini
	// Server will infer Type from options count and set defaults
}

type geminiRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
	SafetySettings   []interface{}          `json:"safetySettings,omitempty"`
	GenerationConfig map[string]interface{} `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func main() {
	port := getEnv("PORT", "9090")
	model := getEnv("GEMINI_MODEL", "models/gemini-2.0-flash-exp") // Updated model
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY must be set")
	}

	// Read rate limit configuration from environment
	rpm := getEnvInt("GEMINI_RATE_LIMIT_RPM", 10)
	maxWaitSeconds := getEnvInt("GEMINI_RATE_LIMIT_MAX_WAIT_SECONDS", 30)

	// Initialize rate limiter
	rateLimiter := NewRateLimiter(rpm, maxWaitSeconds)
	log.Printf("Rate limiter initialized: %d RPM, max wait: %d seconds\n", rpm, maxWaitSeconds)

	srv := &server{
		client: &http.Client{
			Timeout: 60 * time.Second, // Increased timeout for larger batches
			Transport: &http.Transport{
				MaxIdleConns:        200,   // allow many parallel calls
				MaxIdleConnsPerHost: 200,   // allow many Gemini requests
				IdleConnTimeout:     90 * time.Second,
			},
		},
		model:       model,
		apiKey:      apiKey,
		rateLimiter: rateLimiter,
	}

	http.HandleFunc("/generate", srv.handleGenerate)
	http.HandleFunc("/enqueue", handleEnqueue)

	log.Printf("Gemini wrapper listening on :%s (model=%s)\n", port, model)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

type server struct {
	client      *http.Client
	model       string
	apiKey      string
	rateLimiter *RateLimiter
}

func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	
	// Auto-calculate difficulty_counts if not provided
	if len(req.DifficultyCounts) == 0 {
		req.DifficultyCounts = getDefaultDifficultyDistribution(req.Count)
	}
	
	// Rate limiting: acquire token before proceeding
	if !s.rateLimiter.Acquire(r.Context()) {
		http.Error(w, "rate limit exceeded: request queued for too long", http.StatusTooManyRequests)
		return
	}
	
	// Concurrency throttling (existing semaphore)
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-r.Context().Done():
		http.Error(w, "client disconnected", http.StatusRequestTimeout)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	questions, err := s.generateFromGemini(ctx, req)
	if err != nil {
		log.Printf("gemini error: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	resp := generateResponse{Questions: questions}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *server) generateFromGemini(ctx context.Context, req generateRequest) ([]questionPayload, error) {
	prompt := buildPrompt(req)

	gReq := geminiRequest{
		Contents: []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}{
			{
				Parts: []struct {
					Text string `json:"text"`
				}{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: map[string]interface{}{
			"temperature":     0.4,
			"maxOutputTokens": 8192, // Increased for larger batches
			"responseMimeType": "application/json", // Force JSON mode for newer models
		},
	}

	body, _ := json.Marshal(gReq)

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/%s:generateContent?key=%s",
		s.model,
		s.apiKey,
	)

	// RETRY LOOP for malformed/truncated JSON
	for attempt := 1; attempt <= 3; attempt++ {

		httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(httpReq)
		if err != nil {
			if attempt == 3 {
				return nil, fmt.Errorf("gemini request failed: %v", err)
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			if attempt == 3 {
				return nil, fmt.Errorf("gemini status %d", resp.StatusCode)
			}
			time.Sleep(500 * time.Millisecond) // Increased backoff
			continue
		}

		var gResp geminiResponse
		if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
			if attempt == 3 {
				return nil, fmt.Errorf("gemini decode failed: %v", err)
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// Try all candidates and all parts
		for _, c := range gResp.Candidates {
			for _, part := range c.Content.Parts {

				raw := cleanJSON(part.Text)

				var payload generateResponse
				// Try parsing as the full response object
				err := json.Unmarshal([]byte(raw), &payload)
				if err == nil && len(payload.Questions) > 0 {
					// Normalize questions
					return normalizeQuestions(payload.Questions, req), nil
				}
				
				// Sometimes models return just the list array
				var qList []questionPayload
				err = json.Unmarshal([]byte(raw), &qList)
				if err == nil && len(qList) > 0 {
					return normalizeQuestions(qList, req), nil
				}
			}
		}

		// retry parse
		time.Sleep(250 * time.Millisecond)
	}

	return nil, fmt.Errorf("gemini returned malformed JSON after retries")
}

func normalizeQuestions(qs []questionPayload, req generateRequest) []questionPayload {
	for i := range qs {
		// Type field removed - only MCQ questions supported
		if qs[i].ID == "" {
			qs[i].ID = "gen-" + fmt.Sprint(i) // Placeholder
		}
		// Ensure options include answer
		found := false
		for _, opt := range qs[i].Options {
			if strings.EqualFold(opt, qs[i].Answer) {
				found = true
				break
			}
		}
		if !found && qs[i].Answer != "" {
			qs[i].Options = append(qs[i].Options, qs[i].Answer)
		}
	}
	return qs
}

func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Cut off leading junk before first { or [
	iObj := strings.Index(raw, "{")
	iArr := strings.Index(raw, "[")
	
	start := -1
	if iObj >= 0 && (iArr == -1 || iObj < iArr) {
		start = iObj
	} else if iArr >= 0 {
		start = iArr
	}

	if start > 0 {
		raw = raw[start:]
	}

	// Cut off trailing junk after last } or ]
	jObj := strings.LastIndex(raw, "}")
	jArr := strings.LastIndex(raw, "]")
	
	end := -1
	if jObj >= 0 && (jArr == -1 || jObj > jArr) {
		end = jObj
	} else if jArr >= 0 {
		end = jArr
	}

	if end >= 0 && end+1 < len(raw) {
		raw = raw[:end+1]
	}

	return strings.TrimSpace(raw)
}

func handleEnqueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func buildPrompt(req generateRequest) string {
	builder := strings.Builder{}

	builder.WriteString("Return ONLY valid JSON. No text. No markdown. No explanations.\n")
	builder.WriteString("Structure: {\"questions\":[{\"id\":\"uuid\",\"prompt\":\"string\",\"options\":[\"a\",\"b\",\"c\",\"d\"],\"answer\":\"string\"}]}.\n")

	builder.WriteString("Task: Generate ")
	builder.WriteString(fmt.Sprint(req.Count))
	builder.WriteString(" trivia questions about '")
	builder.WriteString(req.Category)
	builder.WriteString("'.\n")

	if len(req.DifficultyCounts) > 0 {
		builder.WriteString("Difficulty distribution:\n")
		for diff, count := range req.DifficultyCounts {
			if count > 0 {
				builder.WriteString(fmt.Sprintf("- %d %s questions\n", count, diff))
			}
		}
	} else {
		builder.WriteString("Difficulty: ")
		builder.WriteString(req.Difficulty)
		builder.WriteString("\n")
	}

	builder.WriteString("Rules:\n")
	builder.WriteString("- Only generate MCQ (Multiple Choice) questions.\n")
	builder.WriteString("- 'options' must have exactly 4 choices.\n")
	builder.WriteString("- Ensure 'answer' is exactly one of the 'options'.\n")
	builder.WriteString("- Keep prompts concise.\n")
	builder.WriteString("- Use JSON format strictly.\n")

	return builder.String()
}

// getDefaultDifficultyDistribution returns default difficulty counts based on question count.

func getDefaultDifficultyDistribution(count int) map[string]int {
	switch count {
	case 5:
		return map[string]int{
			"easy":   2,
			"medium": 2,
			"hard":   1,
		}
	case 10:
		return map[string]int{
			"easy":   5,
			"medium": 3,
			"hard":   2,
		}
	case 15:
		return map[string]int{
			"easy":   7,
			"medium": 5,
			"hard":   3,
		}
	default:
		// Fallback to 10-question distribution
		return map[string]int{
			"easy":   5,
			"medium": 3,
			"hard":   2,
		}
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt reads an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}
