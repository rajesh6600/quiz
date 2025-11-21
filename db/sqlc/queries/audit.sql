-- name: InsertAuditLog :exec
INSERT INTO audit_logs (
    actor_id,
    entity_type,
    entity_id,
    action,
    payload
) VALUES (
    sqlc.arg(actor_id),
    sqlc.arg(entity_type),
    sqlc.arg(entity_id),
    sqlc.arg(action),
    sqlc.arg(payload)
);

-- name: ListAuditLogsForEntity :many
SELECT *
FROM audit_logs
WHERE entity_type = $1
  AND entity_id = $2
ORDER BY created_at DESC
LIMIT $3;

