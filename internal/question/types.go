package question

// Difficulty constants for readability.
const (
	DifficultyEasy   = "easy"
	DifficultyMedium = "medium"
	DifficultyHard   = "hard"
)

// Type constants.
const (
	TypeMCQ       = "mcq"
	TypeTrueFalse = "true_false"
)

// Question represents the normalized payload delivered to clients.
type Question struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer,omitempty"` // server-side only
	Difficulty string   `json:"difficulty"`
	Category   string   `json:"category"`
	Source     string   `json:"source"`
	Token      string   `json:"token"`
}

// PackRequest guides selection for matchmaking.
type PackRequest struct {
	Category           string
	DifficultyCounts   map[string]int
	TotalQuestions     int
	Seed               string
	PerQuestionSeconds int
}

// PackResponse holds selected questions and metadata.
type PackResponse struct {
	Questions []Question
	Seed      string
	ExpiresAt int64
}
