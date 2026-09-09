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

// One entry in a provider.list response (internal/taskrunner.ProviderInfo):
// the provider id itself plus an optional human-facing label.
export interface ProviderInfo {
  id: Provider;
  label?: string;
}

// Result of provider.list (internal/wsapi/handlers.go's providerListResult).
export interface ProviderListResult {
  providers: ProviderInfo[];
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
