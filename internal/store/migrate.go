package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// migration describes a single additive schema change needed to bring a
// database created before the change landed in schema.sql up to date.
// Migrations run every time Open applies the schema, after the base
// schema.sql's CREATE TABLE IF NOT EXISTS statements, and must be
// idempotent: safe to run against a database that already has the change
// (because it was created fresh from the current schema.sql) as well as
// one that's missing it (because it predates the change).
//
// This intentionally does not pull in an external migration library --
// schema.sql plus this additive-column mechanism covers the only case
// smind has needed so far (a NOT NULL column with a constant default
// added to an existing table). If a future change needs something this
// can't express (renames, drops, backfills from other tables), extend
// the migration type rather than reaching for a new dependency.
type migration struct {
	name  string
	apply func(*sql.DB) error
}

// migrations lists schema changes that new databases already get from
// schema.sql but pre-existing databases need applied explicitly. Append
// new entries here whenever schema.sql adds a column to an existing
// table; do not remove or reorder existing entries, since a database may
// still be missing an old one.
var migrations = []migration{
	{
		// Added by PR #93 (2026-09-13). Databases created before that PR
		// lack this column, which runs rehydrate reads unconditionally --
		// see docs/plans/active/task-permission-ux.md's Validation notes.
		name: "runs.approval_policy",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "runs", "approval_policy", "TEXT NOT NULL DEFAULT 'manual'")
		},
	},
	{
		// Added for docs/decisions/0016-multiple-chats-per-task.md. schema.sql
		// already creates the chats table for every database (fresh or
		// pre-existing, via CREATE TABLE IF NOT EXISTS, applied before migrate
		// runs -- see Open); what a pre-existing database still needs is the
		// runs.chat_id column (addColumnIfMissing can't express this one:
		// unlike approval_policy's constant default, each task's default chat
		// id is different, so there's nothing constant to default the column
		// to) plus, for every task, exactly one default chat with every one
		// of that task's chat_id-less runs repointed at it. See
		// backfillDefaultChats.
		name: "chats.default_chat_backfill",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "chat_id", "INTEGER REFERENCES chats(id)"); err != nil {
				return err
			}
			// Only safe to create once the column above is guaranteed to
			// exist -- see schema.sql's comment on why this index isn't
			// declared there.
			if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_runs_chat_id ON runs(chat_id)`); err != nil {
				return fmt.Errorf("create idx_runs_chat_id: %w", err)
			}
			return backfillDefaultChats(db)
		},
	},
	{
		// Task hierarchy (docs/plans/completed/smind-control-parity.md): nullable
		// parent pointer on tasks, nil for root tasks. Pre-existing databases
		// get the column; no backfill needed (NULL is the valid root default).
		name: "tasks.parent_task_id",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "tasks", "parent_task_id", "INTEGER REFERENCES tasks(id)"); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_tasks_parent_task_id ON tasks(parent_task_id)`)
			return err
		},
	},
	{
		// ADR-0019: provider-native permission modes replace the legacy
		// approval_policy (manual/auto-safe/full-access), which the
		// backfill clears in the same UPDATE so each row is migrated
		// exactly once (resolved decision 8).
		name: "permission_modes",
		apply: func(db *sql.DB) error {
			for _, table := range []string{"runs", "agent_profiles"} {
				if err := addColumnIfMissing(db, table, "permission_mode", "TEXT NOT NULL DEFAULT ''"); err != nil {
					return err
				}
				if err := addColumnIfMissing(db, table, "auto_accept", "INTEGER NOT NULL DEFAULT 0"); err != nil {
					return err
				}
			}
			return backfillPermissionModes(db)
		},
	},
	{
		// ADR-0020 §5: model-aware routing globs on accounts. NULL (no
		// list) is the valid default for every pre-existing row.
		name: "accounts.models",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "accounts", "models", "TEXT")
		},
	},
	{
		// Structured cancel kind on chat_queue items: 'user' (the
		// chat.queueCancel RPC) or 'auto' (delivery failed). The reason
		// string stays display text; the kind is what clients branch on.
		// Pre-existing cancelled rows get theirs backfilled from the
		// reason the daemon wrote at the time ('cancelled by user' is the
		// only user-cancel reason it ever stored), every other cancelled
		// row was a delivery failure. Idempotent: the backfill only
		// touches rows whose kind is still '' and which are no longer
		// queued, and a fresh database has no cancelled rows at all.
		name: "chat_queue.cancel_kind",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "chat_queue", "cancel_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			if _, err := db.Exec(`UPDATE chat_queue
				SET cancel_kind = CASE WHEN cancel_reason = 'cancelled by user' THEN 'user' ELSE 'auto' END
				WHERE status = 'cancelled' AND cancel_kind = ''`); err != nil {
				return fmt.Errorf("backfill chat_queue.cancel_kind: %w", err)
			}
			return nil
		},
	},
	{
		// ADR-0024: per-workspace setup/teardown scripts, per-task setup
		// state and port. All NULL (none) for pre-existing rows. The partial
		// unique index lives here, not in schema.sql, because it needs the
		// port column to exist first.
		name: "worktree_setup",
		apply: func(db *sql.DB) error {
			for _, c := range []struct{ table, col, ddl string }{
				{"workspaces", "setup_script", "TEXT"},
				{"workspaces", "teardown_script", "TEXT"},
				{"tasks", "setup_status", "TEXT"},
				{"tasks", "setup_error", "TEXT"},
				{"tasks", "setup_log", "TEXT"},
				{"tasks", "port", "INTEGER"},
			} {
				if err := addColumnIfMissing(db, c.table, c.col, c.ddl); err != nil {
					return err
				}
			}
			_, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_port_live ON tasks(port) WHERE port IS NOT NULL AND archived_at IS NULL`)
			return err
		},
	},
	{
		// ADR-0025: the requested model on runs and agent profiles. NULL
		// (provider default) is the valid value for every pre-existing row.
		name: "model_selection",
		apply: func(db *sql.DB) error {
			for _, table := range []string{"runs", "agent_profiles"} {
				if err := addColumnIfMissing(db, table, "model", "TEXT"); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0028 §7: quota_snapshots.kind distinguishes polled ('fetched')
		// from passively observed ('observed') readings. Every pre-existing
		// row is a fetched one.
		name: "quota_snapshots_kind",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "quota_snapshots", "kind", "TEXT NOT NULL DEFAULT 'fetched'")
		},
	},
	{
		// ADR-0028 §7.4: quota_cooldown.rejected_window records which window
		// (five_hour, weekly, weekly_model, unified) a rejection cooldown came
		// from, for display. Existing rows predate it: '' (unknown). Not named
		// "window": that is an SQL keyword.
		name: "quota_cooldown_rejected_window",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "quota_cooldown", "rejected_window", "TEXT NOT NULL DEFAULT ''")
		},
	},
	{
		// O1 run provenance: the worktree's git state for a run -- base_sha
		// (HEAD at start), head_sha (HEAD at the terminal state) and dirty
		// (latest successful capture). All NULL (unknown) for pre-existing
		// rows: git may legitimately fail, and unknown is not false.
		name: "run_provenance",
		apply: func(db *sql.DB) error {
			for _, c := range []struct{ col, ddl string }{
				{"base_sha", "TEXT"},
				{"head_sha", "TEXT"},
				{"dirty", "INTEGER"},
			} {
				if err := addColumnIfMissing(db, "runs", c.col, c.ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0033 D6: accounts.identity, the hashed vendor account identity
		// that lets a re-login update a row instead of orphaning it. NULL
		// for every pre-existing row (no provider call backfills it; a row
		// gains one at its first re-login). The partial unique index lives
		// here, not in schema.sql, because it needs the column to exist first.
		name: "accounts_identity",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "accounts", "identity", "TEXT"); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_provider_identity ON accounts(provider, identity) WHERE identity IS NOT NULL`)
			return err
		},
	},
	{
		// ADR-0026: self-learning skills. The new tables are additive
		// (schema.sql's CREATE TABLE IF NOT EXISTS covers them on every
		// Open); what pre-existing databases need are the new columns on
		// workspaces/chats/runs/tasks, whose NOT NULL defaults backfill
		// the pre-existing rows (learning off, nothing pending, every run
		// a user run, every task a user task).
		name: "learning_columns",
		apply: func(db *sql.DB) error {
			for _, c := range []struct{ table, col, ddl string }{
				{"workspaces", "learning_mode", "TEXT NOT NULL DEFAULT 'off'"},
				{"chats", "learn_watermark", "INTEGER NOT NULL DEFAULT 0"},
				{"chats", "learn_pending", "INTEGER NOT NULL DEFAULT 0"},
				{"runs", "purpose", "TEXT NOT NULL DEFAULT 'user'"},
				{"tasks", "kind", "TEXT NOT NULL DEFAULT 'task'"},
			} {
				if err := addColumnIfMissing(db, c.table, c.col, c.ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0039 section 2: the base a task forked from, whether smind
		// created its branch, and the workspace's default-base override.
		// Existing tasks are backfilled at daemon start by the workspace
		// package (it needs git), not here.
		name: "task_base",
		apply: func(db *sql.DB) error {
			for _, c := range []struct{ table, col, ddl string }{
				{"tasks", "base_ref", "TEXT"},
				{"tasks", "base_sha", "TEXT"},
				{"tasks", "branch_owned", "INTEGER NOT NULL DEFAULT 0"},
				{"workspaces", "default_base", "TEXT"},
			} {
				if err := addColumnIfMissing(db, c.table, c.col, c.ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0039 section 4: the task's PR identity. task_pr_state and
		// forge_budget are new tables, created by schema.sql.
		name: "task_pr",
		apply: func(db *sql.DB) error {
			for _, c := range []struct{ col, ddl string }{
				{"pr_forge", "TEXT"},
				{"pr_repo", "TEXT"},
				{"pr_number", "INTEGER"},
				{"pr_url", "TEXT"},
				{"pr_head_ref", "TEXT"},
			} {
				if err := addColumnIfMissing(db, "tasks", c.col, c.ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0039 section 7: a task started from a fork PR (or from a PR
		// the forge could not describe) is untrusted until the user runs its
		// setup. Existing tasks are trusted (0).
		name: "task_untrusted_source",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "tasks", "untrusted_source", "INTEGER NOT NULL DEFAULT 0")
		},
	},
	{
		// ADR-0039 section 9: set when archiving deleted the task's local
		// branch (the Restore button reads it). Existing rows keep NULL.
		name: "task_branch_deleted_at",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "tasks", "branch_deleted_at", "TIMESTAMP")
		},
	},
	{
		// ADR-0026: who authored a run's prompt (human|orchestrator|agent)
		// and whether it was a mid-run interrupt, for the window builder.
		// Pre-existing runs keep '' (unknown) / 0.
		name: "run_origin",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "origin", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "runs", "interrupt", "INTEGER NOT NULL DEFAULT 0")
		},
	},
	{
		// ADR-0038: the accepted attachment set of a run / queued prompt, as
		// attach.EncodeSet JSON. Pre-existing rows keep '' (none).
		name: "attachment_columns",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "attachments", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "chat_queue", "attachments", "TEXT NOT NULL DEFAULT ''")
		},
	},
	{
		// ADR-0041 B1: typed terminal reason of an error run. Pre-existing
		// rows keep '' / '' / NULL (no typed failure, reset unknown).
		name: "run_failure",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "failure_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			if err := addColumnIfMissing(db, "runs", "failure_window", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "runs", "failure_reset_at", "TIMESTAMP")
		},
	},
	{
		// A rate-limit reset read in an assumed zone is shown with "≈". No
		// installed base: old rows read 0 (stated).
		name: "run_failure_reset_inferred",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "runs", "failure_reset_inferred", "INTEGER NOT NULL DEFAULT 0")
		},
	},
	{
		// The zone a rate-limit reset's text was read in (the run's "Details"
		// note). No installed base: old rows read '' (no note).
		name: "run_failure_reset_zone",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "runs", "failure_reset_zone", "TEXT NOT NULL DEFAULT ''")
		},
	},
	{
		// ADR-0041: a queued item may be held until not_before. Existing
		// rows stay NULL (deliverable now).
		name: "chat_queue_not_before",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "chat_queue", "not_before", "TIMESTAMP")
		},
	},
	{
		// ADR-0041 B3: parent chat to notify on finish + the idempotent
		// notified marker. Pre-existing rows stay NULL (never notified).
		name: "run_reply_chat",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "reply_chat_id", "INTEGER"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "runs", "parent_notified_at", "TIMESTAMP")
		},
	},
	{
		// ADR-0041 A5: opt-in tree budget on the root task. Nullable with no
		// default; existing rows stay NULL (no budget).
		name: "task_budget_usd",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "tasks", "budget_usd", "REAL")
		},
	},
	{
		// Stalled-tree alert: the stalledSince of the last alerted episode.
		// Nullable with no default; existing rows stay NULL (never alerted).
		name: "stall_alert",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "tasks", "stall_alerted_since", "TIMESTAMP")
		},
	},
	{
		// ADR-0041 F2: rate-limit failover policy on agent profiles.
		name: "profile_failover",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "agent_profiles", "on_rate_limit", "TEXT NOT NULL DEFAULT 'none'"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "agent_profiles", "fallback_profile_id", "INTEGER")
		},
	},
	{
		// ADR-0041 S1: the seat contract on agent profiles.
		name: "profile_contract",
		apply: func(db *sql.DB) error {
			for _, c := range []struct{ name, def string }{
				{"orchestration_tools", "INTEGER"},
				{"instructions", "TEXT NOT NULL DEFAULT ''"},
				{"skills", "TEXT NOT NULL DEFAULT ''"},
				{"child_policy", "TEXT NOT NULL DEFAULT ''"},
				{"child_profile_ids", "TEXT NOT NULL DEFAULT ''"},
			} {
				if err := addColumnIfMissing(db, "agent_profiles", c.name, c.def); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0041 F2: the profile a run was launched with, and the run it
		// continues after a rate limit.
		name: "run_profile_failover",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "runs", "profile_id", "INTEGER"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "runs", "failover_of", "TEXT NOT NULL DEFAULT ''")
		},
	},
	{
		// ADR-0026 section 6: learned_skill_offers / learned_skill_loads are
		// new tables, so schema.sql's CREATE TABLE IF NOT EXISTS already
		// creates them on every Open; this entry is the explicit marker and
		// re-creates them should a database somehow lack them.
		name: "learning_counters",
		apply: func(db *sql.DB) error {
			for _, q := range []string{
				`CREATE TABLE IF NOT EXISTS learned_skill_offers (
					skill_id INTEGER NOT NULL REFERENCES learned_skills(id),
					run_id TEXT NOT NULL,
					created_at TIMESTAMP NOT NULL,
					PRIMARY KEY (skill_id, run_id))`,
				`CREATE TABLE IF NOT EXISTS learned_skill_loads (
					skill_id INTEGER NOT NULL REFERENCES learned_skills(id),
					run_id TEXT NOT NULL,
					via TEXT NOT NULL,
					created_at TIMESTAMP NOT NULL,
					PRIMARY KEY (skill_id, run_id, via))`,
			} {
				if _, err := db.Exec(q); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0041 F1: provider_cooldowns is a new table (also in
		// schema.sql); this entry re-creates it should a database lack it.
		name: "provider_cooldowns",
		apply: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS provider_cooldowns (
				provider TEXT PRIMARY KEY,
				reset_at TIMESTAMP NOT NULL,
				run_id TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMP NOT NULL)`)
			return err
		},
	},
	{
		// ADR-0042: plugins is a new table (also in schema.sql); this entry
		// re-creates it should a database lack it.
		name: "plugins",
		apply: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugins (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				version TEXT NOT NULL,
				source_kind TEXT NOT NULL,
				source TEXT NOT NULL,
				hash TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 0,
				capabilities TEXT NOT NULL DEFAULT '{}',
				consent_hash TEXT NOT NULL DEFAULT '',
				pending TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				installed_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL)`)
			return err
		},
	},
	{
		// ADR-0042 PB.2: plugin_settings is a new table (also in schema.sql);
		// this entry re-creates it should a database lack it.
		name: "plugin_settings",
		apply: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_settings (
				plugin_id TEXT NOT NULL,
				key TEXT NOT NULL,
				value TEXT NOT NULL,
				updated_at TIMESTAMP NOT NULL,
				PRIMARY KEY (plugin_id, key))`)
			return err
		},
	},
	{
		// ADR-0026 section 6: learning_snapshots is a new table (also in
		// schema.sql); this entry re-creates it should a database lack it.
		name: "learning_snapshots",
		apply: func(db *sql.DB) error {
			for _, q := range []string{
				`CREATE TABLE IF NOT EXISTS learning_snapshots (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					workspace_id INTEGER NOT NULL REFERENCES workspaces(id),
					created_at TIMESTAMP NOT NULL,
					reason TEXT NOT NULL DEFAULT '',
					payload TEXT NOT NULL)`,
				`CREATE INDEX IF NOT EXISTS idx_learning_snapshots_ws ON learning_snapshots(workspace_id, id)`,
			} {
				if _, err := db.Exec(q); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0046 section 4: provider_secrets is a new table (also in
		// schema.sql); this entry re-creates it should a database lack it.
		name: "provider_secrets",
		apply: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS provider_secrets (
				instance_id TEXT NOT NULL,
				name TEXT NOT NULL,
				value TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL,
				PRIMARY KEY (instance_id, name))`)
			return err
		},
	},
	{
		// ADR-0048 F1a: the fleet tables are new (also in schema.sql); this
		// entry re-creates them should a database lack them.
		name: "fleet_tables",
		apply: func(db *sql.DB) error {
			for _, q := range []string{
				`CREATE TABLE IF NOT EXISTS fleet_hosts (
					host_id TEXT PRIMARY KEY,
					alias TEXT NOT NULL UNIQUE,
					pub BLOB NOT NULL,
					label TEXT NOT NULL DEFAULT '',
					state TEXT NOT NULL,
					created_at TIMESTAMP NOT NULL)`,
				`CREATE TABLE IF NOT EXISTS fleet_links (
					host_id TEXT NOT NULL REFERENCES fleet_hosts(host_id) ON DELETE CASCADE,
					direction TEXT NOT NULL CHECK (direction IN ('outbound','inbound')),
					enabled INTEGER NOT NULL DEFAULT 0,
					grant_json TEXT NOT NULL DEFAULT '',
					transport_json TEXT NOT NULL DEFAULT '',
					updated_at TIMESTAMP NOT NULL,
					PRIMARY KEY (host_id, direction))`,
				`CREATE TABLE IF NOT EXISTS fleet_audit (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					ts TIMESTAMP NOT NULL,
					direction TEXT NOT NULL,
					host_id TEXT NOT NULL,
					trace TEXT NOT NULL DEFAULT '',
					req_id TEXT NOT NULL DEFAULT '',
					method TEXT NOT NULL,
					task_id TEXT NOT NULL DEFAULT '',
					run_id TEXT NOT NULL DEFAULT '',
					outcome TEXT NOT NULL,
					code TEXT NOT NULL DEFAULT '',
					latency_ms INTEGER NOT NULL DEFAULT 0,
					bytes INTEGER NOT NULL DEFAULT 0,
					summary TEXT NOT NULL DEFAULT '')`,
				`CREATE INDEX IF NOT EXISTS idx_fleet_audit_ts ON fleet_audit(ts)`,
				`CREATE INDEX IF NOT EXISTS idx_fleet_audit_host ON fleet_audit(host_id, id)`,
				`CREATE TABLE IF NOT EXISTS fleet_watches (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					host_id TEXT NOT NULL,
					remote_task_id TEXT NOT NULL,
					remote_run_id TEXT NOT NULL DEFAULT '',
					local_task_id INTEGER NOT NULL DEFAULT 0,
					chat_id INTEGER NOT NULL DEFAULT 0,
					cursor TEXT NOT NULL DEFAULT '',
					state TEXT NOT NULL DEFAULT 'open',
					failures INTEGER NOT NULL DEFAULT 0,
					notify INTEGER NOT NULL DEFAULT 1,
					closed_reason TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMP NOT NULL,
					updated_at TIMESTAMP NOT NULL)`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_fleet_watches_run ON fleet_watches(host_id, remote_task_id, remote_run_id)`,
				`CREATE TABLE IF NOT EXISTS fleet_idem (
					host_id TEXT NOT NULL,
					idem_key TEXT NOT NULL,
					method TEXT NOT NULL,
					task_id TEXT NOT NULL DEFAULT '',
					run_id TEXT NOT NULL DEFAULT '',
					response TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMP NOT NULL,
					PRIMARY KEY (host_id, idem_key))`,
				`CREATE TABLE IF NOT EXISTS fleet_remote_tasks (
					host_id TEXT NOT NULL,
					remote_task_id TEXT NOT NULL,
					local_task_id INTEGER NOT NULL DEFAULT 0,
					title TEXT NOT NULL DEFAULT '',
					branch TEXT NOT NULL DEFAULT '',
					state TEXT NOT NULL DEFAULT '',
					cost_usd REAL,
					updated_at TIMESTAMP NOT NULL,
					PRIMARY KEY (host_id, remote_task_id))`,
			} {
				if _, err := db.Exec(q); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		// ADR-0048 F1a: fleet provenance on existing rows -- which host a
		// task/run came from. Pre-existing rows keep '' (local origin).
		name: "fleet_origin_columns",
		apply: func(db *sql.DB) error {
			if err := addColumnIfMissing(db, "tasks", "origin_host", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			if err := addColumnIfMissing(db, "tasks", "origin_trace", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			return addColumnIfMissing(db, "runs", "origin_host", "TEXT NOT NULL DEFAULT ''")
		},
	},
	{
		// ADR-0048 F5: operator labels on a fleet host. Caller-local, a
		// JSON object, never sent to a peer. '' means no labels.
		name: "fleet_host_labels",
		apply: func(db *sql.DB) error {
			return addColumnIfMissing(db, "fleet_hosts", "labels", "TEXT NOT NULL DEFAULT ''")
		},
	},
}

// legacyPermissionModeSQL maps a row's legacy approval_policy (and
// provider) onto ADR-0019's migration table -- always toward asking a
// human: manual and auto-safe both become the provider's own
// asks-for-shell-commands mode (Claude acceptEdits, Codex auto, ACP the
// agent's own default ""), and only full-access becomes an auto-approving
// setting (Claude bypassPermissions, Codex full-access, ACP autoAccept).
// ACP's "accept_edits if advertised" isn't knowable at migration time (no
// agent is running), so ACP auto-safe rows get the agent default -- the
// narrower choice.
const legacyPermissionModeSQL = `
	permission_mode = CASE
		WHEN provider = 'claude-native' AND approval_policy = 'full-access' THEN 'bypassPermissions'
		WHEN provider = 'claude-native' THEN 'acceptEdits'
		WHEN provider = 'codex-native' AND approval_policy = 'full-access' THEN 'full-access'
		WHEN provider = 'codex-native' THEN 'auto'
		ELSE '' END,
	auto_accept = CASE
		WHEN provider IN ('glm', 'kimi') AND approval_policy = 'full-access' THEN 1
		ELSE 0 END`

// backfillPermissionModes applies legacyPermissionModeSQL to every row
// still carrying a legacy approval_policy, clearing approval_policy in the
// same UPDATE -- so each row is backfilled exactly once, and a user's later
// edit to its permission_mode/auto_accept (e.g. unticking a migrated
// full-access profile's autoAccept, or resetting its mode to "") is never
// re-overwritten on the next Open. New rows never carry a non-empty
// approval_policy (CreateRun writes ” explicitly; agent_profiles
// defaults to ”), so they never match.
func backfillPermissionModes(db *sql.DB) error {
	for _, table := range []string{"runs", "agent_profiles"} {
		if _, err := db.Exec(`UPDATE ` + table + ` SET` + legacyPermissionModeSQL + `, approval_policy = '' WHERE approval_policy != ''`); err != nil {
			return fmt.Errorf("backfill %s.permission_mode: %w", table, err)
		}
	}
	return nil
}

// backfillDefaultChats gives every task exactly one default chat
// (provider/agent_session NULL, title "Chat") and repoints every one of
// that task's runs still missing a chat_id at it. Idempotent: a task that
// already has at least one chat (whether from a prior run of this
// migration, or because it was created after chats shipped) is left alone,
// and a run that already has a chat_id is left alone -- so running this
// twice, or against a fresh database with no tasks at all, is a no-op.
// Transactional: either every task gets its default chat and every run
// gets repointed, or (on any failure) none of it is applied, since this
// runs against the user's real smind.db on every daemon startup and a
// half-applied backfill (some tasks migrated, others not) would be worse
// than simply failing to start.
func backfillDefaultChats(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("backfill default chats: begin: %w", err)
	}
	defer tx.Rollback()

	taskRows, err := tx.Query(`SELECT id FROM tasks`)
	if err != nil {
		return fmt.Errorf("backfill default chats: list tasks: %w", err)
	}
	var taskIDs []int64
	for taskRows.Next() {
		var id int64
		if err := taskRows.Scan(&id); err != nil {
			taskRows.Close()
			return fmt.Errorf("backfill default chats: scan task id: %w", err)
		}
		taskIDs = append(taskIDs, id)
	}
	if err := taskRows.Err(); err != nil {
		taskRows.Close()
		return fmt.Errorf("backfill default chats: read task ids: %w", err)
	}
	taskRows.Close()

	for _, taskID := range taskIDs {
		var chatID int64
		err := tx.QueryRow(`SELECT id FROM chats WHERE task_id = ? ORDER BY id LIMIT 1`, taskID).Scan(&chatID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			now := time.Now().UTC()
			res, err := tx.Exec(
				`INSERT INTO chats (task_id, title, provider, agent_session, created_at) VALUES (?, 'Chat', NULL, NULL, ?)`,
				taskID, now,
			)
			if err != nil {
				return fmt.Errorf("backfill default chats: create default chat for task %d: %w", taskID, err)
			}
			chatID, err = res.LastInsertId()
			if err != nil {
				return fmt.Errorf("backfill default chats: default chat id for task %d: %w", taskID, err)
			}
		case err != nil:
			return fmt.Errorf("backfill default chats: find chat for task %d: %w", taskID, err)
		}

		if _, err := tx.Exec(
			`UPDATE runs SET chat_id = ? WHERE task_id = ? AND chat_id IS NULL`, chatID, taskID,
		); err != nil {
			return fmt.Errorf("backfill default chats: repoint runs for task %d: %w", taskID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("backfill default chats: commit: %w", err)
	}
	return nil
}

// migrate applies any pending migrations to db. It runs on every Open;
// each migration is a no-op if its change is already present, so this is
// cheap and safe for fresh and already-migrated databases alike.
func migrate(db *sql.DB) error {
	for _, m := range migrations {
		if err := m.apply(db); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

// addColumnIfMissing adds column to table with the given type/constraint
// clause (e.g. "TEXT NOT NULL DEFAULT 'manual'") if it isn't already
// present. table and column must be trusted (compile-time) identifiers,
// never user input, since they're interpolated into the SQL text --
// PRAGMA and ALTER TABLE don't support bound parameters for identifiers.
func addColumnIfMissing(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			return nil // already present, nothing to do
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s columns: %w", table, err)
	}

	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, ddl)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}
