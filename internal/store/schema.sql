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
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    worktree_path TEXT,
    branch TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    provider TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    stop_reason TEXT NOT NULL DEFAULT '',
    err_msg TEXT NOT NULL DEFAULT ''
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

CREATE INDEX IF NOT EXISTS idx_routing_decisions_session_key ON routing_decisions(session_key);
CREATE INDEX IF NOT EXISTS idx_quota_snapshots_account_id ON quota_snapshots(account_id);
CREATE INDEX IF NOT EXISTS idx_workspace_accounts_account_id ON workspace_accounts(account_id);
CREATE INDEX IF NOT EXISTS idx_spaces_workspace_id ON spaces(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_workspace_id ON tasks(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_space_id ON tasks(space_id);
CREATE INDEX IF NOT EXISTS idx_runs_task_id ON runs(task_id);
CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at);
CREATE INDEX IF NOT EXISTS idx_run_events_run_id ON run_events(run_id);
CREATE INDEX IF NOT EXISTS idx_terminal_sessions_task_id ON terminal_sessions(task_id);
