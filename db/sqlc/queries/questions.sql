-- name: InsertQuestion :one
INSERT INTO questions (
    source,
    category,
    difficulty,
    type,
    prompt,
    options,
    correct_answer,
    metadata,
    verified
) VALUES (
    sqlc.arg(source),
    sqlc.arg(category),
    sqlc.arg(difficulty),
    sqlc.arg(type),
    sqlc.arg(prompt),
    sqlc.arg(options),
    sqlc.arg(correct_answer),
    COALESCE(sqlc.arg(metadata), '{}'::jsonb),
    sqlc.arg(verified)
)
RETURNING *;

-- name: GetQuestionPool :many
SELECT *
FROM questions
WHERE difficulty = ANY(sqlc.arg(difficulties)::text[])
  AND category = ANY(sqlc.arg(categories)::text[])
  AND verified = true
ORDER BY RANDOM()
LIMIT $1;

-- name: UpsertQuestionVerification :one
UPDATE questions
SET verified = sqlc.arg(verified),
    metadata = COALESCE(sqlc.arg(metadata), metadata),
    updated_at = NOW()
WHERE question_id = sqlc.arg(question_id)
RETURNING *;

