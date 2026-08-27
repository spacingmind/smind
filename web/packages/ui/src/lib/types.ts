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

// The only two internal/taskrunner.Provider values that exist today (see
// internal/taskrunner/provider.go) -- not dynamically discovered, see
// docs/plans/active/web-ui-task-detail.md's Decisions.
export type Provider = "claude-native" | "glm";

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
export interface PermissionRequestEventParams {
  requestId: string;
  summary: string;
  options: PermissionOption[];
}

// Params of a "permission_resolved" event task.prompt/run.attach emit
// (internal/wsapi/handlers.go's permissionResolvedParams).
export interface PermissionResolvedEventParams {
  requestId: string;
  optionId: string;
}

// One event in a run.logs response (internal/wsapi/handlers.go's
// runLogEvent) -- the same fields chunk/permission events and terminal
// results carry, batched instead of streamed. Which of the other fields
// are populated depends on `type`, mirroring internal/taskrunner.Event's
// own discriminated-by-Type shape.
export interface RunLogEvent {
  type: "chunk" | "done" | "permission_request" | "permission_resolved";
  text?: string;
  stopReason?: string;
  requestId?: string;
  summary?: string;
  options?: PermissionOption[];
  optionId?: string;
}

// Terminal result of run.logs (internal/wsapi/handlers.go's runLogsResult).
export interface RunLogsResult {
  runId: string;
  status: RunStatusValue;
  stopReason?: string;
  err?: string;
  events: RunLogEvent[];
}

// internal/terminal.Status's two values (internal/terminal/terminal.go),
// carried over the wire as their underlying string.
export type TerminalStatusValue = "running" | "closed";

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

// Result of file.read (internal/wsapi/files.go's fileReadResult).
export interface FileReadResult {
  content: string;
}

// Result of task.diff (internal/wsapi/handlers.go's taskDiffResult): the
// task's full unified diff text, or an empty string for a task with no
// changes.
export interface TaskDiffResult {
  diff: string;
}
