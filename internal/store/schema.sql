CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    label TEXT NOT NULL,
    credential_type TEXT NOT NULL,
    credential_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS routing_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    policy TEXT NOT NULL,
    decided_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS quota_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    usage_data TEXT NOT NULL,
    polled_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    title TEXT NOT NULL,
    routing_policy TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_accounts (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    PRIMARY KEY (workspace_id, account_id)
);

CREATE TABLE IF NOT EXISTS spaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    title TEXT NOT NULL,
    env_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    space_id INTEGER REFERENCES spaces(id),
    parent_task_id INTEGER REFERENCES tasks(id),
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    worktree_path TEXT,
    branch TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS chats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    title TEXT NOT NULL DEFAULT '',
    provider TEXT,
    agent_session TEXT,
    created_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    chat_id INTEGER REFERENCES chats(id),
    provider TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    stop_reason TEXT NOT NULL DEFAULT '',
    err_msg TEXT NOT NULL DEFAULT '',
    approval_policy TEXT NOT NULL DEFAULT 'manual',
    permission_mode TEXT NOT NULL DEFAULT '',
    auto_accept INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS run_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL REFERENCES runs(id),
    seq INTEGER NOT NULL,
    event_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (run_id, seq)
);

CREATE TABLE IF NOT EXISTS terminal_sessions (
    id TEXT PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    status TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    closed_at TIMESTAMP,
    scrollback TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    approval_policy TEXT NOT NULL DEFAULT '',
    permission_mode TEXT NOT NULL DEFAULT '',
    auto_accept INTEGER NOT NULL DEFAULT 0,
    thinking_level TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS mcp_servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,     -- human label and the wire-identity key on
                                   -- every backend (ACP's McpServerStdio.name,
                                   -- Claude's mcpServers map key) -- ADR-0018,
                                   -- "Name is the join key, not id"
    transport TEXT NOT NULL,       -- 'stdio' | 'http' | 'sse'
    command TEXT NOT NULL DEFAULT '',  -- stdio only: absolute path or bare
                                       -- command to resolve via exec.LookPath
    args TEXT NOT NULL DEFAULT '[]',   -- stdio only: JSON array of strings
    env TEXT NOT NULL DEFAULT '{}',    -- stdio only: JSON object, string->string;
                                       -- secret-bearing (ADR-0018 "Secrets")
    url TEXT NOT NULL DEFAULT '',      -- http/sse only
    headers TEXT NOT NULL DEFAULT '{}',-- http/sse only: JSON object,
                                       -- string->string; secret-bearing
    enabled INTEGER NOT NULL DEFAULT 1,-- disable without deleting
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS workspace_mcp_servers (
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    mcp_server_id INTEGER NOT NULL REFERENCES mcp_servers(id),
    PRIMARY KEY (workspace_id, mcp_server_id)
);

CREATE TABLE IF NOT EXISTS request_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at TIMESTAMP NOT NULL,
    provider TEXT NOT NULL,
    account_id INTEGER REFERENCES accounts(id),
    session_key TEXT NOT NULL,
    model TEXT,
    stream INTEGER,
    status INTEGER NOT NULL,
    upstream_status INTEGER,
    outcome TEXT NOT NULL,
    error TEXT,
    ttfb_ms INTEGER,
    duration_ms INTEGER NOT NULL,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cache_read_tokens INTEGER,
    cache_write_tokens INTEGER,
    reasoning_tokens INTEGER
);

CREATE INDEX IF NOT EXISTS idx_routing_decisions_session_key ON routing_decisions(session_key);
CREATE INDEX IF NOT EXISTS idx_quota_snapshots_account_id ON quota_snapshots(account_id);
CREATE INDEX IF NOT EXISTS idx_workspace_accounts_account_id ON workspace_accounts(account_id);
CREATE INDEX IF NOT EXISTS idx_workspace_mcp_servers_mcp_server_id ON workspace_mcp_servers(mcp_server_id);
CREATE INDEX IF NOT EXISTS idx_spaces_workspace_id ON spaces(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_id ON tasks(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_space_id ON tasks(space_id);
-- idx_tasks_parent_task_id is created by the tasks.parent_task_id migration,
-- not here, for the same reason idx_runs_chat_id isn't: schema.sql runs
-- before migrations, so an index on the column would fail against a
-- pre-existing database whose tasks table doesn't have it yet.
CREATE INDEX IF NOT EXISTS idx_chats_task_id ON chats(task_id);
CREATE INDEX IF NOT EXISTS idx_runs_task_id ON runs(task_id);
CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at);
-- idx_runs_chat_id is created by the chats.default_chat_backfill migration,
-- not here: this file is applied in full (via one db.Exec) before any
-- migration runs, so an index on runs.chat_id would fail against a
-- pre-existing database whose runs table doesn't have that column yet --
-- CREATE TABLE IF NOT EXISTS runs above is a no-op for it, since the table
-- already exists.
CREATE INDEX IF NOT EXISTS idx_run_events_run_id ON run_events(run_id);
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_task_id ON terminal_sessions(task_id);
CREATE INDEX IF NOT EXISTS idx_request_log_started_at ON request_log(started_at);
CREATE INDEX IF NOT EXISTS idx_request_log_account_id ON request_log(account_id);
