-- Workspaces group bots (and hosted sites) for a person or a team. Every
-- account has exactly one personal workspace; members hold a role that grants
-- access to everything in the workspace. Per-bot sharing (bot_subusers) keeps
-- working alongside and can only add access, never remove it.
CREATE TABLE workspaces (
    id                  TEXT PRIMARY KEY NOT NULL,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    owner_id            TEXT NOT NULL
                        REFERENCES users(id) ON DELETE CASCADE,
    personal            INTEGER NOT NULL DEFAULT 0 CHECK (personal IN (0, 1)),
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX workspaces_personal_idx ON workspaces(owner_id) WHERE personal = 1;
CREATE INDEX workspaces_owner_idx ON workspaces(owner_id);

CREATE TABLE workspace_members (
    workspace_id        TEXT NOT NULL
                        REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id             TEXT NOT NULL
                        REFERENCES users(id) ON DELETE CASCADE,
    role                TEXT NOT NULL
                        CHECK (role IN ('owner', 'admin', 'developer', 'viewer')),
    added_by            TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at_ms       INTEGER NOT NULL,
    updated_at_ms       INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, user_id)
) STRICT;

CREATE INDEX workspace_members_user_idx ON workspace_members(user_id);

-- One personal workspace per existing account (random version-4 UUIDs).
INSERT INTO workspaces (id, name, owner_id, personal, created_at_ms, updated_at_ms)
SELECT lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' ||
       substr(lower(hex(randomblob(2))), 2) || '-' || substr('89ab', 1 + (abs(random()) % 4), 1) ||
       substr(lower(hex(randomblob(2))), 2) || '-' || lower(hex(randomblob(6))),
       'Personal', id, 1, created_at_ms, created_at_ms
FROM users;

INSERT INTO workspace_members (workspace_id, user_id, role, created_at_ms, updated_at_ms)
SELECT id, owner_id, 'owner', created_at_ms, created_at_ms FROM workspaces;

-- Every bot lives in a workspace; existing bots join their owner's.
ALTER TABLE bots ADD COLUMN workspace_id TEXT REFERENCES workspaces(id);
UPDATE bots SET workspace_id = (SELECT w.id FROM workspaces w WHERE w.owner_id = bots.owner_id AND w.personal = 1);
CREATE INDEX bots_workspace_idx ON bots(workspace_id);
