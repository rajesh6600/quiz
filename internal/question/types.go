package question

import (
	"github.com/google/uuid"
)
// Difficulty constants for readability.
const (
	DifficultyEasy   = "easy"
	DifficultyMedium = "medium"
	DifficultyHard   = "hard"
)

// Type constants.
const (
	TypeMCQ = "mcq"
	// Only MCQ questions are supported
)

// Question represents the normalized payload delivered to clients.
type Question struct {
	ID         string   `json:"id"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer,omitempty"` // server-side only
	Source     string   `json:"source"`
	Token      string   `json:"token"`
	// Type, Difficulty, Category removed - not needed, only stored in DB
}

// PackRequest guides selection for matchmaking.
type PackRequest struct {
	Category           string
	DifficultyCounts   map[string]int
	TotalQuestions     int
	Seed               string
	PerQuestionSeconds int
	UserID             *uuid.UUID   // DEPRECATED: Use UserIDs for 1v1 matches (backward compatibility)
	UserIDs            []*uuid.UUID  // NEW: For 1v1 matches - [player1, player2] for fair uniqueness checking
	MatchMode          string       // Optional: "random_1v1" or "private_room" - determines if cross-match check applies
}

// PackResponse holds selected questions and metadata.
type PackResponse struct {
	Questions []Question
	Seed      string
	ExpiresAt int64
}
