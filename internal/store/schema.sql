CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL,
    label TEXT NOT NULL,
    credential_type TEXT NOT NULL,
    credential_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    -- JSON array of path.Match globs this account serves (ADR-0020 §5);
    -- NULL = no list.
    models TEXT,
    -- Hashed vendor account identity (ADR-0033 D6); NULL = none (API-key and
    -- pasted credentials, and OAuth rows not yet re-logged-in). The partial
    -- unique index lives in migrate.go because it needs the column to exist
    -- on older databases first.
    identity TEXT
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
    expires_at TIMESTAMP NOT NULL,
    kind TEXT NOT NULL DEFAULT 'fetched'
);

CREATE TABLE IF NOT EXISTS workspaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    title TEXT NOT NULL,
    routing_policy TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    setup_script TEXT,
    teardown_script TEXT,
    learning_mode TEXT NOT NULL DEFAULT 'off', -- 'off' | 'propose' (ADR-0026)
    default_base TEXT -- ADR-0039: '' / NULL = auto-detect
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
    archived_at TIMESTAMP,
    setup_status TEXT,
    setup_error TEXT,
    setup_log TEXT,
    port INTEGER,
    kind TEXT NOT NULL DEFAULT 'task', -- 'task' | 'system' (ADR-0026)
    base_ref TEXT, -- ADR-0039: branch the task is based on / its PR targets
    base_sha TEXT, -- ADR-0039: commit the task branch forked from (diff base)
    branch_owned INTEGER NOT NULL DEFAULT 0, -- ADR-0039: 1 when CreateTask created the branch
    pr_forge TEXT, -- ADR-0039 section 4: the task's PR identity (forge name,
    pr_repo TEXT, --   owner/name, number, validated URL, head branch)
    pr_number INTEGER,
    pr_url TEXT,
    pr_head_ref TEXT,
    untrusted_source INTEGER NOT NULL DEFAULT 0 -- ADR-0039 section 7: 1 = fork PR / unknown forge; setup blocked until the user runs it
);

CREATE TABLE IF NOT EXISTS chats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL REFERENCES tasks(id),
    title TEXT NOT NULL DEFAULT '',
    provider TEXT,
    agent_session TEXT,
    created_at TIMESTAMP NOT NULL,
    archived_at TIMESTAMP,
    learn_watermark INTEGER NOT NULL DEFAULT 0,
    learn_pending INTEGER NOT NULL DEFAULT 0
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
    auto_accept INTEGER NOT NULL DEFAULT 0,
    model TEXT, -- requested model (ADR-0025); NULL = provider default
    -- O1 provenance: worktree HEAD at start (base_sha) and at the terminal
    -- state (head_sha), plus dirty at the latest successful capture. NULL =
    -- unknown (git failed or pre-dates O1).
    base_sha TEXT,
    head_sha TEXT,
    dirty INTEGER,
    purpose TEXT NOT NULL DEFAULT 'user', -- 'user' | 'reflect' | 'curate' | 'probe' (ADR-0026, ADR-0037)
    origin TEXT NOT NULL DEFAULT '', -- who authored the prompt: human|orchestrator|agent; '' = unknown
    interrupt INTEGER NOT NULL DEFAULT 0, -- 1 = delivered as a mid-run interrupt
    attachments TEXT NOT NULL DEFAULT '' -- attach.EncodeSet of the prompt's attachments (ADR-0038); '' = none
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
    model TEXT, -- requested model (ADR-0025); NULL = provider default
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

CREATE TABLE IF NOT EXISTS chat_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id INTEGER NOT NULL REFERENCES chats(id),
    prompt TEXT NOT NULL,
    run_config TEXT NOT NULL DEFAULT '{}', -- JSON: provider, permissionMode, autoAccept, thinkingLevel, model
    source TEXT NOT NULL,                  -- 'human' | 'orchestrator' | 'agent'
    from_task_id INTEGER,                  -- sender, when source='agent'
    from_chat_id INTEGER,
    priority INTEGER NOT NULL DEFAULT 0,   -- 1 for interrupt items (delivered first)
    status TEXT NOT NULL,                  -- 'queued' | 'delivered' | 'cancelled'
    run_id TEXT,                           -- set on delivery
    cancel_reason TEXT NOT NULL DEFAULT '',
    cancel_kind TEXT NOT NULL DEFAULT '',  -- 'user' | 'auto' once cancelled
    attachments TEXT NOT NULL DEFAULT '',  -- attach.EncodeSet of the prompt's attachments (ADR-0038)
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_queue_chat_status ON chat_queue(chat_id, status, priority DESC, id);

-- run_usage: one row per run of what the agent itself reported (ADR-0020).
-- Additive; NULL means "not reported", never estimated. workspace_id is
-- denormalized from tasks at upsert time. No prompt text, no secrets.
CREATE TABLE IF NOT EXISTS run_usage (
    run_id TEXT PRIMARY KEY REFERENCES runs(id),
    workspace_id INTEGER,
    task_id INTEGER NOT NULL,
    chat_id INTEGER,
    provider TEXT NOT NULL,
    model TEXT,
    input_tokens INTEGER,
    cached_input_tokens INTEGER,
    cache_write_tokens INTEGER,
    output_tokens INTEGER,
    reasoning_tokens INTEGER,
    cost_usd REAL,
    context_used INTEGER,
    context_size INTEGER,
    session_snapshot TEXT, -- JSON, ACP only
    source TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_usage_task_id ON run_usage(task_id);

-- quota_poll_state: the call-budget ledger for the usage poller, one row per
-- account (ADR-0028 §3 "Call budget"). Additive: CREATE TABLE IF NOT EXISTS
-- applies it to existing databases on the next Open, so no migrate.go entry.
CREATE TABLE IF NOT EXISTS quota_poll_state (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id),
    last_attempt_at TIMESTAMP,
    next_allowed_at TIMESTAMP,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    forced_at TEXT NOT NULL DEFAULT '[]',
    rejected_fp TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT ''
);

-- quota_cooldown: passive rejection cooldowns (ADR-0028 §7), one row per
-- account and scope. scope_model '' is a credential-wide cooldown; a model id
-- scopes it to that model. Additive: CREATE TABLE IF NOT EXISTS applies it to
-- existing databases on the next Open; rejected_window (the window whose
-- rejection set it, display only) was added later, so it also has a migrate.go
-- entry for databases that already had the table.
CREATE TABLE IF NOT EXISTS quota_cooldown (
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    scope_model TEXT NOT NULL DEFAULT '',
    until TIMESTAMP NOT NULL,
    reason TEXT NOT NULL,
    level INTEGER NOT NULL DEFAULT 0,
    rejected_window TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMP NOT NULL,
    PRIMARY KEY (account_id, scope_model)
);

-- Self-learning skills (ADR-0026): learned skills, their version history,
-- an append-only ledger, pending patch/bundle proposals, and reflection/
-- curation jobs. Additive tables: CREATE TABLE IF NOT EXISTS applies them
-- to existing databases on the next Open. New columns on existing tables
-- (workspaces.learning_mode, chats.learn_watermark/learn_pending,
-- runs.purpose, tasks.kind) are added both here and by the learning_columns
-- migration in migrate.go.

CREATE TABLE IF NOT EXISTS learned_skills (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL,                 -- active|disabled|archived (ADR-0026; proposals live in learned_skill_proposals)
    pinned INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL,                 -- reflector|curator|human (who proposed it)
    created_by_run TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '[]',  -- redacted JSON array
    possible_duplicate_of TEXT NOT NULL DEFAULT '',
    similarity REAL NOT NULL DEFAULT 0,
    version INTEGER NOT NULL DEFAULT 1,
    offered_runs INTEGER NOT NULL DEFAULT 0,
    loaded_runs INTEGER NOT NULL DEFAULT 0,
    loads INTEGER NOT NULL DEFAULT 0,
    loads_via_skill INTEGER NOT NULL DEFAULT 0,
    loads_via_read INTEGER NOT NULL DEFAULT 0,
    patched INTEGER NOT NULL DEFAULT 0,
    reused_after_patch INTEGER NOT NULL DEFAULT 0,
    measured INTEGER NOT NULL DEFAULT 0,
    last_offered_at TIMESTAMP,
    last_loaded_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_learned_skills_ws_name_live ON learned_skills(workspace_id, name) WHERE status IN ('active','disabled');
CREATE INDEX IF NOT EXISTS idx_learned_skills_ws_status ON learned_skills(workspace_id, status);

CREATE TABLE IF NOT EXISTS learned_skill_versions (
    skill_id INTEGER NOT NULL REFERENCES learned_skills(id),
    version INTEGER NOT NULL,
    description TEXT NOT NULL,
    body TEXT NOT NULL,
    actor TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (skill_id, version)
);

CREATE TABLE IF NOT EXISTS learned_skill_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    skill_id INTEGER,                     -- no FK: survives skill delete
    skill_name TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,                 -- create|patch|merge|archive|restore|reject|delete|disable|enable|pin|unpin|undo
    reason TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '[]',
    absorbed_into TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,                  -- reflector|curator|human|system
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_learned_skill_ledger_skill ON learned_skill_ledger(workspace_id, skill_id, id);

-- ALL proposals (create, patch, bundle) pending human approval.
-- learned_skills holds only approved skills. Rejected proposals stay
-- (negative examples for the reflector).
CREATE TABLE IF NOT EXISTS learned_skill_proposals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    kind TEXT NOT NULL,                   -- create|patch|bundle
    source TEXT NOT NULL,                 -- reflector|curator
    name TEXT NOT NULL DEFAULT '',        -- create: proposed name; patch: target name; bundle: umbrella name
    target_skill_id INTEGER,              -- patch/bundle target; NULL for create
    job_id INTEGER,
    intents TEXT NOT NULL,                -- JSON array of validated intents
    evidence TEXT NOT NULL DEFAULT '[]',
    possible_duplicate_of TEXT NOT NULL DEFAULT '',
    similarity REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL,                 -- pending|approved|rejected
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    decided_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_learned_skill_proposals_ws ON learned_skill_proposals(workspace_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_learned_skill_proposals_ws_pending_create ON learned_skill_proposals(workspace_id, name) WHERE status = 'pending' AND kind = 'create';

CREATE TABLE IF NOT EXISTS learning_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    chat_id INTEGER,
    kind TEXT NOT NULL,                   -- reflect|curate
    trigger_kind TEXT NOT NULL,           -- cadence|explicit|tail|schedule
    window_from_event_id INTEGER NOT NULL DEFAULT 0,
    window_to_event_id INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,                 -- queued|running|landed|empty|failed|skipped
    skip_reason TEXT NOT NULL DEFAULT '',
    verdicts TEXT NOT NULL DEFAULT '[]',
    run_id TEXT NOT NULL DEFAULT '',
    cost_usd REAL,
    tokens INTEGER,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_learning_jobs_ws ON learning_jobs(workspace_id, id);

-- Learning probes (ADR-0037): one A/A2/B experiment before a skill
-- proposal is approved. Contrast JSON, arm metrics and check results are
-- stored uninterpreted; the verdict is decided by internal/learning/probe.
CREATE TABLE IF NOT EXISTS learning_probes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    proposal_id INTEGER NOT NULL,         -- no FK: forget-workspace deletes both
    quick INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,                 -- queued|running|done|failed|cancelled|unsafe
    verdict TEXT NOT NULL DEFAULT '',     -- helped|no_effect|never_loaded|hurt|inconclusive|probe_failed|unsafe ('' until decided)
    verdict_reason TEXT NOT NULL DEFAULT '',
    est_usd REAL NOT NULL DEFAULT 0,
    canary_cost_usd REAL NOT NULL DEFAULT 0,
    source_run_id TEXT NOT NULL DEFAULT '',
    base_sha TEXT NOT NULL DEFAULT '',
    contrast TEXT NOT NULL DEFAULT '',    -- JSON, uninterpreted by the store
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_learning_probes_ws ON learning_probes(workspace_id, id);
CREATE INDEX IF NOT EXISTS idx_learning_probes_proposal ON learning_probes(proposal_id, id);

CREATE TABLE IF NOT EXISTS learning_probe_arms (
    probe_id INTEGER NOT NULL REFERENCES learning_probes(id),
    role TEXT NOT NULL,                   -- A|A2|B
    seq INTEGER NOT NULL,                 -- execution order, 0-based
    status TEXT NOT NULL,                 -- pending|running|done|capped|timeout|turns|output|killed|error
    run_id TEXT NOT NULL DEFAULT '',
    projection_hash TEXT NOT NULL DEFAULT '',
    metrics TEXT NOT NULL DEFAULT '{}',   -- JSON, uninterpreted
    diff TEXT NOT NULL DEFAULT '',        -- redacted, clipped by the caller (<= 64 KB)
    final_message TEXT NOT NULL DEFAULT '', -- redacted (<= 8 KB)
    check_result TEXT NOT NULL DEFAULT '', -- JSON or ''
    cost_usd REAL NOT NULL DEFAULT 0,
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    PRIMARY KEY (probe_id, role)
);

-- Delivery counters (ADR-0026 section 6): one row per (skill, run) offer and
-- per (skill, run, via) load, so the learned_skills counters are idempotent
-- under re-delivered run-finished notifications.
CREATE TABLE IF NOT EXISTS learned_skill_offers (
    skill_id INTEGER NOT NULL REFERENCES learned_skills(id),
    run_id TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (skill_id, run_id)
);

CREATE TABLE IF NOT EXISTS learned_skill_loads (
    skill_id INTEGER NOT NULL REFERENCES learned_skills(id),
    run_id TEXT NOT NULL,
    via TEXT NOT NULL,                    -- skill|read
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (skill_id, run_id, via)
);

-- Library snapshots (ADR-0026 section 6): a JSON export of a workspace's
-- learned skills and versions taken before each curator run; 5 kept.
CREATE TABLE IF NOT EXISTS learning_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
    created_at TIMESTAMP NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_learning_snapshots_ws ON learning_snapshots(workspace_id, id);

-- task_pr_state: the forge poller's per-task cache and call ledger (ADR-0039
-- section 2). snapshot is the forge.PR JSON ('' = none yet). Additive, like
-- quota_poll_state; DeleteTask removes the row.
CREATE TABLE IF NOT EXISTS task_pr_state (
    task_id INTEGER PRIMARY KEY REFERENCES tasks(id),
    snapshot TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMP,
    next_allowed_at TIMESTAMP,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_error_code TEXT NOT NULL DEFAULT '',
    forced_at TEXT NOT NULL DEFAULT '[]'
);

-- forge_budget: rolling-hour forge call counter per host (ADR-0039 section 4).
CREATE TABLE IF NOT EXISTS forge_budget (
    host TEXT PRIMARY KEY,
    window_start TIMESTAMP NOT NULL,
    calls INTEGER NOT NULL
);
