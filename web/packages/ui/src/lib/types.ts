// Wire types for internal/wsapi's workspace.*/task.* results. These mirror
// internal/store.Workspace/Task field-for-field, including their exact
// (PascalCase) JSON keys -- those types carry no `json:` tags, so
// encoding/json marshals them using the Go field names verbatim.

export interface Workspace {
  ID: number;
  Path: string;
  Title: string;
  RoutingPolicy: string;
  /** Whether a worktree setup/teardown script is configured (ADR-0024); the script text itself is only returned by workspace.get. */
  HasSetupScript?: boolean;
  HasTeardownScript?: boolean;
  /** ADR-0039: the stored default-base override ("" = auto-detect); mirrors store.Workspace's field. */
  DefaultBase?: string;
  CreatedAt: string;
  UpdatedAt: string;
}

/** Result of workspace.defaultBase/workspace.setDefaultBase (ADR-0039): the effective default base ref, whether it was auto-detected, and the stored override ("" when none). */
export interface WorkspaceDefaultBase {
  ref: string;
  detected: boolean;
  override: string;
}

/** workspace.get's result: the workspace plus its setup/teardown script text ("" = none) -- the only wire method that returns it (ADR-0024). */
export interface WorkspaceWithScripts extends Workspace {
  SetupScript: string;
  TeardownScript: string;
}

/** A task's worktree setup state (ADR-0024): "" = no setup script ran. */
export type SetupStatus = "" | "running" | "ok" | "failed" | "blocked";

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
  /** The setup/port fields are optional only so an older daemon (and bare test fixtures) still type-check; read them as ""/null when absent. */
  SetupStatus?: SetupStatus;
  /** Failure reason when SetupStatus is "failed". */
  SetupError?: string;
  /** Tail of the setup script's merged stdout/stderr (capped daemon-side). */
  SetupLog?: string;
  /** The allocated SMIND_PORT, or null when no port range is configured. */
  Port?: number | null;
  /** The parent task id; null/absent for a root task. */
  ParentTaskID?: number | null;
  /** ADR-0039 §7: the task came from a fork PR, so its setup script is blocked until the user runs it (`task.retrySetup`). Absent on an older daemon. */
  UntrustedSource?: boolean;
  /** The PR the task was started from (ADR-0039 G4), when it was. */
  PRNumber?: number | null;
  PRRepo?: string;
  PRHeadRef?: string;
  BaseRef?: string;
  /** ADR-0039 §9: set when archiving deleted the task's branch, so Restore has nothing to rebuild the worktree from. */
  BranchDeletedAt?: string | null;
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

// Mirrors internal/store.AgentProfile field-for-field (ADR-0014, "docs/
// decisions/0014-agent-profiles.md") -- same no-json-tags, PascalCase-on-
// the-wire convention as Workspace/Task/Space above. A named, daemon-global
// bundle of per-run settings (Provider/PermissionMode/AutoAccept/
// ThinkingLevel), not scoped to any workspace. PermissionMode/ThinkingLevel
// may be "" (unset -- the provider's own default mode applies):
// the composer's own default applies in that case, same as an omitted
// task.prompt field does today. Model (ADR-0025) is the profile's requested
// model id, "" (NULL) meaning the provider's default.
export interface AgentProfile {
  ID: number;
  Name: string;
  Provider: string;
  /** One of the provider's own permission mode ids (ADR-0019), or "" for the provider default. */
  PermissionMode: string;
  /** ACP providers only: approve every permission prompt. */
  AutoAccept: boolean;
  ThinkingLevel: string;
  /** ADR-0025: a model id from the provider's catalog, or "" for the provider's default (absent on an older daemon). */
  Model?: string;
  Notes: string;
  /** ADR-0041 seat contract; all optional, absent/unset = a legacy profile. null = unmanaged. */
  OrchestrationTools?: boolean | null;
  Instructions?: string;
  Skills?: string[];
  ChildPolicy?: string;
  ChildProfileIDs?: number[];
  CreatedAt: string;
  UpdatedAt: string;
}

// Mirrors internal/store.Chat field-for-field (ADR-0016, "docs/decisions/
// 0016-multiple-chats-per-task.md") -- same no-json-tags, PascalCase-on-
// the-wire convention as Workspace/Task above. One conversation thread
// inside a task's shared worktree; Provider is null until the chat's first
// run binds it (immutable afterwards, see chat.rename/run.start).
export interface Chat {
  ID: number;
  TaskID: number;
  Title: string;
  Provider: string | null;
  AgentSession: string | null;
  CreatedAt: string;
  ArchivedAt: string | null;
}

// internal/taskrunner.Provider values, carried over the wire as their
// underlying strings. The authoritative list is served dynamically by the
// daemon's provider.list method (see ProviderListResult); this union just
// covers the values that can appear, including the hardcoded fallback's two.
export type Provider = "claude-native" | "glm" | "kimi" | "codex-native";

// One of a provider's own permission modes (internal/taskrunner.ModeInfo,
// ADR-0019): Claude Code's --permission-mode values, Codex's presets, or
// an ACP agent's advertised session modes -- the provider's own
// vocabulary, never a smind-invented tier. autoApproves marks a mode in
// which the provider runs tool calls without asking anyone.
export interface ModeInfo {
  id: string;
  label: string;
  description?: string;
  autoApproves?: boolean;
}

// A run's provider-native permission settings as the composer submits them
// (run.start's permissionMode/autoAccept params, ADR-0019): permissionMode
// "" means the provider's default mode.
export interface PermissionSettings {
  permissionMode: string;
  autoAccept: boolean;
}

// internal/taskrunner.ThinkingLevel's values, carried over the wire as
// their underlying string -- run.start/task.prompt's optional
// `thinkingLevel` param. Claude-only (every other provider ignores it, and
// the composer only ever renders this control when provider === "claude-native");
// GLM/Kimi's own thinking-level control is a completely different,
// live-session-scoped mechanism (ACP's ConfigOption, see run.listConfigOptions/
// run.setConfigOption), and Codex has no reachable per-turn equivalent at
// all. "" (omitted) preserves today's SDK default exactly.
export type ThinkingLevel = "" | "off" | "standard" | "extended";

// One entry of a provider's model catalog (internal/taskrunner.ModelInfo,
// ADR-0025). `default` marks the model the provider uses when none is
// requested.
export interface ModelInfo {
  id: string;
  label: string;
  description?: string;
  default?: boolean;
}

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
  /** ADR-0038: whether the provider can view images natively ("no" = the file path is sent instead). Absent on a daemon without attachments. */
  images?: "yes" | "no" | "unknown";
  label?: string;
  kind?: ProviderKind;
  credentialKind?: ProviderCredentialKind;
  accountProvider?: string;
  /** The provider's own permission modes (ADR-0019), in display order. */
  modes?: ModeInfo[];
  /** The mode a run gets when it names none. */
  defaultMode?: string;
  /** ACP: true once the agent's own modes were discovered (else `modes` is a fallback). */
  modesDiscovered?: boolean;
  /** ACP providers: the run may also set autoAccept (approve every prompt). */
  supportsAutoAccept?: boolean;
  /** Whether a running run's mode can be switched (run.setPermissionMode) -- false for Codex. */
  liveModeSwitch?: boolean;
  /** ADR-0025: the provider's model catalog. Empty/absent when none is known (an ACP agent not probed yet). */
  models?: ModelInfo[];
  /** True once `models` came from the agent itself rather than a static catalog. */
  modelsDiscovered?: boolean;
  /** Config `providers.<id>.defaultModel`: the model a run that names none resolves to. Absent = the provider's own default. */
  defaultModel?: string;
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
/** "interrupted" is a run whose daemon crashed under it (the startup reconciliation); a graceful restart reads "stopped" with stop reason daemon_restart. */
export type RunStatusValue = "running" | "done" | "error" | "stopped" | "interrupted";

// Mirrors internal/runs.RunUsage (ADR-0020): a run's reported usage from
// its own agent events. Every field but `source` is optional/null and
// null means "not reported" -- NEVER 0, which is a real reported value.
// `source` names which agent surface produced it.
export interface RunUsage {
  inputTokens?: number | null;
  cachedInputTokens?: number | null;
  cacheWriteTokens?: number | null;
  outputTokens?: number | null;
  reasoningTokens?: number | null;
  costUsd?: number | null;
  model?: string | null;
  contextUsed?: number | null;
  contextSize?: number | null;
  source: string;
}

// Mirrors internal/runs.RunStatus (aliased as RunSummary for run.list's
// result) field-for-field. Like Workspace/Task, this struct carries no
// `json:` tags, so its wire shape is the exact PascalCase Go field names.
export interface RunSummary {
  ID: string;
  TaskID: number;
  /** ADR-0016 P1.4: which chat this run belongs to -- every run has exactly one, the task's default chat when its prompt/run.start omitted chatId. */
  ChatID: number;
  Provider: Provider;
  Prompt: string;
  Status: RunStatusValue;
  StartedAt: string;
  FinishedAt: string | null;
  StopReason: string;
  /** Derived by the daemon (ADR-0027 D13): "needs-continue" when the run ended done at a turn/token/budget cap; absent/empty otherwise. Not a status. */
  Disposition?: string;
  /** ADR-0041: "rate_limited" when the run failed on a provider quota (status stays "error"); absent/empty otherwise. */
  FailureKind?: string;
  /** The quota window that was hit ("5h", "weekly", ...); absent/empty when unknown. */
  FailureWindow?: string;
  /** When the limit lifts (RFC 3339 UTC); null/absent when unknown. */
  FailureResetAt?: string | null;
  Err: string;
  /** The run's current provider-native permission mode -- live for a running run (see run.setPermissionMode), whatever it last was for a finished one. */
  PermissionMode: string;
  /** ACP only: whether every permission prompt is auto-approved (live-switchable). */
  AutoAccept: boolean;
  /** Claude-only; "" (unset) for every other provider or a run started before the thinking-level selector was touched. Not persisted across a daemon restart -- a rehydrated run always reports "". */
  ThinkingLevel: ThinkingLevel;
  /** ADR-0025: the model this run was asked to use; "" (or absent on an older daemon) means the provider's default. */
  Model?: string;
  /** ADR-0020: the run's usage totals -- lowercase json tag, so unlike the PascalCase fields above. Absent for an old run or one that reported nothing. */
  usage?: RunUsage;
  /** O1 provenance: the worktree HEAD at the run's start; null/absent when unknown (an older daemon, a non-git worktree, or a failed git call). */
  BaseSHA?: string | null;
  /** O1 provenance: the worktree HEAD at the run's terminal state; null/absent while running or unknown. */
  HeadSHA?: string | null;
  /** O1 provenance: the worktree's dirty state at the latest successful capture (start, overwritten at the terminal state). */
  Dirty?: boolean | null;
  /** ADR-0038: the prompt's attachments as metadata (never bytes). Absent/empty when it had none. */
  attachments?: AttachmentMeta[];
  /** Who authored the prompt: "human", "orchestrator", "agent" or "child_event" (ADR-0041). Lowercase json tag, omitted when unknown. */
  origin?: string;
}

// internal/runs.WhenBusy's values (ADR-0021 §2), run.start's optional
// `whenBusy` param: what the daemon does when the chat already has a
// running run -- legacy reject (the default), queue the prompt, or
// interrupt the live run. The queue/interrupt results come back as
// RunStartResult's queued variant instead of a runId.
export type WhenBusy = "reject" | "queue" | "interrupt";

// Result of run.start (internal/wsapi/handlers.go's runStartResult and,
// when the chat was busy and whenBusy was queue/interrupt, its
// runQueuedResult -- ADR-0021 §2). Only reachable by explicitly passing
// whenBusy, so a call without it stays the runId variant.
export type RunStartResult = { runId: string } | { queued: true; queueItemId: number };

// One entry in a chat.queueList result or chat.queueUpdated snapshot
// (internal/wsapi/handlers.go's chatQueueItemParams, ADR-0021 §8) --
// store.ChatQueueItem's fields under their Go names as JSON keys, same
// convention as Chat above. Status covers the item's whole lifecycle;
// RunID is set once delivered, CancelReason/CancelKind once cancelled.
export interface ChatQueueItem {
  ID: number;
  ChatID: number;
  Prompt: string;
  /** The run.start run config as a JSON string, opaque to the UI. */
  RunConfig: string;
  Source: string;
  FromTaskID: number | null;
  FromChatID: number | null;
  Priority: number;
  Status: "queued" | "delivered" | "cancelled";
  RunID: string | null;
  /** Display text only ("cancelled by user", "delivery failed: ..."); branch on CancelKind, never on this string. */
  CancelReason: string;
  /** Who cancelled: "user" (vanishes at once) or "auto" (shows its reason for the TTL); "" while queued/delivered. */
  CancelKind: "" | "user" | "auto";
  CreatedAt: string;
  UpdatedAt: string;
  /** ADR-0041: RFC 3339 instant the item is held until (a child-update debounce); null/absent = deliverable now. */
  NotBefore?: string | null;
  /** ADR-0038: the queued prompt's attachments (metadata only). */
  attachments?: AttachmentMeta[];
}

// Result of run.setPermissionMode (internal/wsapi/handlers.go's
// runPermissionModeResult): the run's permission settings after the
// change, so a caller can confirm the switch took without a round trip.
export interface RunSetPermissionModeResult {
  permissionMode: string;
  autoAccept: boolean;
}

// Terminal result of a successful run.attach (internal/wsapi/handlers.go's
// taskPromptResult -- task.prompt/run.attach share this shape).
export interface RunAttachResult {
  runId: string;
  stopReason: string;
  /** "needs-continue" when the run ended done at a cap (ADR-0027 D13). */
  disposition?: string;
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

/**
 * Structured detail the daemon attaches to a permission_request (wire event
 * params and run.logs entries alike). Every field but `kind` is optional.
 */
export interface PermissionDetail {
  kind: "write" | "edit" | "execute" | "other";
  path?: string;
  newBytes?: number;
  newLines?: number;
  existingBytes?: number;
  existingLines?: number;
  /** The target file exists and this write replaces it entirely. */
  replacesExisting?: boolean;
  /** The target is outside the task worktree. */
  outsideWorktree?: boolean;
  /** Existing >= 20 lines and the new content is under half of it. */
  largeShrink?: boolean;
  preview?: string;
  /** Unified-style text: lines start with ' ', '+', '-' or '@@'. */
  diff?: string;
  command?: string;
  /** The preview/diff was cut. */
  truncated?: boolean;
  /** Existing contents deliberately not shown (sensitive/outside path). */
  redacted?: boolean;
}

export interface PermissionRequestEventParams {
  requestId: string;
  summary: string;
  options: PermissionOption[];
  /** Present only for a question-form-shaped request -- see PermissionQuestion's doc comment on why nothing produces this yet. */
  questions?: PermissionQuestion[];
  /** Present only for a plan-review-shaped request: the plan as markdown, reviewed via Chat about it / Refuse / Approve rather than a plain option list. Same "additive, no producer yet" status as `questions`. */
  plan?: string;
  detail?: PermissionDetail;
}

// Wire form of taskrunner.PermissionResolution (internal/taskrunner/event.go).
// Widened past the three known values (rather than a strict union) so a
// reason this client has never heard of still decodes and renders as
// "no badge" instead of a type error -- the same append-only-enum
// tolerance RunEventType documents below.
// "auto_safe" is legacy (ADR-0019 removed smind's auto-safe allowlist) --
// kept so an older daemon's payload still renders.
export type PermissionResolutionReason = "human" | "auto_accept" | "auto_safe" | "timeout" | (string & {});

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

// One server entry in an "mcp_servers" run event (docs/plans/completed/
// agent-mcp-servers.md): the per-run state of an MCP server the run tried to
// start. Multiple mcp_servers events per run are possible; the latest one
// wins.
export interface McpServerState {
  name: string;
  transport: "stdio" | "http" | "sse";
  status: "active" | "dropped" | "failed";
  reason?: "transport_unsupported" | "command_not_found" | "provider_unsupported" | "failed_to_start";
}

// Every event name run.attach/run.logs can carry today
// (internal/wsapi/handlers.go's toRunLogEvent). The three structured ones
// were added by ADR 0008 and are additive: a run recorded before it still
// decodes as chunk/done/permission_* only. "raw" was added by ADR 0010.
export type RunEventType =
  | "chunk"
  | "user_message"
  | "thinking"
  | "tool_call"
  | "done"
  | "permission_request"
  | "permission_resolved"
  | "mcp_servers"
  | "usage"
  | "raw";

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
  /** Populated for a "permission_request" entry -- see PermissionDetail. */
  detail?: PermissionDetail;
  optionId?: string;
  /** Client-side only: the kind of `optionId`, filled in from the matching "permission_request" by the timeline hook. */
  optionKind?: string;
  reason?: PermissionResolutionReason;
  /** Populated for a "raw" entry (ADR 0010) -- the ACP session-update kind the normalizer didn't recognize (e.g. "plan"). */
  kind?: string;
  /** Populated for a "raw" entry (ADR 0010) -- the update's full original JSON, forwarded as-is. */
  payload?: unknown;
  /** Populated for an "mcp_servers" entry -- the run's current per-server state, latest event wins. */
  mcpServers?: McpServerState[];
  /** Populated for a "usage" entry (ADR-0020) -- the run's usage, latest event wins. */
  usage?: RunUsage;
}

// Terminal result of run.logs (internal/wsapi/handlers.go's runLogsResult).
export interface RunLogsResult {
  runId: string;
  status: RunStatusValue;
  stopReason?: string;
  /** "needs-continue" when the run ended done at a cap (ADR-0027 D13). */
  disposition?: string;
  /** ADR-0041: "rate_limited" when the run failed on a provider quota; omitted otherwise. */
  failureKind?: string;
  /** The quota window that was hit ("5h", "weekly", ...); omitted when unknown. */
  window?: string;
  /** When the limit lifts (RFC 3339 UTC); omitted when unknown. */
  resetAt?: string;
  err?: string;
  /** O1 provenance -- omitted by the daemon when unknown (or by an older daemon). */
  baseSha?: string;
  headSha?: string;
  dirty?: boolean;
  /** ADR-0038: the run's attachments (metadata only). */
  attachments?: AttachmentMeta[];
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

// ---- Create / Update PR (ADR-0039 §5; docs/research/local/git-pr-g5-briefs.md "PR 6 wire contract") -----------

/** One commit of a PR preview. */
export interface CommitSummary {
  sha: string;
  subject: string;
}

/** `task.prPreview`: local git and cache only, never a forge call. */
export interface PrPreview {
  base: { ref: string; sha: string };
  head: { branch: string; ref: string; strategy: "direct" | "cherry-pick" };
  /** Oldest first, at most 50. */
  commits: CommitSummary[];
  commitCount: number;
  /** The committed change the pull request will contain (not the uncommitted files). Absent when there are no commits. */
  diff?: { files: number; insertions: number; deletions: number };
  uncommitted: { staged: number; unstaged: number; untracked: number };
  existingPr?: TaskGitPr;
  defaults: { title: string; body: string; draft: boolean };
  forge?: { name: string; repo: string; host: string };
  /** Empty, or the single reason `actions.createPr` is disabled. */
  blockers: { code: string; reason: string }[];
}

/** Params of `task.createPr`; an empty `baseBranch` means the task's `base_ref`. */
export interface TaskCreatePrParams {
  taskId: number;
  baseBranch?: string;
  title: string;
  body: string;
  draft: boolean;
}

/** Result of `task.createPr`. */
export interface TaskCreatePrResult {
  pr: TaskGitPr;
}

/** Result of `task.updatePr`. */
export interface TaskUpdatePrResult {
  pr: TaskGitPr;
  pushed: { from: string; to: string };
}

/** `data` of a typed `pr_exists` error. */
export interface PrExistsErrorData {
  url: string;
  number: number;
  detail?: string;
}

/** `workspace.branches` (ADR-0039 §4, PR 7); the Create PR dialog tolerates its absence. */
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
  /** ADR-0016 P1.4. */
  chatId: number;
  status: RunStatusValue;
  stopReason?: string;
  /** "needs-continue" when the run ended done at a cap (ADR-0027 D13). */
  disposition?: string;
  /** ADR-0041: "rate_limited" when the run failed on a provider quota; omitted otherwise. */
  failureKind?: string;
  /** The quota window that was hit ("5h", "weekly", ...); omitted when unknown. */
  window?: string;
  /** When the limit lifts (RFC 3339 UTC); omitted when unknown. */
  resetAt?: string;
  err?: string;
  /** O1 provenance -- omitted when unknown. */
  baseSha?: string;
  headSha?: string;
  dirty?: boolean;
}

// Payload of permission.pending: {runId, taskId, chatId, requestId,
// summary, options} -- same option shape as PermissionOption. chatId is
// ADR-0016 P1.4.
export interface PermissionPendingEventPayload {
  runId: string;
  taskId: number;
  chatId: number;
  requestId: string;
  summary: string;
  options: PermissionOption[];
}

// Payloads of ADR-0016's chat lifecycle events (internal/wsapi/events.go's
// chatCreatedPayload/chatUpdatedPayload/chatArchivedPayload): a full Chat
// snapshot each time, no delete -- a chat is archived, never destroyed,
// matching task.archive's own event shape.
export interface ChatCreatedEventPayload {
  chat: Chat;
}
export interface ChatUpdatedEventPayload {
  chat: Chat;
}
export interface ChatArchivedEventPayload {
  chat: Chat;
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
  /** ADR-0033: whether this account row has a signed-in identity (true) or only an API-key credential (false). Absent on an older daemon. */
  hasIdentity?: boolean;
}

// ADR-0033's login contract (account.oauthStart/account.oauthComplete):
// how the browser half of an OAuth connect is opened. "device" is offered
// only when the daemon advertises it; "browser" is always available.
export type LoginMethod = "browser" | "device";

// Params of account.oauthStart (ADR-0033). accountId is present only when
// signing in against an existing account row (adopting it).
export interface OAuthStartParams {
  provider: string;
  label: string;
  method?: LoginMethod;
  accountId?: number;
}

// Terminal result of a successful account.oauthStart: the account row plus
// whether it was newly created or adopted into an existing row.
export interface OAuthStartResult extends Account {
  created: boolean;
  adopted?: boolean;
}

// Streaming events account.oauthStart emits (ADR-0033's login contract):
// the browser-half URL to open (pasteOnly: no loopback listener, the user
// must copy the redirect address back) or a device-code pair to show.
export type LoginEvent =
  | { kind: "authorizeUrl"; loginId: string; url: string; pasteOnly: boolean }
  | { kind: "deviceCode"; loginId: string; verificationUrl: string; userCode: string; expiresAt: string; intervalSeconds: number };

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
// One entry in an mcp.list/mcp.get response, or the result of mcp.create/
// mcp.update/mcp.setEnabled (internal/wsapi/mcp_handlers.go's
// mcpServerResult): lowercase json tags. Every env/headers VALUE is
// redacted server-side to the literal "[redacted]" (keys preserved) --
// the UI must never expect or show a real secret value, and sending the
// "[redacted]" placeholder back on mcp.update means "keep the stored
// secret". Full-replace semantics: mcp.update carries every field.
export interface McpServer {
  id: number;
  name: string;
  transport: McpTransport;
  /** stdio only. */
  command: string;
  /** stdio only. */
  args: string[];
  /** stdio only; values always "[redacted]" on read. */
  env: Record<string, string>;
  /** http/sse only. */
  url: string;
  /** http/sse only; values always "[redacted]" on read. */
  headers: Record<string, string>;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

// internal/store.McpServer's transport values, carried over the wire as
// their underlying strings (ADR-0018).
export type McpTransport = "stdio" | "http" | "sse";

// One row of a usage.summary response (internal/wsapi's usage summary
// row, ADR-0020): per-group sums. Token sums are COALESCE(SUM, 0) on the
// server, so a group where no row reported a field reads 0 -- the
// `unreported` array names those fields (absent when everything was
// reported) and a null costUsd means no run reported a cost.
export interface UsageSummaryRow {
  scope: "proxy" | "runs";
  key: string;
  count: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  reasoningTokens: number;
  costUsd?: number | null;
  unreported?: Array<"inputTokens" | "outputTokens" | "cacheReadTokens" | "cacheWriteTokens" | "reasoningTokens">;
}

export type UsageScope = "runs" | "proxy" | "all";
export type UsageGroupBy = "task" | "provider" | "model" | "workspace";
export type UsageRange = "today" | "7d" | "30d";

// ADR-0038 prompt attachments (frozen wire contract). Requests carry bytes as standard base64; stored metadata never does.
export type AttachmentRequest =
  | { kind: "image" | "file"; name: string; mimeType: string; data: string }
  | { kind: "file_ref"; path: string; line?: number; endLine?: number }
  | { kind: "pr" | "issue"; forge?: "github"; number: number; repo?: string }
  | { kind: "reuse"; runId: string; id: string };

/** A stored attachment as the daemon reports it (`run.list`, `run.logs`, queue rows). `id` is the 1-based position as a string. */
export interface AttachmentMeta {
  id: string;
  kind: "image" | "file" | "file_ref" | "pr" | "issue";
  name?: string;
  mimeType?: string;
  size?: number;
  width?: number;
  height?: number;
  path?: string;
  isDir?: boolean;
  line?: number;
  endLine?: number;
  forge?: string;
  number?: number;
  repo?: string;
  title?: string;
  url?: string;
  state?: string;
  /** Client-only: an object URL for a thumbnail the client already holds (the optimistic echo), never sent by the daemon. */
  previewUrl?: string;
}

/** `daemon.info.attachments`: the caps the composer mirrors. Absent on a daemon without the feature. */
export interface AttachmentLimits {
  maxCount: number;
  maxImageBytes: number;
  maxFileBytes: number;
  maxTotalBytes: number;
  maxMessageBytes: number;
  images: string[];
  /** The attachment kinds this daemon accepts. Absent means ["image", "file", "file_ref"]. */
  kinds?: string[];
}

/** `daemon.info.forge` (ADR-0039 §8): the forge CLI capability of the daemon host. `cli` is whether `gh` is on its PATH. */
export interface DaemonForge {
  provider: "github";
  cli: boolean;
}

/** `forge.status` (ADR-0039 §8): the daemon host's `gh` and its signed-in hosts. Never carries a token or a path. */
export interface ForgeHostStatus {
  host: string;
  authenticated: boolean;
  login: string;
  scopes: string[];
  protocol: string;
  source: "keyring" | "file" | "env" | "none";
}

export interface ForgeStatus {
  provider: "github";
  /** Whether THIS connection's channel is secure (loopback, TLS or E2EE): the token field is disabled when false. */
  canSetToken: boolean;
  cli: { installed: boolean; version: string };
  hosts: ForgeHostStatus[];
}

/** `forge.setToken`: the token is on the wire only here and never stored on the client. */
export interface ForgeSetTokenParams {
  host?: string;
  token: string;
}

/** `forge.setToken` / `forge.logout` result, and the `forge.status` event payload. */
export interface ForgeStatusResult {
  status: ForgeStatus;
}

/** `run.attachment` result. */
export interface RunAttachmentResult {
  name: string;
  mimeType: string;
  size: number;
  data: string;
}

// ---- Task git state (ADR-0039 §3): `task.git`, `task.gitList` and the `task.git` event --------------------------
// Every nested object is optional: an absent object or field means "not known", never zero.

export type GitConflictState = "none" | "conflicts" | "unknown";
export type GitMergedState = "merged" | "unmerged" | "unknown";
export type PrState = "open" | "closed" | "merged";
export type PrMergeable = "mergeable" | "conflicting" | "unknown";
export type PrReviewDecision = "approved" | "changes_requested" | "review_required" | "none";
export type PrChecksState = "success" | "failure" | "pending" | "none";
/** Why a PR snapshot is not fresh: "" = fresh. */
export type PrStale = "" | "budget" | "backoff" | "rate_limited" | "unauthenticated" | "unavailable";

export interface TaskGitPr {
  number: number;
  repo: string;
  url: string;
  title: string;
  state: PrState;
  draft: boolean;
  headRef: string;
  headSha: string;
  baseRef: string;
  fromFork: boolean;
  mergeable: PrMergeable;
  reviewDecision: PrReviewDecision;
  checks?: { state: PrChecksState; passed: number; failed: number; pending: number; total: number };
  /** RFC 3339. */
  fetchedAt: string;
  stale: PrStale;
  /** RFC 3339 or "" (when an explicit refresh is possible again). */
  refreshAvailableAt: string;
}

export interface TaskGitAction {
  enabled: boolean;
  /** Machine code when disabled (e.g. "no_commits", "pr_exists"). */
  code?: string;
  /** A full sentence saying why it is disabled and what to do next. */
  reason?: string;
}

export interface TaskGit {
  taskId: number;
  forge?: string;
  branch?: { name: string; owned: boolean };
  base?: { ref?: string; sha?: string; tip?: string; known: boolean };
  diff?: { files: number; insertions: number; deletions: number };
  worktree?: { dirty: boolean; staged: number; unstaged: number; untracked: number };
  commits?: { aheadOfBase?: number; behindBase?: number; sinceFork?: number };
  origin?: { upstream?: string; published: boolean; ahead?: number; behind?: number };
  conflict?: { state: GitConflictState; files: string[]; against?: string; checkedAt: string; reason?: string };
  merged?: { state: GitMergedState; via: string };
  pr?: TaskGitPr;
  actions?: Record<string, TaskGitAction>;
  untrusted: boolean;
  /** RFC 3339. */
  computedAt: string;
}

/** `task.gitList` result. */
export interface TaskGitListResult {
  items: TaskGit[];
}

/** Payload of a `task.git` event. */
export interface TaskGitEventPayload {
  taskId: number;
  git: TaskGit;
}

// ---- Archive risk dialog and Restore (ADR-0039 §9) ----------------------------------------------------------------
/** `task.archivePreview`: what archiving the task would do and lose, from local git and cache only (no fetch). */
export interface ArchivePreview {
  branch: { name: string; owned: boolean };
  merged: { state: GitMergedState; via: string };
  base: { ref: string };
  risks: {
    uncommitted: { files: number; added: number; deleted: number };
    commitsAheadOfOrigin: number;
    commitsAheadOfBase: number;
    published: boolean;
  };
  ignoredFilesLost: boolean;
  hasTeardown: boolean;
  /** Whether the non-forced policy would delete the branch now; also the checkbox default. */
  canDeleteBranch: boolean;
  deleteBranchDefault: boolean;
  /** The daemon's sentence for why `canDeleteBranch` is false. */
  deleteBlockedReason?: string;
}

/** `task.archive` params. `id` is what daemons before ADR-0039 §9 read; `taskId` is the frozen wire name. */
export interface TaskArchiveParams {
  id: number;
  taskId: number;
  deleteBranch?: boolean;
  /** Delete an owned branch although it is unmerged or dirty (the dialog's explicit "anyway"). */
  force?: boolean;
}

// ---- New Task "Start from" (ADR-0039 G4) --------------------------------------------------------------------------
/** One row of `workspace.branches`. `ahead`/`behind` are omitted when unknown: no chip, never 0. */
export interface WorkspaceBranch {
  name: string;
  remote: "local" | "origin";
  ahead?: number;
  behind?: number;
}

/** `workspace.branches` {workspaceId, query?, limit?}: newest first; `current` may be "". */
export interface WorkspaceBranches {
  defaultBase: string;
  current: string;
  branches: WorkspaceBranch[];
}
