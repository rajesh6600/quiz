-- name: CreateMatch :one
INSERT INTO matches (
    mode,
    question_count,
    per_question_seconds,
    global_timeout_seconds,
    seed_hash,
    leaderboard_eligible,
    status,
    created_by,
    metadata
) VALUES (
    sqlc.arg(mode),
    sqlc.arg(question_count),
    sqlc.arg(per_question_seconds),
    sqlc.arg(global_timeout_seconds),
    sqlc.arg(seed_hash),
    sqlc.arg(leaderboard_eligible),
    sqlc.arg(status),
    sqlc.arg(created_by),
    COALESCE(sqlc.arg(metadata), '{}'::jsonb)
)
RETURNING *;

-- name: UpdateMatchStatus :exec
UPDATE matches
SET status = sqlc.arg(status),
    started_at = COALESCE(sqlc.arg(started_at), started_at),
    completed_at = COALESCE(sqlc.arg(completed_at), completed_at),
    updated_at = NOW()
WHERE match_id = sqlc.arg(match_id);

-- name: GetMatchForSummary :one
SELECT *
FROM matches
WHERE match_id = $1;

-- name: CreatePlayerMatchState :exec
INSERT INTO player_match_state (
    match_id,
    user_id,
    is_guest,
    status,
    answers
) VALUES (
    sqlc.arg(match_id),
    sqlc.arg(user_id),
    sqlc.arg(is_guest),
    sqlc.arg(status),
    COALESCE(sqlc.arg(answers), '[]'::jsonb)
)
ON CONFLICT (match_id, user_id) DO NOTHING;

-- name: UpdatePlayerMatchResult :exec
UPDATE player_match_state
SET final_score = sqlc.arg(final_score),
    status = sqlc.arg(status),
    accuracy = sqlc.arg(accuracy),
    streak_bonus_pct = sqlc.arg(streak_bonus_pct),
    left_at = COALESCE(sqlc.arg(left_at), left_at),
    answers = COALESCE(sqlc.arg(answers), answers),
    updated_at = NOW()
WHERE match_id = sqlc.arg(match_id)
  AND user_id = sqlc.arg(user_id);

-- name: GetPlayerStatesByMatch :many
SELECT *
FROM player_match_state
WHERE match_id = $1;

