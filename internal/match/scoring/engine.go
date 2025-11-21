package scoring

import (
	"time"
)

// ScoringConfig holds configurable scoring constants (defaults match requirements).
type ScoringConfig struct {
	BaseScore          int     // default: 100
	MaxTimeBonus       int     // default: 50
	StreakBonusPercent float64 // default: 0.05 (5% per consecutive correct, cap +50%)
	MaxStreakBonus     float64 // default: 0.50 (50% cap)
}

// DefaultScoringConfig returns production defaults.
func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		BaseScore:          100,
		MaxTimeBonus:       50,
		StreakBonusPercent: 0.05,
		MaxStreakBonus:     0.50,
	}
}

// Engine computes server-side scores with configurable constants.
type Engine struct {
	config ScoringConfig
}

// NewEngine creates a scoring engine with the provided config.
func NewEngine(config ScoringConfig) *Engine {
	return &Engine{config: config}
}

// CalculateScore computes points for a single answer.
// Formula: base + time_bonus + streak_bonus
// - base: always awarded if correct
// - time_bonus: max when answered instantly, decays linearly to 0 at timeout
// - streak_bonus: percentage of base, increases with consecutive correct answers
func (e *Engine) CalculateScore(
	isCorrect bool,
	timeRemaining time.Duration,
	perQuestionTimeout time.Duration,
	currentStreak int,
) int {
	if !isCorrect {
		return 0
	}

	score := e.config.BaseScore

	// Time bonus: linear decay from max to 0
	if perQuestionTimeout > 0 {
		timeRatio := float64(timeRemaining) / float64(perQuestionTimeout)
		if timeRatio > 1.0 {
			timeRatio = 1.0
		}
		if timeRatio < 0.0 {
			timeRatio = 0.0
		}
		timeBonus := int(float64(e.config.MaxTimeBonus) * timeRatio)
		score += timeBonus
	}

	// Streak bonus: percentage of base, capped
	if currentStreak > 0 {
		streakMultiplier := float64(currentStreak) * e.config.StreakBonusPercent
		if streakMultiplier > e.config.MaxStreakBonus {
			streakMultiplier = e.config.MaxStreakBonus
		}
		streakBonus := int(float64(e.config.BaseScore) * streakMultiplier)
		score += streakBonus
	}

	return score
}

// AnswerRecord represents a single answer for scoring (duplicated here to avoid import cycle).
type AnswerRecord struct {
	QuestionOrder int       `json:"question_order"`
	QuestionToken  string    `json:"question_token"`
	Answer         string    `json:"answer"`
	SubmittedAt    time.Time `json:"submitted_at"`
	IsCorrect      bool      `json:"is_correct"`
	ScoreEarned    int       `json:"score_earned"`
}

// ComputeFinalScore aggregates all answers and returns total + accuracy + streak bonus percentage.
func (e *Engine) ComputeFinalScore(
	answers []AnswerRecord,
	perQuestionTimeout time.Duration,
) (totalScore int, accuracy float64, streakBonusPct float64) {
	if len(answers) == 0 {
		return 0, 0.0, 0.0
	}

	correctCount := 0
	currentStreak := 0
	maxStreak := 0

	for i, ans := range answers {
		if ans.IsCorrect {
			correctCount++
			currentStreak++
			if currentStreak > maxStreak {
				maxStreak = currentStreak
			}
		} else {
			currentStreak = 0
		}

		// Recalculate with actual streak at submission time
		// For simplicity, we use the streak up to this question
		streakAtAnswer := 0
		for j := i - 1; j >= 0; j-- {
			if answers[j].IsCorrect {
				streakAtAnswer++
			} else {
				break
			}
		}
		if ans.IsCorrect {
			streakAtAnswer++
		}

		timeRemaining := perQuestionTimeout - time.Since(ans.SubmittedAt)
		if timeRemaining < 0 {
			timeRemaining = 0
		}

		score := e.CalculateScore(ans.IsCorrect, timeRemaining, perQuestionTimeout, streakAtAnswer)
		totalScore += score
	}

	accuracy = float64(correctCount) / float64(len(answers))

	// Calculate final streak bonus percentage applied
	if maxStreak > 0 {
		streakMultiplier := float64(maxStreak) * e.config.StreakBonusPercent
		if streakMultiplier > e.config.MaxStreakBonus {
			streakMultiplier = e.config.MaxStreakBonus
		}
		streakBonusPct = streakMultiplier
	}

	return totalScore, accuracy, streakBonusPct
}
