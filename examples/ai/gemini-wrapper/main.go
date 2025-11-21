// package main

// import (
// 	"bytes"
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"log"
// 	"net/http"
// 	"os"
// 	"strings"
// 	"time"

// )


// type generateRequest struct {
// 	Category   string `json:"category"`
// 	Difficulty string `json:"difficulty"`
// 	Count      int    `json:"count"`
// 	Seed       string `json:"seed"`
// }

// type generateResponse struct {
// 	Questions []questionPayload `json:"questions"`
// }

// type questionPayload struct {
// 	ID         string   `json:"id"`
// 	Prompt     string   `json:"prompt"`
// 	Options    []string `json:"options"`
// 	Answer     string   `json:"answer"`
// 	Type       string   `json:"type"`
// 	Difficulty string   `json:"difficulty"`
// 	Category   string   `json:"category"`
// }

// type geminiRequest struct {
// 	Contents []struct {
// 		Parts []struct {
// 			Text string `json:"text"`
// 		} `json:"parts"`
// 	} `json:"contents"`
// 	SafetySettings   []interface{}          `json:"safetySettings,omitempty"`
// 	GenerationConfig map[string]interface{} `json:"generationConfig,omitempty"`
// }

// type geminiResponse struct {
// 	Candidates []struct {
// 		Content struct {
// 			Parts []struct {
// 				Text string `json:"text"`
// 			} `json:"parts"`
// 		} `json:"content"`
// 	} `json:"candidates"`
// }

// func main() {
// 	port := getEnv("PORT", "9090")
// 	model := getEnv("GEMINI_MODEL", "models/gemini-2.5-flash")
// 	apiKey := os.Getenv("GEMINI_API_KEY")
// 	if apiKey == "" {
// 		log.Fatal("GEMINI_API_KEY must be set")
// 	}

// 	srv := &server{
// 		client: &http.Client{
// 			Timeout: 12 * time.Second,
// 		},
// 		model:  model,
// 		apiKey: apiKey,
// 	}

// 	http.HandleFunc("/generate", srv.handleGenerate)
// 	http.HandleFunc("/enqueue", handleEnqueue)

// 	log.Printf("Gemini wrapper listening on :%s (model=%s)\n", port, model)
// 	log.Fatal(http.ListenAndServe(":"+port, nil))
// }

// type server struct {
// 	client *http.Client
// 	model  string
// 	apiKey string
// }

// func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request) {
// 	if r.Method != http.MethodPost {
// 		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
// 		return
// 	}
// 	defer r.Body.Close()

// 	var req generateRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
// 		return
// 	}
// 	if req.Count <= 0 {
// 		req.Count = 5
// 	}

// 	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
// 	defer cancel()

// 	questions, err := s.generateFromGemini(ctx, req)
// 	if err != nil {
// 		log.Printf("gemini error: %v\n", err)
// 		http.Error(w, err.Error(), http.StatusBadGateway)
// 		return
// 	}

// 	resp := generateResponse{Questions: questions}
// 	w.Header().Set("Content-Type", "application/json")
// 	json.NewEncoder(w).Encode(resp)
// }

// func (s *server) generateFromGemini(ctx context.Context, req generateRequest) ([]questionPayload, error) {
// 	prompt := buildPrompt(req)

// 	gReq := geminiRequest{
// 		Contents: []struct {
// 			Parts []struct {
// 				Text string `json:"text"`
// 			} `json:"parts"`
// 		}{
// 			{
// 				Parts: []struct {
// 					Text string `json:"text"`
// 				}{
// 					{Text: prompt},
// 				},
// 			},
// 		},
// 		GenerationConfig: map[string]interface{}{
// 			"temperature":     0.4,
// 			"maxOutputTokens": 5000,
// 		},
// 	}

// 	body, err := json.Marshal(gReq)
// 	if err != nil {
// 		return nil, err
// 	}

// 	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s:generateContent?key=%s", s.model, s.apiKey)
// 	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
// 	if err != nil {
// 		return nil, err
// 	}
// 	httpReq.Header.Set("Content-Type", "application/json")

// 	resp, err := s.client.Do(httpReq)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer resp.Body.Close()

// 	if resp.StatusCode >= 300 {
// 		return nil, fmt.Errorf("gemini status %d", resp.StatusCode)
// 	}

// 	var gResp geminiResponse
// 	if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
// 		return nil, err
// 	}

// 	if len(gResp.Candidates) == 0 || len(gResp.Candidates[0].Content.Parts) == 0 {
// 		return nil, fmt.Errorf("gemini returned empty response")
// 	}

// 	raw := gResp.Candidates[0].Content.Parts[0].Text
// 	raw = strings.TrimSpace(raw)
// 	raw = strings.TrimPrefix(raw, "```json")
// 	raw = strings.TrimPrefix(raw, "```")
// 	raw = strings.TrimSuffix(raw, "```")

// 	var payload generateResponse
// 	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
// 		return nil, fmt.Errorf("parse gemini JSON: %w", err)
// 	}

// 	for i := range payload.Questions {
// 		q := &payload.Questions[i]
// 		if q.Type == "" {
// 			q.Type = "mcq"
// 		}
// 		if len(q.Options) == 0 {
// 			q.Options = []string{q.Answer}
// 		}
// 	}
// 	if len(payload.Questions) == 0 {
// 		return nil, fmt.Errorf("gemini returned zero questions")
// 	}
// 	return payload.Questions, nil
// }

// func handleEnqueue(w http.ResponseWriter, r *http.Request) {
// 	if r.Method != http.MethodPost {
// 		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
// 		return
// 	}
// 	w.WriteHeader(http.StatusAccepted)
// }

// func buildPrompt(req generateRequest) string {
// 	builder := strings.Builder{}
// 	builder.WriteString("You are an assistant that generates valid trivia questions in JSON.\n")
// 	builder.WriteString("Return JSON ONLY in the shape {\"questions\": [ ... ]} matching this Go struct:\n")
// 	builder.WriteString("{\"id\":\"uuid\",\"prompt\":\"string\",\"options\":[\"...\"],\"answer\":\"string\",\"type\":\"mcq|true_false\",\"difficulty\":\"easy|medium|hard\",\"category\":\"category\"}.\n")
// 	builder.WriteString("Guidelines:\n")
// 	builder.WriteString("- Provide exactly ")
// 	builder.WriteString(fmt.Sprint(req.Count))
// 	builder.WriteString(" questions.\n")
// 	if req.Category != "" {
// 		builder.WriteString("- Category: ")
// 		builder.WriteString(req.Category)
// 		builder.WriteString(".\n")
// 	}
// 	if req.Difficulty != "" {
// 		builder.WriteString("- Difficulty: ")
// 		builder.WriteString(req.Difficulty)
// 		builder.WriteString(".\n")
// 	}
// 	builder.WriteString("- Ensure the correct answer is always present in options.\n")
// 	builder.WriteString("- Use concise prompts and avoid markdown.\n")
// 	if req.Seed != "" {
// 		builder.WriteString("- Use seed ")
// 		builder.WriteString(req.Seed)
// 		builder.WriteString(" to inspire variation.\n")
// 	}
// 	return builder.String()
// }

// func getEnv(key, fallback string) string {
// 	if v := os.Getenv(key); v != "" {
// 		return v
// 	}
// 	return fallback
// }







//chatgpt written code

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type generateRequest struct {
	Category   string `json:"category"`
	Difficulty string `json:"difficulty"`
	Count      int    `json:"count"`
	Seed       string `json:"seed"`
}

type generateResponse struct {
	Questions []questionPayload `json:"questions"`
}

type questionPayload struct {
	ID         string   `json:"id"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer"`
	Type       string   `json:"type"`
	Difficulty string   `json:"difficulty"`
	Category   string   `json:"category"`
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
	model := getEnv("GEMINI_MODEL", "models/gemini-2.5-flash")
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY must be set")
	}

	srv := &server{
		client: &http.Client{
			Timeout: 40 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,   // allow many parallel calls
				MaxIdleConnsPerHost: 200,   // allow many Gemini requests
				IdleConnTimeout:     90 * time.Second,
			},
		}
		model:  model,
		apiKey: apiKey,
	}

	http.HandleFunc("/generate", srv.handleGenerate)
	http.HandleFunc("/enqueue", handleEnqueue)

	log.Printf("Gemini wrapper listening on :%s (model=%s)\n", port, model)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

type server struct {
	client *http.Client
	model  string
	apiKey string
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
	if req.Count <= 0 {
		req.Count = 5
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
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
			"maxOutputTokens": 5000,
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
			time.Sleep(250 * time.Millisecond)
			continue
		}

		var gResp geminiResponse
		if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
			if attempt == 3 {
				return nil, fmt.Errorf("gemini decode failed: %v", err)
			}
			time.Sleep(200 & time.Millisecond)
			continue
		}

		// Try all candidates and all parts
		for _, c := range gResp.Candidates {
			for _, part := range c.Content.Parts {

				raw := cleanJSON(part.Text)

				var payload generateResponse
				err := json.Unmarshal([]byte(raw), &payload)
				if err == nil && len(payload.Questions) > 0 {

					// Fill missing fields
					for i := range payload.Questions {
						if payload.Questions[i].Type == "" {
							payload.Questions[i].Type = "mcq"
						}
					}

					return payload.Questions, nil
				}
			}
		}

		// retry parse
		time.Sleep(250 * time.Millisecond)
	}

	return nil, fmt.Errorf("gemini returned malformed JSON after retries")
}

func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)

	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Cut off leading junk before first {
	i := strings.Index(raw, "{")
	if i > 0 {
		raw = raw[i:]
	}

	// Cut off trailing junk after last }
	j := strings.LastIndex(raw, "}")
	if j > 0 && j+1 < len(raw) {
		raw = raw[:j+1]
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
	builder.WriteString("{\"questions\":[{\"id\":\"uuid\",\"prompt\":\"string\",\"options\":[\"a\",\"b\",\"c\",\"d\"],\"answer\":\"string\",\"type\":\"mcq\",\"difficulty\":\"")
	builder.WriteString(req.Difficulty)
	builder.WriteString("\",\"category\":\"")
	builder.WriteString(req.Category)
	builder.WriteString("\"}]}.\n")

	builder.WriteString("Rules:\n")
	builder.WriteString("- Generate exactly ")
	builder.WriteString(fmt.Sprint(req.Count))
	builder.WriteString(" questions.\n")
	builder.WriteString("- Ensure 'answer' appears inside 'options'.\n")
	builder.WriteString("- Keep prompts short.\n")
	builder.WriteString("- No markdown.\n")
	builder.WriteString("- No commentary.\n")

	return builder.String()
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
