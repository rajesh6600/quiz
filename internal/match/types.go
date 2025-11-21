package match

import (
	"time"

	"github.com/google/uuid"
)

// MatchMode constants.
const (
	ModeRandom1v1   = "random_1v1"
	ModePrivateRoom = "private_room"
	ModeBotFill     = "bot_fill"
)

// MatchStatus lifecycle states.
const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusTimeout   = "timeout"
	StatusCancelled = "cancelled"
)

// PlayerStatus within a match.
const (
	PlayerStatusQueued    = "queued"
	PlayerStatusActive    = "active"
	PlayerStatusCompleted = "completed"
	PlayerStatusLeftEarly = "left_early"
	PlayerStatusTimeout   = "timeout"
)


// Match represents a game session.
type Match struct {
	ID                   uuid.UUID
	Mode                 string
	QuestionCount        int
	PerQuestionSeconds   int
	GlobalTimeoutSeconds int
	SeedHash             string
	LeaderboardEligible  bool
	Status               string
	CreatedBy            *uuid.UUID
	StartedAt            *time.Time
	CompletedAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// PlayerState tracks individual player progress.
type PlayerState struct {
	MatchID        uuid.UUID
	UserID         uuid.UUID
	IsGuest        bool
	DisplayName    string
	JoinedAt       time.Time
	LeftAt         *time.Time
	FinalScore     *int
	Status         string
	Accuracy       *float64
	StreakBonusPct *float64
	Answers        []AnswerRecord
}

// AnswerRecord stores per-question response with timing.
type AnswerRecord struct {
	QuestionOrder int       `json:"question_order"`
	QuestionToken string    `json:"question_token"`
	Answer        string    `json:"answer"`
	SubmittedAt   time.Time `json:"submitted_at"`
	IsCorrect     bool      `json:"is_correct"`
	ScoreEarned   int       `json:"score_earned"`
}

// MatchmakingRequest for random 1v1 queue.
type MatchmakingRequest struct {
	UserID              uuid.UUID
	DisplayName         string
	IsGuest             bool
	PreferredCategory   string
	PreferredDifficulty string
	BotOK               bool
}

// PrivateRoomRequest for creating/joining private matches.
type PrivateRoomRequest struct {
	HostID             uuid.UUID
	DisplayName        string
	MatchName          string
	MaxPlayers         int
	QuestionCount      int
	PerQuestionSeconds int
}
