package match

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/gokatarajesh/quiz-platform/internal/db/repository"
	sqlcgen "github.com/gokatarajesh/quiz-platform/internal/db/sqlc"
	"github.com/gokatarajesh/quiz-platform/internal/leaderboard"
	"github.com/gokatarajesh/quiz-platform/internal/match/queue"
	"github.com/gokatarajesh/quiz-platform/internal/match/scoring"
	"github.com/gokatarajesh/quiz-platform/internal/question"
	"github.com/gokatarajesh/quiz-platform/pkg/http/ws"
)

// Service orchestrates match lifecycle, scoring, and state transitions.
type Service struct {
	matchRepo     *repository.MatchRepository
	questionSvc   *question.Service
	stateMgr      *StateManager
	queueMgr      *queue.Manager
	roomMgr       *RoomManager
	leaderboard   *leaderboard.Service
	scoringEngine *scoring.Engine
	hmacKey       []byte
	logger        zerolog.Logger
}

// ServiceOptions configures the match service.
type ServiceOptions struct {
	HMACSecret    []byte
	ScoringConfig scoring.ScoringConfig
}

// NewService creates a match service with all dependencies.
func NewService(
	matchRepo *repository.MatchRepository,
	questionSvc *question.Service,
	stateMgr *StateManager,
	queueMgr *queue.Manager,
	roomMgr *RoomManager,
	leaderboardSvc *leaderboard.Service,
	opts ServiceOptions,
	logger zerolog.Logger,
) *Service {
	scoringCfg := opts.ScoringConfig
	if scoringCfg.BaseScore == 0 {
		scoringCfg = scoring.DefaultScoringConfig()
	}

	return &Service{
		matchRepo:     matchRepo,
		questionSvc:   questionSvc,
		stateMgr:      stateMgr,
		queueMgr:      queueMgr,
		roomMgr:       roomMgr,
		leaderboard:   leaderboardSvc,
		scoringEngine: scoring.NewEngine(scoringCfg),
		hmacKey:       opts.HMACSecret,
		logger:        logger,
	}
}

// getFixedDifficultyDistribution 
func getFixedDifficultyDistribution(questionCount int) map[string]int {
	switch questionCount {
	case 5:
		return map[string]int{
			question.DifficultyEasy:   2,
			question.DifficultyMedium: 2,
			question.DifficultyHard:   1,
		}
	case 10:
		return map[string]int{
			question.DifficultyEasy:   4,
			question.DifficultyMedium: 3,
			question.DifficultyHard:   3,
		}
	case 15:
		return map[string]int{
			question.DifficultyEasy:   7,
			question.DifficultyMedium: 5,
			question.DifficultyHard:   3,
		}
	default:
		// Fallback to 10-question distribution for invalid counts
		return map[string]int{
			question.DifficultyEasy:   4,
			question.DifficultyMedium: 3,
			question.DifficultyHard:   3,
		}
	}
}

// CreateRandomMatch creates a 1v1 match from a matched pair.
func (s *Service) CreateRandomMatch(ctx context.Context, pair *queue.MatchPair, questionCount int, perQuestionSec int, category string) (*Match, []QuestionPackItem, error) {
	matchID := uuid.New()
	seedHash := fmt.Sprintf("%s-%d", matchID.String(), time.Now().Unix())

	// Calculate global timeout
	globalTimeout := (questionCount * perQuestionSec) + 20 // padding

	// Create match record
	pgMatchID := pgtype.UUID{}
	if err := pgMatchID.Scan(matchID); err != nil {
		return nil, nil, fmt.Errorf("scan uuid: %w", err)
	}

	pgPlayer1ID := pgtype.UUID{}
	if err := pgPlayer1ID.Scan(pair.Player1.UserID); err != nil {
		return nil, nil, fmt.Errorf("scan uuid: %w", err)
	}

	createParams := sqlcgen.CreateMatchParams{
		Mode:                 ModeRandom1v1,
		QuestionCount:        int16(questionCount),
		PerQuestionSeconds:   int16(perQuestionSec),
		GlobalTimeoutSeconds: int16(globalTimeout),
		SeedHash:             seedHash,
		LeaderboardEligible:  !pair.Player1.IsGuest && !pair.Player2.IsGuest, // only if both registered
		Status:               StatusPending,
		CreatedBy:            pgPlayer1ID,
	}

	_, err := s.matchRepo.Create(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("create match: %w", err)
	}

	// Get fixed difficulty distribution based on question count
	diffCounts := getFixedDifficultyDistribution(questionCount)

	// Get both player IDs for fair uniqueness checking
	player1ID := pair.Player1.UserID
	player2ID := pair.Player2.UserID
	
	// Use category from request, default to "general" if empty
	if category == "" {
		category = "general"
	}
	
	packReq := question.PackRequest{
		Category:           category,
		DifficultyCounts:   diffCounts,
		TotalQuestions:     questionCount,
		Seed:               seedHash,
		PerQuestionSeconds: perQuestionSec,
		UserIDs:            []*uuid.UUID{&player1ID, &player2ID}, // Pass both players for fair checking
		MatchMode:          ModeRandom1v1,
	}

	packResp, err := s.questionSvc.FetchPack(ctx, packReq)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch questions: %w", err)
	}

	// Convert to pack items with HMAC tokens
	packItems := make([]QuestionPackItem, len(packResp.Questions))
	for i, q := range packResp.Questions {
		token := s.signQuestionToken(q.ID, q.Answer)
		packItems[i] = QuestionPackItem{
			Order:         i + 1,
			ID:            q.ID,
			Prompt:        q.Prompt,
			Options:       q.Options,
			Token:         token,
			CorrectAnswer: q.Answer,
		}
	}

	// Store questions in Redis
	if err := s.stateMgr.StoreMatchQuestions(ctx, matchID, packItems); err != nil {
		s.logger.Warn().Err(err).Msg("failed to cache questions")
	}

	// Save question IDs to user history for 1v1 matches (for cross-match uniqueness)
	questionIDs := make([]string, len(packItems))
	for i, item := range packItems {
		questionIDs[i] = item.ID
	}
	// Save for both players
	if err := s.questionSvc.AddUserQuestionHistory(ctx, pair.Player1.UserID, questionIDs); err != nil {
		s.logger.Warn().Err(err).Str("user_id", pair.Player1.UserID.String()).Msg("failed to save question history")
	}
	if err := s.questionSvc.AddUserQuestionHistory(ctx, pair.Player2.UserID, questionIDs); err != nil {
		s.logger.Warn().Err(err).Str("user_id", pair.Player2.UserID.String()).Msg("failed to save question history")
	}

	// Initialize player states
	now := time.Now()
	for _, player := range []queue.WaitingPlayer{pair.Player1, pair.Player2} {
		pgUserID := pgtype.UUID{}
		if err := pgUserID.Scan(player.UserID); err != nil {
			continue
		}

		state := PlayerState{
			MatchID:     matchID,
			UserID:      player.UserID,
			IsGuest:     player.IsGuest,
			Username:    player.Username,
			JoinedAt:    now,
			Status:      PlayerStatusQueued,
			Answers:     []AnswerRecord{},
		}

		if err := s.stateMgr.StorePlayerState(ctx, matchID, player.UserID, state); err != nil {
			s.logger.Warn().Err(err).Str("user_id", player.UserID.String()).Msg("failed to store initial state")
		}

		// Also persist to DB
		playerParams := sqlcgen.CreatePlayerMatchStateParams{
			MatchID: pgMatchID,
			UserID:  pgUserID,
			IsGuest: player.IsGuest,
			Status:  PlayerStatusQueued,
		}
		if err := s.matchRepo.UpsertPlayerState(ctx, playerParams); err != nil {
			s.logger.Warn().Err(err).Msg("failed to persist player state to DB")
		}
	}

	match := &Match{
		ID:                   matchID,
		Mode:                 ModeRandom1v1,
		QuestionCount:        questionCount,
		PerQuestionSeconds:   perQuestionSec,
		GlobalTimeoutSeconds: globalTimeout,
		SeedHash:             seedHash,
		LeaderboardEligible:  createParams.LeaderboardEligible,
		Status:               StatusPending,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	return match, packItems, nil
}

// CreatePrivateMatch creates a match from a private room.
func (s *Service) CreatePrivateMatch(ctx context.Context, roomCode string, players []RoomPlayer, questionCount int, perQuestionSec int, category string) (*Match, []QuestionPackItem, error) {
	matchID := uuid.New()
	seedHash := fmt.Sprintf("%s-%d", matchID.String(), time.Now().Unix())

	// Calculate global timeout
	globalTimeout := (questionCount * perQuestionSec) + 20 // padding

	// Create match record with room code in metadata
	pgMatchID := pgtype.UUID{}
	if err := pgMatchID.Scan(matchID); err != nil {
		return nil, nil, fmt.Errorf("scan uuid: %w", err)
	}

	pgHostID := pgtype.UUID{}
	if len(players) > 0 {
		if err := pgHostID.Scan(players[0].UserID); err != nil {
			return nil, nil, fmt.Errorf("scan host uuid: %w", err)
		}
	}

	// Store room code in metadata
	metadata := map[string]interface{}{
		"room_code": roomCode,
	}
	metadataJSON, _ := json.Marshal(metadata)

	createParams := sqlcgen.CreateMatchParams{
		Mode:                 ModePrivateRoom,
		QuestionCount:        int16(questionCount),
		PerQuestionSeconds:   int16(perQuestionSec),
		GlobalTimeoutSeconds: int16(globalTimeout),
		SeedHash:             seedHash,
		LeaderboardEligible:  true, // Private rooms can have leaderboards
		Status:               StatusPending,
		CreatedBy:            pgHostID,
		Metadata:             metadataJSON,
	}

	_, err := s.matchRepo.Create(ctx, createParams)
	if err != nil {
		return nil, nil, fmt.Errorf("create match: %w", err)
	}

	// Get fixed difficulty distribution based on question count
	diffCounts := getFixedDifficultyDistribution(questionCount)

	// Use category from request, default to "general" if empty
	if category == "" {
		category = "general"
	}
	
	// Private rooms: no cross-match uniqueness check
	packReq := question.PackRequest{
		Category:           category,
		DifficultyCounts:   diffCounts,
		TotalQuestions:     questionCount,
		Seed:               seedHash,
		PerQuestionSeconds: perQuestionSec,
		UserID:             nil, // No user history check for private rooms
		MatchMode:          ModePrivateRoom,
	}

	packResp, err := s.questionSvc.FetchPack(ctx, packReq)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch questions: %w", err)
	}

	// Convert to pack items with HMAC tokens
	packItems := make([]QuestionPackItem, len(packResp.Questions))
	for i, q := range packResp.Questions {
		token := s.signQuestionToken(q.ID, q.Answer)
		packItems[i] = QuestionPackItem{
			Order:         i + 1,
			ID:            q.ID,
			Prompt:        q.Prompt,
			Options:       q.Options,
			Token:         token,
			CorrectAnswer: q.Answer,
		}
	}

	// Store questions in Redis
	if err := s.stateMgr.StoreMatchQuestions(ctx, matchID, packItems); err != nil {
		s.logger.Warn().Err(err).Msg("failed to cache questions")
	}

	// Initialize player states
	now := time.Now()
	for _, player := range players {
		pgUserID := pgtype.UUID{}
		if err := pgUserID.Scan(player.UserID); err != nil {
			continue
		}

		state := PlayerState{
			MatchID:     matchID,
			UserID:      player.UserID,
			IsGuest:     player.IsGuest,
			Username:    player.Username,
			JoinedAt:    now,
			Status:      PlayerStatusQueued,
			Answers:     []AnswerRecord{},
		}

		if err := s.stateMgr.StorePlayerState(ctx, matchID, player.UserID, state); err != nil {
			s.logger.Warn().Err(err).Str("user_id", player.UserID.String()).Msg("failed to store initial state")
		}

		// Also persist to DB
		playerParams := sqlcgen.CreatePlayerMatchStateParams{
			MatchID: pgMatchID,
			UserID:  pgUserID,
			IsGuest: player.IsGuest,
			Status:  PlayerStatusQueued,
		}
		if err := s.matchRepo.UpsertPlayerState(ctx, playerParams); err != nil {
			s.logger.Warn().Err(err).Msg("failed to persist player state to DB")
		}
	}

	match := &Match{
		ID:                   matchID,
		Mode:                 ModePrivateRoom,
		QuestionCount:        questionCount,
		PerQuestionSeconds:   perQuestionSec,
		GlobalTimeoutSeconds: globalTimeout,
		SeedHash:             seedHash,
		LeaderboardEligible: true,
		Status:               StatusPending,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	return match, packItems, nil
}

// SubmitAnswer processes a player's answer with server-side validation and scoring.
func (s *Service) SubmitAnswer(ctx context.Context, matchID uuid.UUID, userID uuid.UUID, questionToken string, answer string, submittedAt time.Time) error {
	unlock, err := s.stateMgr.LockMatch(ctx, matchID)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer unlock()

	// Get player state
	state, err := s.stateMgr.GetPlayerState(ctx, matchID, userID)
	if err != nil || state == nil {
		return fmt.Errorf("player state not found")
	}

	// Get match questions
	questions, err := s.stateMgr.GetMatchQuestions(ctx, matchID)
	if err != nil || len(questions) == 0 {
		return fmt.Errorf("questions not found")
	}

	// Find question by token and validate
	var targetQuestion *QuestionPackItem
	var questionOrder int
	for _, q := range questions {
		if q.Token == questionToken {
			targetQuestion = &q
			questionOrder = q.Order
			break
		}
	}

	if targetQuestion == nil {
		return fmt.Errorf("invalid question token")
	}

	// Check if already answered
	for _, ans := range state.Answers {
		if ans.QuestionOrder == questionOrder {
			return fmt.Errorf("question already answered")
		}
	}

	// Validate answer
	isCorrect := answer == targetQuestion.CorrectAnswer

	// Get match config for per-question timeout
	perQuestionTimeout := 15 * time.Second // default fallback
	if meta, err := s.matchRepo.GetSummary(ctx, matchID); err == nil {
		perQuestionTimeout = time.Duration(meta.PerQuestionSeconds) * time.Second
	}

	// Count current streak
	streak := 0
	for i := len(state.Answers) - 1; i >= 0; i-- {
		if state.Answers[i].IsCorrect {
			streak++
		} else {
			break
		}
	}

	timeRemaining := perQuestionTimeout - time.Since(submittedAt)
	if timeRemaining < 0 {
		timeRemaining = 0
	}

	score := s.scoringEngine.CalculateScore(isCorrect, timeRemaining, perQuestionTimeout, streak)

	// Record answer
	answerRecord := AnswerRecord{
		QuestionOrder: questionOrder,
		QuestionToken: questionToken,
		Answer:        answer,
		SubmittedAt:   submittedAt,
		IsCorrect:     isCorrect,
		ScoreEarned:   score,
	}

	state.Answers = append(state.Answers, answerRecord)

	// Update state
	if err := s.stateMgr.StorePlayerState(ctx, matchID, userID, *state); err != nil {
		return fmt.Errorf("store state: %w", err)
	}

	s.logger.Info().
		Str("match_id", matchID.String()).
		Str("user_id", userID.String()).
		Int("question_order", questionOrder).
		Bool("correct", isCorrect).
		Int("score", score).
		Msg("answer submitted")

	return nil
}

// signQuestionToken creates HMAC-signed token for anti-cheat.
func (s *Service) signQuestionToken(questionID, correctAnswer string) string {
	if len(s.hmacKey) == 0 {
		return questionID
	}
	payload := fmt.Sprintf("%s:%s", questionID, correctAnswer)
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// FinalizeMatch computes final scores and updates DB.
// Returns match complete payload for WebSocket broadcast.
func (s *Service) FinalizeMatch(ctx context.Context, matchID uuid.UUID) (*ws.MatchCompletePayload, error) {
	unlock, err := s.stateMgr.LockMatch(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer unlock()

	var leaderboardEligible bool
	var isPrivateRoom bool
	var roomCode string
	if s.leaderboard != nil {
		if meta, err := s.matchRepo.GetSummary(ctx, matchID); err != nil {
			s.logger.Warn().Err(err).Str("match_id", matchID.String()).Msg("failed to load match summary for leaderboard")
		} else {
			leaderboardEligible = meta.LeaderboardEligible
			isPrivateRoom = meta.Mode == ModePrivateRoom
			
			// Extract room code from metadata if private room
			if isPrivateRoom && len(meta.Metadata) > 0 {
				var metadata map[string]interface{}
				if err := json.Unmarshal(meta.Metadata, &metadata); err == nil {
					if rc, ok := metadata["room_code"].(string); ok {
						roomCode = rc
					}
				}
			}
		}
	}

	// Get all player states
	states, err := s.stateMgr.GetAllPlayerStates(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get states: %w", err)
	}

	// Get match questions and config
	questions, err := s.stateMgr.GetMatchQuestions(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("get questions: %w", err)
	}

	// Get per-question timeout from match config
	perQuestionTimeout := 15 * time.Second // default fallback
	if meta, err := s.matchRepo.GetSummary(ctx, matchID); err == nil {
		perQuestionTimeout = time.Duration(meta.PerQuestionSeconds) * time.Second
	}
	totalQuestions := len(questions)

	var leaderboardReqs []leaderboard.RecordRequest

	// Finalize each player
	for _, state := range states {
		// Mark unanswered questions as incorrect
		answeredOrders := make(map[int]bool)
		for _, ans := range state.Answers {
			answeredOrders[ans.QuestionOrder] = true
		}

		// Add missing answers as incorrect
		for _, q := range questions {
			if !answeredOrders[q.Order] {
				state.Answers = append(state.Answers, AnswerRecord{
					QuestionOrder: q.Order,
					QuestionToken: q.Token,
					Answer:        "",
					SubmittedAt:   time.Now(),
					IsCorrect:     false,
					ScoreEarned:   0,
				})
			}
		}

		// Convert answers to scoring format
		scoringAnswers := make([]scoring.AnswerRecord, len(state.Answers))
		for idx, ans := range state.Answers {
			scoringAnswers[idx] = scoring.AnswerRecord{
				QuestionOrder: ans.QuestionOrder,
				QuestionToken: ans.QuestionToken,
				Answer:        ans.Answer,
				SubmittedAt:   ans.SubmittedAt,
				IsCorrect:     ans.IsCorrect,
				ScoreEarned:   ans.ScoreEarned,
			}
		}

		// Compute final score
		totalScore, accuracy, streakBonus := s.scoringEngine.ComputeFinalScore(scoringAnswers, perQuestionTimeout)
		correctCount := 0
		for _, ans := range state.Answers {
			if ans.IsCorrect {
				correctCount++
			}
		}

		state.FinalScore = &totalScore
		state.Accuracy = &accuracy
		state.StreakBonusPct = &streakBonus

		if state.LeftAt != nil {
			state.Status = PlayerStatusLeftEarly
		} else {
			state.Status = PlayerStatusCompleted
		}

		// Update DB
		pgMatchID := pgtype.UUID{}
		pgMatchID.Scan(matchID)
		pgUserID := pgtype.UUID{}
		pgUserID.Scan(state.UserID)

		// Convert to pgtype
		pgFinalScore := pgtype.Int4{}
		pgFinalScore.Scan(totalScore)
		pgAccuracy := pgtype.Numeric{}
		pgAccuracy.Scan(accuracy)
		pgStreakBonus := pgtype.Numeric{}
		pgStreakBonus.Scan(streakBonus)

		// Serialize answers
		answersJSON, _ := json.Marshal(state.Answers)

		updateParams := sqlcgen.UpdatePlayerMatchResultParams{
			MatchID:        pgMatchID,
			UserID:         pgUserID,
			FinalScore:     pgFinalScore,
			Accuracy:       pgAccuracy,
			StreakBonusPct: pgStreakBonus,
			Status:         state.Status,
			Answers:        answersJSON,
		}

		if err := s.matchRepo.FinalizePlayerState(ctx, updateParams); err != nil {
			s.logger.Warn().Err(err).Str("user_id", state.UserID.String()).Msg("failed to finalize player state")
		}

		// Update Redis state
		if err := s.stateMgr.StorePlayerState(ctx, matchID, state.UserID, state); err != nil {
			s.logger.Warn().Err(err).Msg("failed to update final state")
		}

		if leaderboardEligible && s.leaderboard != nil && !state.IsGuest {
			leaderboardReqs = append(leaderboardReqs, leaderboard.RecordRequest{
				UserID:        state.UserID,
				Username:      state.Username,
				Score:         totalScore,
				CorrectCount:  correctCount,
				QuestionCount: totalQuestions,
				MatchID:       matchID,
				Eligible:      true,
			})
		}
	}

	// Update match status
	pgMatchID := pgtype.UUID{}
	pgMatchID.Scan(matchID)
	updateParams := sqlcgen.UpdateMatchStatusParams{
		MatchID: pgMatchID,
		Status:  StatusCompleted,
	}
	if err := s.matchRepo.UpdateStatus(ctx, updateParams); err != nil {
		return nil, fmt.Errorf("update match status: %w", err)
	}

	if leaderboardEligible && s.leaderboard != nil && len(leaderboardReqs) > 0 {
		highest := leaderboardReqs[0].Score
		for _, req := range leaderboardReqs[1:] {
			if req.Score > highest {
				highest = req.Score
			}
		}
		for i := range leaderboardReqs {
			leaderboardReqs[i].Won = leaderboardReqs[i].Score == highest
			
			// Route to appropriate leaderboard based on match mode
			if isPrivateRoom && roomCode != "" {
				// Private room leaderboard (separate from main)
				if err := s.leaderboard.RecordPrivateRoomResult(ctx, roomCode, leaderboardReqs[i]); err != nil {
					s.logger.Warn().Err(err).
						Str("user_id", leaderboardReqs[i].UserID.String()).
						Str("room_code", roomCode).
						Msg("failed to record private room leaderboard result")
				}
			} else if meta, err := s.matchRepo.GetSummary(ctx, matchID); err == nil && meta.Mode == ModeRandom1v1 {
				// Main leaderboard (only for random 1v1)
				if err := s.leaderboard.RecordResult(ctx, leaderboardReqs[i]); err != nil {
					s.logger.Warn().Err(err).
						Str("user_id", leaderboardReqs[i].UserID.String()).
						Msg("failed to record leaderboard result")
				}
			}
		}
	}

	// Build match complete payload
	results := make([]ws.MatchResult, len(states))
	for i, state := range states {
		finalScore := 0
		if state.FinalScore != nil {
			finalScore = *state.FinalScore
		}
		accuracy := 0.0
		if state.Accuracy != nil {
			accuracy = *state.Accuracy
		}
		streakBonus := 0.0
		if state.StreakBonusPct != nil {
			streakBonus = *state.StreakBonusPct
		}

		results[i] = ws.MatchResult{
			UserID:             state.UserID.String(),
			Username:           state.Username,
			FinalScore:         finalScore,
			Accuracy:           accuracy,
			StreakBonusApplied: streakBonus,
			Status:             state.Status,
		}
	}

	// Get leaderboard position if eligible (simplified - would need actual lookup)
	leaderboardPosition := 0

	payload := &ws.MatchCompletePayload{
		MatchID:             matchID.String(),
		Results:             results,
		LeaderboardEligible: leaderboardEligible,
		LeaderboardPosition: leaderboardPosition,
	}

	return payload, nil
}

// CreateRoom creates a private room via RoomManager.
func (s *Service) CreateRoom(ctx context.Context, req PrivateRoomRequest) (string, *PrivateRoom, error) {
	return s.roomMgr.CreateRoom(ctx, req)
}

// GetRoom retrieves a private room by code.
func (s *Service) GetRoom(ctx context.Context, roomCode string) (*PrivateRoom, error) {
	return s.roomMgr.GetRoom(roomCode)
}
