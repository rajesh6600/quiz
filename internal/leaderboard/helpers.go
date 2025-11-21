package leaderboard

import ws "github.com/gokatarajesh/quiz-platform/pkg/http/ws"

func toWSEntries(entries []Entry) []ws.LeaderboardEntry {
	result := make([]ws.LeaderboardEntry, len(entries))
	for i, e := range entries {
		result[i] = ws.LeaderboardEntry{
			Rank:        i + 1,
			UserID:      e.UserID.String(),
			DisplayName: e.DisplayName,
			Score:       e.Score,
			Wins:        e.Wins,
			Games:       e.Games,
			Accuracy:    e.Accuracy,
		}
	}
	return result
}

