-- Managed coding-agent hooks

-- name: ListHooksByWorkspace :many
SELECT * FROM hook
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: GetHook :one
SELECT * FROM hook WHERE id = $1;

-- name: GetHookInWorkspace :one
SELECT * FROM hook WHERE id = $1 AND workspace_id = $2;

-- name: CreateHook :one
INSERT INTO hook (workspace_id, name, description, command, providers, events, matcher, config, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateHook :one
UPDATE hook SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    command = COALESCE(sqlc.narg('command'), command),
    providers = COALESCE(sqlc.narg('providers'), providers),
    events = COALESCE(sqlc.narg('events'), events),
    matcher = COALESCE(sqlc.narg('matcher'), matcher),
    config = COALESCE(sqlc.narg('config'), config),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteHook :exec
DELETE FROM hook WHERE id = $1 AND workspace_id = $2;

-- Agent-Hook junction

-- name: ListAgentHooks :many
SELECT h.* FROM hook h
JOIN agent_hook ah ON ah.hook_id = h.id
WHERE ah.agent_id = $1 AND ah.enabled = TRUE
ORDER BY h.name ASC;

-- name: ListAgentHookSummaries :many
SELECT h.id, h.workspace_id, h.name, h.description, h.command, h.providers,
       h.events, h.matcher, h.config, h.created_by, h.created_at, h.updated_at,
       ah.enabled
FROM hook h
JOIN agent_hook ah ON ah.hook_id = h.id
WHERE ah.agent_id = $1
ORDER BY h.name ASC;

-- name: AddAgentHook :exec
INSERT INTO agent_hook (agent_id, hook_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: SetAgentHookEnabled :execrows
UPDATE agent_hook SET enabled = $3
WHERE agent_id = $1 AND hook_id = $2;

-- name: RemoveAgentHook :exec
DELETE FROM agent_hook WHERE agent_id = $1 AND hook_id = $2;

-- name: RemoveAllAgentHooks :exec
DELETE FROM agent_hook WHERE agent_id = $1;

-- name: ListAgentHooksByWorkspace :many
SELECT ah.agent_id, h.id, h.name, h.description, ah.enabled
FROM agent_hook ah
JOIN hook h ON h.id = ah.hook_id
WHERE h.workspace_id = $1
ORDER BY h.name ASC;
