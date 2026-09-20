// Wire types for internal/wsapi's workspace.*/task.* results. These mirror
// internal/store.Workspace/Task field-for-field, including their exact
// (PascalCase) JSON keys -- those types carry no `json:` tags, so
// encoding/json marshals them using the Go field names verbatim.

export interface Workspace {
  ID: number;
  Path: string;
  Title: string;
  RoutingPolicy: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface Task {
  ID: number;
  WorkspaceID: number;
  SpaceID: number | null;
  Title: string;
  Status: string;
  WorktreePath: string | null;
  Branch: string | null;
  CreatedAt: string;
  UpdatedAt: string;
  ArchivedAt: string | null;
}

// Mirrors internal/store.Space field-for-field -- same no-json-tags,
// PascalCase-on-the-wire convention as Workspace/Task above. An optional
// grouping layer within a workspace; Task.SpaceID (nullable) points at
// one of these.
export interface Space {
  ID: number;
  WorkspaceID: number;
  Title: string;
  EnvData: string;
  CreatedAt: string;
  UpdatedAt: string;
}

// internal/taskrunner.Provider values, carried over the wire as their
// underlying strings. The authoritative list is served dynamically by the
// daemon's provider.list method (see ProviderListResult); this union just
// covers the values that can appear, including the hardcoded fallback's two.
export type Provider = "claude-native" | "glm" | "kimi" | "codex-native";

// internal/taskrunner.ApprovalPolicy's three values, carried over the wire
// as their underlying string -- run.start/task.prompt's optional
// `approvalPolicy` param (internal/wsapi/handlers.go). "manual" is the
// default when omitted (today's always-ask-a-human behavior); "auto-safe"
// lets a small, conservative allowlist of read-only verification commands
// (see internal/taskrunner.AllowlistedCommand) skip the human prompt;
// "full-access" installs no decider at all and hands the provider its own
// native "auto-approve everything" mechanism instead (Claude Code's
// bypassPermissions mode, Codex's AutoApprovePolicy, ACP's
// AutoApprovePolicy) -- each provider's own real ceiling, not one shared
// generic tier (see docs/plans/active/task-move-approval-thinking.md).
export type ApprovalPolicy = "manual" | "auto-safe" | "full-access";

// internal/taskrunner.ThinkingLevel's values, carried over the wire as
// their underlying string -- run.start/task.prompt's optional
// `thinkingLevel` param. Claude-only (every other provider ignores it, and
// the composer only ever renders this control when provider === "claude-native");
// GLM/Kimi's own thinking-level control is a completely different,
// live-session-scoped mechanism (ACP's ConfigOption, see run.listConfigOptions/
// run.setConfigOption), and Codex has no reachable per-turn equivalent at
// all. "" (omitted) preserves today's SDK default exactly.
export type ThinkingLevel = "" | "off" | "standard" | "extended";

// internal/taskrunner.ProviderInfo's Kind: "cli" marks a provider that's
// spawned as an external CLI subprocess managing its own authentication out
// of band (e.g. GLM's `npx -y glm-acp-agent`) -- the daemon tracks no
// credential for it, so accounts-dialog renders it as "managed externally"
// instead of offering a credential form or flagging a missing key. Absent
// means "handled through the separate account-credential system instead"
// (today: everything else provider.list returns).
export type ProviderKind = "cli";

// internal/taskrunner.ProviderCredentialKind's two values, present only on
// providers with a credential row in accounts-dialog (i.e. kind is unset).
// "oauth" gets a Connect button (account.oauthStart) with manual-paste as a
// fallback; "api-key" gets only the manual-paste form (account.add).
export type ProviderCredentialKind = "oauth" | "api-key";

// One entry in a provider.list response (internal/taskrunner.ProviderInfo):
// the provider id itself plus an optional human-facing label, Kind, and (for
// providers with a credential row) CredentialKind/accountProvider.
//
// accountProvider is the id accounts-dialog must actually send to
// account.add/account.oauthStart -- it's internal/accounts' own provider
// vocabulary (anthropic/openai/kimi/xai/antigravity), which predates and
// differs from this Provider union (claude-native/glm/kimi/codex-native).
// The two aren't fully unified: xai and antigravity are accounts-only
// providers with no taskrunner counterpart, so provider.list can't (and
// doesn't try to) describe them -- see accounts-dialog.tsx's doc comment and
// docs/plans/active/task-permission-ux.md's Item 7d note for that gap.
export interface ProviderInfo {
  id: Provider;
  label?: string;
  kind?: ProviderKind;
  credentialKind?: ProviderCredentialKind;
  accountProvider?: string;
}

// Result of provider.list (internal/wsapi/handlers.go's providerListResult).
export interface ProviderListResult {
  providers: ProviderInfo[];
}

// Result of provider.test (internal/wsapi/handlers.go's providerTestResult):
// a lightweight "can this provider actually start?" diagnostic -- ok plus a
// short human-readable detail either way (which account/credential it used,
// or why it isn't ready). Never mutates anything (no refresh, no run).
export interface ProviderTestResult {
  ok: boolean;
  detail: string;
}

// internal/runs.Status's four values (internal/runs/runs.go) -- carried
// over the wire as their underlying string, same as any other Go string
// enum with no json tag remapping.
export type RunStatusValue = "running" | "done" | "error" | "stopped";

// Mirrors internal/runs.RunStatus (aliased as RunSummary for run.list's
// result) field-for-field. Like Workspace/Task, this struct carries no
// `json:` tags, so its wire shape is the exact PascalCase Go field names.
export interface RunSummary {
  ID: string;
  TaskID: number;
  Provider: Provider;
  Prompt: string;
  Status: RunStatusValue;
  StartedAt: string;
  FinishedAt: string | null;
  StopReason: string;
  Err: string;
}

// Result of run.start (internal/wsapi/handlers.go's runStartResult).
export interface RunStartResult {
  runId: string;
}

// Terminal result of a successful run.attach (internal/wsapi/handlers.go's
// taskPromptResult -- task.prompt/run.attach share this shape).
export interface RunAttachResult {
  runId: string;
  stopReason: string;
}

// Params of every "chunk" event task.prompt/run.attach emit
// (internal/wsapi/handlers.go's taskChunkParams).
export interface RunChunkEventParams {
  text: string;
}

// One selectable choice of a ConfigOption whose Type is "select"
// (internal/wsapi/handlers.go's configSelectOptionParams, mirroring
// internal/acp.ConfigSelectOption -- ACP's own SessionConfigSelectOption).
export interface ConfigSelectOption {
  value: string;
  name: string;
  description?: string;
}

// One ACP session config option, as run.listConfigOptions/run.setConfigOption
// report it (internal/wsapi/handlers.go's configOptionParams, mirroring
// internal/acp.ConfigOption). GLM/Kimi-only -- Claude/Codex runs always
// report an empty list (see ThinkingLevel's own doc comment for why their
// thinking-level knobs work completely differently). currentValue is raw
// JSON since its shape varies by type: a bare string for "select", a bare
// boolean for "boolean". options is only ever populated for type "select"
// (an agent's own enumerated named choices, e.g. GLM's real thinking-level
// tiers) -- read ids/labels from here, never hardcoded, since this
// package has no fixed list of what any given agent will advertise.
export interface ConfigOptionParams {
  configId: string;
  name: string;
  description?: string;
  category?: string;
  type: string;
  currentValue?: unknown;
  options?: ConfigSelectOption[];
}

// Result of run.listConfigOptions/run.setConfigOption
// (internal/wsapi/handlers.go's runConfigOptionsResult).
export interface RunConfigOptionsResult {
  options: ConfigOptionParams[];
}

// One choice offered by a pending permission request (internal/wsapi/
// handlers.go's permissionOptionParams -- the wire shape of
// taskrunner.PermissionOption). `kind` is one of ACP's
// PermissionOptionKind values, carried through as-is: "allow_once" |
// "allow_always" | "reject_once" | "reject_always".
export interface PermissionOption {
  id: string;
  label: string;
  kind: string;
}

// Params of a "permission_request" event task.prompt/run.attach emit
// (internal/wsapi/handlers.go's permissionRequestParams).
// One question in a structured, multi-question permission ask
// (ui-redesign-parity Item 11, per audit-deepseek-harness.md's question-
// form card). **No provider on either wire path produces this shape
// today** -- internal/taskrunner's PermissionDecider always resolves to a
// flat PermissionOption list (see permission.go), so `questions` is
// defined here as forward-compatible, additive surface: a future daemon
// change could populate it without breaking today's clients, and today's
// UI can already render it correctly (see permission-card.tsx's
// Validation note) the moment something does.
export interface PermissionQuestion {
  id: string;
  prompt: string;
  kind: "single_select" | "multi_select" | "free_text";
  /** Present for single_select/multi_select. */
  options?: { id: string; label: string }[];
  /** Whether a free-text "other" answer is accepted alongside the listed options. */
  allowOther?: boolean;
}

export interface PermissionRequestEventParams {
  requestId: string;
  summary: string;
  options: PermissionOption[];
  /** Present only for a question-form-shaped request -- see PermissionQuestion's doc comment on why nothing produces this yet. */
  questions?: PermissionQuestion[];
  /** Present only for a plan-review-shaped request: the plan as markdown, reviewed via Chat about it / Refuse / Approve rather than a plain option list. Same "additive, no producer yet" status as `questions`. */
  plan?: string;
}

// Wire form of taskrunner.PermissionResolution (internal/taskrunner/event.go).
// Widened past the three known values (rather than a strict union) so a
// reason this client has never heard of still decodes and renders as
// "no badge" instead of a type error -- the same append-only-enum
// tolerance RunEventType documents below.
export type PermissionResolutionReason = "human" | "auto_safe" | "timeout" | (string & {});

// Params of a "permission_resolved" event task.prompt/run.attach emit
// (internal/wsapi/handlers.go's permissionResolvedParams). `reason` is
// optional (rather than required) so an older server payload with no
// reason field still decodes fine.
export interface PermissionResolvedEventParams {
  requestId: string;
  optionId: string;
  reason?: PermissionResolutionReason;
}

// A tool call's lifecycle status (internal/taskrunner.Event's ToolStatus,
// per docs/decisions/0008-structured-run-events.md). Absent on a partial
// update means "unchanged", never "cleared".
export type ToolCallStatus = "running" | "success" | "failure";

// Params of a "tool_call" event run.attach/task.prompt emit, and the
// tool-call half of a run.logs entry (internal/wsapi/handlers.go's
// runToolCallParams), added by ADR 0008.
//
// **Merge by toolCallId, never replace.** A later event for the same call
// carries its new `status` and `result`, and -- on the ACP path, whose
// tool_call_update is a partial update -- may omit `toolName`, `title`
// and `input` entirely. Treating each event as the call's full current
// state would blank the card's identity the moment it completes.
//
// `input`/`result` are deliberately `unknown`: the daemon forwards each
// provider's own argument/result JSON unnormalized (ADR 0008's Decision
// section says so explicitly), so a renderer must narrow what it wants
// rather than trust a shared schema that does not exist.
export interface RunToolCallEventParams {
  toolCallId: string;
  /** Claude's wire tool name ("Bash", "Read", …) or ACP's kind ("execute", "read", …) -- one vocabulary per provider, both keys into the same renderer registry. */
  toolName?: string;
  title?: string;
  status?: ToolCallStatus;
  input?: unknown;
  result?: unknown;
}

// Every event name run.attach/run.logs can carry today
// (internal/wsapi/handlers.go's toRunLogEvent). The three structured ones
// were added by ADR 0008 and are additive: a run recorded before it still
// decodes as chunk/done/permission_* only.
export type RunEventType =
  | "chunk"
  | "user_message"
  | "thinking"
  | "tool_call"
  | "done"
  | "permission_request"
  | "permission_resolved";

// One event in a run.logs response (internal/wsapi/handlers.go's
// runLogEvent) -- the same fields the streamed events and terminal
// results carry, batched instead of streamed. Which of the other fields
// are populated depends on `type`, mirroring internal/taskrunner.Event's
// own discriminated-by-Type shape.
//
// `type` is intentionally widened past RunEventType: the enum is
// append-only on the daemon side (ADR 0008), so a newer daemon can send a
// name this client has never heard of, and the timeline renders those as
// a fallback row rather than crashing. The union is still spelled out so
// the known names autocomplete.
export interface RunLogEvent extends Partial<RunToolCallEventParams> {
  type: RunEventType | (string & {});
  text?: string;
  stopReason?: string;
  requestId?: string;
  summary?: string;
  options?: PermissionOption[];
  optionId?: string;
  reason?: PermissionResolutionReason;
}

// Terminal result of run.logs (internal/wsapi/handlers.go's runLogsResult).
export interface RunLogsResult {
  runId: string;
  status: RunStatusValue;
  stopReason?: string;
  err?: string;
  events: RunLogEvent[];
}

// internal/terminal.Status's values (internal/terminal/terminal.go), carried
// over the wire as their underlying string. "interrupted" is the daemon-side
// terminal-persistence work's addition (docs/plans/active/daemon-restart-resync.md)
// for a session whose PTY subprocess couldn't have survived a daemon
// restart -- a real, expected, honest outcome distinct from "closed" (which
// only ever means an explicit terminal.close).
export type TerminalStatusValue = "running" | "closed" | "interrupted";

// Mirrors internal/terminal.SessionStatus (internal/wsapi's terminal.list
// result) field-for-field -- like RunSummary, this struct carries no
// `json:` tags on the Go side, so its wire shape is the exact PascalCase
// Go field names.
export interface TerminalSessionStatus {
  ID: string;
  TaskID: number;
  StartedAt: string;
  Status: TerminalStatusValue;
  ClosedAt: string | null;
}

// Result of terminal.create (internal/wsapi/terminal.go's
// terminalCreateResult).
export interface TerminalCreateResult {
  terminalId: string;
}

// Terminal result of terminal.attach, once the session itself closes
// (internal/wsapi/terminal.go's terminalAttachResult).
export interface TerminalAttachResult {
  terminalId: string;
}

// Params of every "data" event terminal.attach emits
// (internal/wsapi/terminal.go's terminalDataParams) -- one chunk of raw
// PTY output, base64-encoded (see that Go type's doc comment for why:
// PTY output isn't guaranteed valid UTF-8 at arbitrary chunk boundaries).
export interface TerminalDataEventParams {
  data: string;
}

// One entry returned by file.list (internal/files.Entry) -- a file or
// subdirectory of the listed directory. Like RunLogEvent, this Go struct
// carries `json:` tags, so its wire shape is lowercase, not the PascalCase
// Go field names.
export interface FileEntry {
  name: string;
  isDir: boolean;
  size: number;
}

// Result of file.read (internal/wsapi/files.go's fileReadResult): content
// plus the read-time mtime, echoed back as file.write's expectedMtime for
// conditional saves (see the file-conflict-detection plan).
export interface FileReadResult {
  content: string;
  mtime: string;
}

// Result of file.write (internal/wsapi/files.go's fileWriteResult): the
// written file's mtime, chained into the next conditional save.
export interface FileWriteResult {
  mtime: string;
}

// Result of task.diff (internal/wsapi/handlers.go's taskDiffResult): the
// task's full unified diff text, or an empty string for a task with no
// changes.
export interface TaskDiffResult {
  diff: string;
}

// Result of task.searchIndex (internal/wsapi/handlers.go's
// taskSearchIndexResult): every worktree-relative path eligible for
// quick-open (Item 18) -- git's own notion of the worktree's contents,
// fuzzy-matched client-side (lib/fuzzy-match.ts) rather than server-side,
// so ranking/highlighting stay in the UI's own testable code.
export interface TaskSearchIndexResult {
  paths: string[];
}

// One entry in a task.files response (internal/workspace.TaskFile): a path
// in the task's base→worktree diff, its change kind, and whether it's
// currently staged in the worktree's real index.
export interface TaskFile {
  path: string;
  status: "added" | "modified" | "deleted" | string;
  staged: boolean;
}

// Result of task.files (internal/wsapi/handlers.go's taskFilesResult).
export interface TaskFilesResult {
  files: TaskFile[];
}

// One entry in a task.stats response (internal/workspace.TaskStat): a
// task's branch and the size of the same base->worktree diff task.diff
// renders. Tasks with no worktree, and tasks whose stat could not be
// computed, are absent from the list -- zero changed files is a real and
// different statement from "not known", so the sidebar must be able to
// tell them apart.
export interface TaskStat {
  taskId: number;
  branch: string;
  filesChanged: number;
  insertions: number;
  deletions: number;
}

// Result of task.stats (internal/wsapi/handlers.go's taskStatsResult).
export interface TaskStatsResult {
  stats: TaskStat[];
}

// Result of task.fileDiff (internal/wsapi/handlers.go's
// taskFileDiffResult): one file's slice of the task's unified diff, or ""
// for a path with no changes.
export interface TaskFileDiffResult {
  diff: string;
}

// Result of workspace.delete/space.delete (internal/wsapi/handlers.go's
// deleteSummaryResult): how many tasks/spaces were actually removed, so the
// UI can show an accurate confirmation without a second round trip.
export interface DeleteSummaryResult {
  tasksRemoved: number;
  spacesRemoved: number;
}

// Result of task.commit (internal/wsapi/handlers.go's taskCommitResult):
// the new commit's full sha, its subject line, and the staged file count
// it recorded.
export interface TaskCommitResult {
  commit: string;
  subject: string;
  files: number;
}

// Result of task.createPr (internal/wsapi/handlers.go's
// taskCreatePRResult): the URL of the pull request opened for the task's
// branch (directly, or from a clean smind/pr-<id> branch if the task
// branch's base had diverged -- see internal/workspace.Manager.CreatePR).
export interface TaskCreatePrResult {
  url: string;
}

// Payloads of ADR 0005 notifications (internal/wsapi/events.go) -- see
// docs/decisions/0005-wsapi-event-subscription.md. Carried in the
// notification envelope's payload field (lowercase json tags, unlike the
// PascalCase no-tag structs above).

// Payload of task.status: {taskId, status}.
export interface TaskStatusEventPayload {
  taskId: number;
  status: string;
}

// Payload of run.status: {runId, taskId, status, stopReason?, err?}.
export interface RunStatusEventPayload {
  runId: string;
  taskId: number;
  status: RunStatusValue;
  stopReason?: string;
  err?: string;
}

// Payload of permission.pending: {runId, taskId, requestId, summary,
// options} -- same option shape as PermissionOption.
export interface PermissionPendingEventPayload {
  runId: string;
  taskId: number;
  requestId: string;
  summary: string;
  options: PermissionOption[];
}

// One entry in an account.list response (internal/wsapi/handlers.go's
// accountResult): lowercase json tags, unlike the PascalCase no-tag store
// structs above. Credential material itself is never returned over RPC.
export interface Account {
  id: number;
  provider: string;
  label: string;
  credentialType: string;
  createdAt: string;
  updatedAt: string;
}

// One entry in an fs.listDir response (internal/wsapi/hostfs.go's
// fsListDirEntry, mirroring internal/hostfs.Entry): a subdirectory of the
// listed directory, and whether it's itself a git repository.
export interface FsListDirEntry {
  name: string;
  path: string;
  isGitRepo: boolean;
}

// Result of fs.listDir (internal/wsapi/hostfs.go's fsListDirResult): the
// resolved directory that was listed, its parent (empty string at the
// filesystem root), and its subdirectories.
export interface FsListDirResult {
  path: string;
  parent: string;
  entries: FsListDirEntry[];
}
