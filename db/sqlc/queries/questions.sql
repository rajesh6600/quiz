-- name: InsertQuestion :one
INSERT INTO questions (
    source,
    prompt,
    options,
    correct_answer,
    metadata,
    verified
) VALUES (
    sqlc.arg(source),
    sqlc.arg(prompt),
    sqlc.arg(options),
    sqlc.arg(correct_answer),
    COALESCE(sqlc.arg(metadata), '{}'::jsonb),
    sqlc.arg(verified)
)
RETURNING *;

-- name: GetQuestionPool :many
SELECT question_id, source, prompt, options, correct_answer, metadata, verified, created_at, updated_at
FROM questions
WHERE verified = true
ORDER BY RANDOM()
LIMIT $1;

-- name: UpsertQuestionVerification :one
UPDATE questions
SET verified = sqlc.arg(verified),
    metadata = COALESCE(sqlc.arg(metadata), metadata),
    updated_at = NOW()
WHERE question_id = sqlc.arg(question_id)
RETURNING question_id, source, prompt, options, correct_answer, metadata, verified, created_at, updated_at;

