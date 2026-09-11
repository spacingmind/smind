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

// internal/taskrunner.ApprovalPolicy's two values, carried over the wire as
// their underlying string -- run.start/task.prompt's optional
// `approvalPolicy` param (internal/wsapi/handlers.go). "manual" is the
// default when omitted (today's always-ask-a-human behavior); "auto-safe"
// lets a small, conservative allowlist of read-only verification commands
// (see internal/taskrunner.AllowlistedCommand) skip the human prompt.
export type ApprovalPolicy = "manual" | "auto-safe";

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
export interface PermissionRequestEventParams {
  requestId: string;
  summary: string;
  options: PermissionOption[];
}

// Params of a "permission_resolved" event task.prompt/run.attach emit
// (internal/wsapi/handlers.go's permissionResolvedParams). `reason` is the
// wire form of taskrunner.PermissionResolution ("human" | "auto_safe" |
// "timeout") -- optional here (rather than a strict union) so an older
// server payload with no reason field still decodes fine.
export interface PermissionResolvedEventParams {
  requestId: string;
  optionId: string;
  reason?: string;
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
  reason?: string;
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
