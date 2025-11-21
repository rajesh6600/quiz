-- name: InsertLeaderboardSnapshot :one
INSERT INTO leaderboard_snapshots (
    time_window,
    generated_at,
    entries,
    source_hash
) VALUES (
    sqlc.arg(time_window),
    sqlc.arg(generated_at),
    sqlc.arg(entries),
    sqlc.arg(source_hash)
)
RETURNING *;

-- name: ListRecentSnapshots :many
SELECT *
FROM leaderboard_snapshots
WHERE time_window = $1
ORDER BY generated_at DESC
LIMIT $2;

