-- Managed coding-agent hooks. Hooks are workspace resources, like Skills, and
-- are materialized into a provider's task-local configuration by the daemon.
-- V1 accepts Codex only; providers is kept as an array so the resource can
-- grow without changing the relationship model.
CREATE TABLE hook (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    command TEXT NOT NULL,
    providers TEXT[] NOT NULL DEFAULT ARRAY['codex']::TEXT[],
    events TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    matcher TEXT NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name),
    CHECK (char_length(name) BETWEEN 1 AND 128),
    CHECK (char_length(command) BETWEEN 1 AND 4096)
);

CREATE TABLE agent_hook (
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    hook_id UUID NOT NULL REFERENCES hook(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, hook_id)
);

CREATE INDEX idx_hook_workspace ON hook(workspace_id);
CREATE INDEX idx_agent_hook_hook ON agent_hook(hook_id);
CREATE INDEX idx_agent_hook_agent ON agent_hook(agent_id);
