package ws

import "encoding/json"

// MessageType constants for WebSocket protocol.
const (
	// Client -> Server
	TypeJoinQueue       = "join_queue"
	TypeCancelQueue     = "cancel_queue"
	TypeAcceptBotFill   = "accept_bot_fill"
	TypeJoinPrivate     = "join_private"
	TypeReadyState      = "ready_state"
	TypeSubmitAnswer    = "submit_answer"
	TypeLeaveMatch      = "leave_match"
	TypeRequestProgress = "request_progress"

	// Server -> Client
	TypeQueueUpdate       = "queue_update"
	TypeBotOffer          = "bot_offer"
	TypeMatchFound        = "match_found"
	TypePrivateRoomUpdate = "private_room_update"
	TypeCountdown         = "countdown"
	TypeQuestionBatch     = "question_batch"
	TypeQuestionTick      = "question_tick"
	TypeAnswerAck         = "answer_ack"
	TypeProgressUpdate    = "progress_update"
	TypeMatchComplete     = "match_complete"
	TypeLeaderboardUpdate = "leaderboard_update"
	TypeMatchTimeout      = "match_timeout"
	TypeError             = "error"
	TypePing              = "ping"
	TypePong              = "pong"
)

// Message wraps all WebSocket payloads with type and optional request ID.
type Message struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	RequestID string          `json:"request_id,omitempty"`
}

// Client Messages (incoming)

type JoinQueuePayload struct {
	QueueToken    string `json:"queue_token"`
	QuestionCount int    `json:"question_count,omitempty"` // 5, 10, or 15 (default: 10)
	Category      string `json:"category,omitempty"`       // e.g., "general", "science", "history" (default: "general")
}

type CancelQueuePayload struct {
	QueueToken string `json:"queue_token"`
}

type AcceptBotFillPayload struct {
	QueueToken string `json:"queue_token"`
	Accept     bool   `json:"accept"`
}

type JoinPrivatePayload struct {
	RoomCode string `json:"room_code"`
}

type ReadyStatePayload struct {
	MatchID string `json:"match_id"`
	Ready   bool   `json:"ready"`
}

type SubmitAnswerPayload struct {
	MatchID         string `json:"match_id"`
	QuestionToken   string `json:"question_token"`
	Answer          string `json:"answer"`
	ClientLatencyMs int    `json:"client_latency_ms"`
}

type LeaveMatchPayload struct {
	MatchID string `json:"match_id"`
	Reason  string `json:"reason"`
}

type RequestProgressPayload struct {
	MatchID string `json:"match_id"`
}

// Server Messages (outgoing)

type QueueUpdatePayload struct {
	QueueToken  string `json:"queue_token"`
	Status      string `json:"status"`
	Position    int    `json:"position"`
	WaitSeconds int    `json:"wait_seconds"`
}

type BotOfferPayload struct {
	QueueToken      string `json:"queue_token"`
	DeadlineSeconds int    `json:"deadline_seconds"`
}

type MatchFoundPayload struct {
	MatchID              string   `json:"match_id"`
	Mode                 string   `json:"mode"`
	Players              []Player `json:"players"`
	QuestionCount        int      `json:"question_count"`
	PerQuestionSeconds   int      `json:"per_question_seconds"`
	GlobalTimeoutSeconds int      `json:"global_timeout_seconds"`
}

type Player struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type PrivateRoomUpdatePayload struct {
	MatchID        string   `json:"match_id"`
	RoomCode       string   `json:"room_code"`
	Players        []Player `json:"players"`
	SlotsRemaining int      `json:"slots_remaining"`
}

type CountdownPayload struct {
	MatchID string `json:"match_id"`
	Seconds int    `json:"seconds"`
}

type QuestionBatchPayload struct {
	MatchID  string            `json:"match_id"`
	Batch    []QuestionPayload `json:"batch"`
	Seed     string            `json:"seed"`
	IssuedAt string            `json:"issued_at"`
}

type QuestionPayload struct {
	Order   int      `json:"order"`
	ID      string   `json:"id"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Token   string   `json:"token"`
	// Removed: Type, Difficulty, Category (not needed by client, only server-side)
}

type QuestionTickPayload struct {
	MatchID          string `json:"match_id"`
	QuestionOrder    int    `json:"question_order"`
	RemainingSeconds int    `json:"remaining_seconds"`
}

type AnswerAckPayload struct {
	MatchID          string `json:"match_id"`
	QuestionOrder    int    `json:"question_order"`
	Accepted         bool   `json:"accepted"`
	ServerReceivedAt string `json:"server_received_at"`
}

type ProgressUpdatePayload struct {
	MatchID string           `json:"match_id"`
	Players []PlayerProgress `json:"players"`
}

type PlayerProgress struct {
	UserID   string `json:"user_id"`
	Answered int    `json:"answered"`
	Pending  int    `json:"pending"`
	Status   string `json:"status"`
}

type MatchCompletePayload struct {
	MatchID             string        `json:"match_id"`
	Results             []MatchResult `json:"results"`
	LeaderboardEligible bool          `json:"leaderboard_eligible"`
	LeaderboardPosition int           `json:"leaderboard_position,omitempty"`
}

type MatchResult struct {
	UserID             string  `json:"user_id"`
	DisplayName        string  `json:"display_name"`
	FinalScore         int     `json:"final_score"`
	Accuracy           float64 `json:"accuracy"`
	StreakBonusApplied float64 `json:"streak_bonus_applied"`
	Status             string  `json:"status"`
}

type LeaderboardUpdatePayload struct {
	Window  string             `json:"window"`
	Top     []LeaderboardEntry `json:"top"`
	MatchID string             `json:"match_id"`
}

type LeaderboardEntry struct {
	Rank        int     `json:"rank"`
	UserID      string  `json:"user_id"`
	DisplayName string  `json:"display_name"`
	Score       int     `json:"score"`
	Wins        int     `json:"wins"`
	Games       int     `json:"games"`
	Accuracy    float64 `json:"accuracy"`
}

type MatchTimeoutPayload struct {
	MatchID string `json:"match_id"`
	Reason  string `json:"reason"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
