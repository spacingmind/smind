import { useEffect, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { FolderPickerDialog } from "@/components/folder-picker-dialog";
import { FormError } from "@/components/ui/form-error";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useLastDefined } from "@/hooks/use-last-defined";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { TaskTreeNode } from "@/lib/task-lineage";
import type { Chat, Space, Task, Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** Shared submit row: Create disabled while in flight, Cancel always closes. */
function FormActions({
  label,
  pending,
  onCancel,
  submitTestId,
  cancelTestId,
}: {
  label: string;
  pending: boolean;
  onCancel: () => void;
  submitTestId?: string;
  cancelTestId?: string;
}) {
  return (
    <DialogFooter>
      <Button type="button" variant="quiet" onClick={onCancel} data-testid={cancelTestId}>
        Cancel
      </Button>
      <Button type="submit" disabled={pending} data-testid={submitTestId}>
        {pending ? "Creating…" : label}
      </Button>
    </DialogFooter>
  );
}

/** Wraps a controlled dialog's content with the <form> + error/pending plumbing every CRUD dialog shares. */
function CrudForm({
  title,
  description,
  submitLabel,
  onSubmit,
  onOpenChange,
  children,
  submitTestId,
  cancelTestId,
}: {
  title: string;
  description?: string;
  submitLabel: string;
  onSubmit: () => Promise<boolean>;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
  submitTestId?: string;
  cancelTestId?: string;
}) {
  const [pending, setPending] = useState(false);

  return (
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description && <DialogDescription>{description}</DialogDescription>}
      </DialogHeader>
      <form
        className="grid gap-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setPending(true);
          const ok = await onSubmit();
          setPending(false);
          if (ok) onOpenChange(false);
        }}
      >
        {children}
        <FormActions
          label={submitLabel}
          pending={pending}
          onCancel={() => onOpenChange(false)}
          submitTestId={submitTestId}
          cancelTestId={cancelTestId}
        />
      </form>
    </DialogContent>
  );
}

export function CreateWorkspaceDialog({
  client,
  open,
  onOpenChange,
  onCreated,
  recentPaths = [],
}: {
  client: WsClient | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the created workspace so the sidebar can refresh the tree (and expand the newcomer). */
  onCreated: (workspace: Workspace) => void;
  /** Parent directories of already-registered workspaces, for the folder picker's quick-jump row -- see `recentWorkspaceParentDirs`. */
  recentPaths?: string[];
}) {
  const [path, setPath] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        {open && (
          <CrudForm
            title="New workspace"
            description="Must be an existing git repository -- tasks are created as worktrees branched off it."
            submitLabel="Create workspace"
            onOpenChange={onOpenChange}
            submitTestId="dialog-new-workspace-submit"
            cancelTestId="dialog-new-workspace-cancel"
            onSubmit={async () => {
              if (!path.trim()) {
                setError("Path is required.");
                return false;
              }
              try {
                const ws = await client!.call<Workspace>("workspace.create", {
                  path: path.trim(),
                  ...(title.trim() ? { title: title.trim() } : {}),
                });
                onCreated(ws);
                setPath("");
                setTitle("");
                return true;
              } catch (err) {
                setError(err instanceof Error ? err.message : String(err));
                return false;
              }
            }}
          >
            <div className="grid gap-2">
              <label htmlFor="workspace-path" className="text-ui-base font-medium">
                Path
              </label>
              <div className="flex gap-2">
                <Input
                  id="workspace-path"
                  data-testid="dialog-new-workspace-path-input"
                  placeholder="/path/to/repo"
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  autoFocus
                  className="flex-1"
                />
                <Button
                  type="button"
                  variant="outline"
                  data-testid="dialog-new-workspace-browse-button"
                  onClick={() => setPickerOpen(true)}
                >
                  Browse…
                </Button>
              </div>
            </div>
            <div className="grid gap-2">
              <label htmlFor="workspace-title" className="text-ui-base font-medium">
                Title <span className="font-normal text-muted-foreground">(optional)</span>
              </label>
              <Input
                id="workspace-title"
                data-testid="dialog-new-workspace-title-input"
                placeholder="Defaults to the repository directory name"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
              />
            </div>
            <FormError message={error} />
          </CrudForm>
        )}
      </Dialog>
      <FolderPickerDialog
        client={client}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onSelect={(selected) => setPath(selected)}
        recentPaths={recentPaths}
      />
    </>
  );
}

export function CreateSpaceDialog({
  client,
  workspaceId,
  open,
  onOpenChange,
  onCreated,
}: {
  client: WsClient | null;
  workspaceId: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}) {
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && (
        <CrudForm
          title="New space"
          description="Groups tasks inside this workspace -- it isn't its own repo; the workspace already points at that."
          submitLabel="Create space"
          onOpenChange={onOpenChange}
          submitTestId="dialog-new-space-submit"
          cancelTestId="dialog-new-space-cancel"
          onSubmit={async () => {
            if (!title.trim()) {
              setError("Title is required.");
              return false;
            }
            try {
              await client!.call("space.create", { workspaceId, title: title.trim() });
              onCreated();
              setTitle("");
              return true;
            } catch (err) {
              setError(err instanceof Error ? err.message : String(err));
              return false;
            }
          }}
        >
          <div className="grid gap-2">
            <label htmlFor="space-title" className="text-ui-base font-medium">
              Title
            </label>
            <Input
              id="space-title"
              data-testid="dialog-new-space-title-input"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
            />
          </div>
          <FormError message={error} />
        </CrudForm>
      )}
    </Dialog>
  );
}

export function CreateTaskDialog({
  client,
  workspace,
  spaces,
  fixedSpaceId = null,
  open,
  onOpenChange,
  onCreated,
}: {
  client: WsClient | null;
  workspace: Workspace;
  spaces: Space[];
  /** When started from a space row, the space is fixed and the select is hidden. */
  fixedSpaceId?: number | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the created task so App can select it (chat tab ready). */
  onCreated: (task: Task) => void;
}) {
  const [title, setTitle] = useState("");
  const [spaceId, setSpaceId] = useState<string>(fixedSpaceId !== null ? String(fixedSpaceId) : "none");
  const [error, setError] = useState<string | null>(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && (
        <CrudForm
          title="New task"
          description={`A task worktree in ${workspace.Title || workspace.Path}.`}
          submitLabel="Create task"
          onOpenChange={onOpenChange}
          submitTestId="dialog-new-task-submit"
          cancelTestId="dialog-new-task-cancel"
          onSubmit={async () => {
            if (!title.trim()) {
              setError("Title is required.");
              return false;
            }
            try {
              const task = await client!.call<Task>("task.create", {
                workspaceId: workspace.ID,
                ...(spaceId !== "none" ? { spaceId: Number(spaceId) } : {}),
                title: title.trim(),
              });
              onCreated(task);
              setTitle("");
              return true;
            } catch (err) {
              setError(err instanceof Error ? err.message : String(err));
              return false;
            }
          }}
        >
          <div className="grid gap-2">
            <label htmlFor="task-title" className="text-ui-base font-medium">
              Title
            </label>
            <Input
              id="task-title"
              data-testid="dialog-new-task-title-input"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
            />
          </div>
          {fixedSpaceId === null && (
            <div className="grid gap-2">
              <label htmlFor="task-space" className="text-ui-base font-medium">
                Space <span className="font-normal text-muted-foreground">(optional)</span>
              </label>
              <Select value={spaceId} onValueChange={setSpaceId}>
                <SelectTrigger id="task-space" data-testid="dialog-new-task-space-select" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="none">No space</SelectItem>
                  {spaces.map((s) => (
                    <SelectItem key={s.ID} value={String(s.ID)}>
                      {s.Title}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          <FormError message={error} />
        </CrudForm>
      )}
    </Dialog>
  );
}

/** Shared confirm copy for DeleteWorkspaceDialog/DeleteSpaceDialog: states plainly that this only affects smind's own tracking, never the real directory on disk. */
function DeleteConsequences({ tasksRemoved, spacesRemoved }: { tasksRemoved: number; spacesRemoved: number }) {
  const spaceClause = spacesRemoved > 0 ? `${spacesRemoved} space${spacesRemoved === 1 ? "" : "s"} and ` : "";
  return (
    <p className="mt-2">
      This removes {spaceClause}
      {tasksRemoved} task{tasksRemoved === 1 ? "" : "s"} from sMind only -- files on disk are untouched.
      Uncommitted work in any task worktree is checkpointed to its branch first. This cannot be undone in
      sMind.
    </p>
  );
}

export function DeleteWorkspaceDialog({
  client,
  workspace,
  tasksRemoved,
  spacesRemoved,
  open,
  onOpenChange,
  onDeleted,
}: {
  client: WsClient | null;
  workspace: Workspace | null;
  /** Total tasks about to be removed (every space's tasks plus ungrouped tasks) -- computed client-side from the already-loaded workspace tree, not a new round trip. */
  tasksRemoved: number;
  /** Spaces about to be removed, from the same already-loaded tree. */
  spacesRemoved: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted: () => void;
}) {
  const action = useConfirmAction(open);
  // The target is cleared on close; keep its name on screen while the dialog animates out.
  const shown = useLastDefined(workspace);

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Delete workspace?"
      description={
        <>
          <span className="font-medium text-foreground">{shown?.Title || shown?.Path}</span>
          <DeleteConsequences tasksRemoved={tasksRemoved} spacesRemoved={spacesRemoved} />
        </>
      }
      confirmLabel="Delete"
      pendingLabel="Deleting…"
      pending={action.pending}
      error={action.error}
      testId="dialog-delete-workspace"
      confirmTestId="dialog-delete-workspace-confirm"
      onConfirm={async () => {
        if (!workspace) return;
        if (await action.run(() => client!.call("workspace.delete", { id: workspace.ID }))) {
          onDeleted();
          onOpenChange(false);
        }
      }}
    />
  );
}

export function DeleteSpaceDialog({
  client,
  space,
  tasksRemoved,
  open,
  onOpenChange,
  onDeleted,
}: {
  client: WsClient | null;
  space: Space | null;
  /** Tasks about to be removed with this space -- computed client-side from the already-loaded workspace tree. */
  tasksRemoved: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted: () => void;
}) {
  const action = useConfirmAction(open);
  // The target is cleared on close; keep its name on screen while the dialog animates out.
  const shown = useLastDefined(space);

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Delete space?"
      description={
        <>
          <span className="font-medium text-foreground">{shown?.Title}</span>
          <DeleteConsequences tasksRemoved={tasksRemoved} spacesRemoved={0} />
        </>
      }
      confirmLabel="Delete"
      pendingLabel="Deleting…"
      pending={action.pending}
      error={action.error}
      testId="dialog-delete-space"
      confirmTestId="dialog-delete-space-confirm"
      onConfirm={async () => {
        if (!space) return;
        if (await action.run(() => client!.call("space.delete", { id: space.ID }))) {
          onDeleted();
          onOpenChange(false);
        }
      }}
    />
  );
}

/** task.tree run states as plain words -- Task.Status is the lifecycle field ("has ever run"), not what's happening now. */
function runStateWord(state: TaskTreeNode["run"]["state"]): string {
  switch (state) {
    case "running":
      return "running";
    case "waiting_permission":
      return "needs you";
    case "error":
      return "failed";
    case "done":
      return "done";
    case "stopped":
      return "stopped";
    case "idle":
      return "idle";
  }
}

/**
 * The live children of the task being archived (PR-U2), so the confirm
 * can say honestly what happens to them -- they stay and become
 * top-level (archiving is not a cascade, see the hierarchy plan's
 * Decision 2). Entries are `{ title, id }` plus the child's current run
 * state when it came from task.tree (preferred); a task.list fallback
 * carries no state at all, and the list renders titles only.
 */
function ArchiveChildren({ children }: { children: Array<{ id: number; title: string; runState?: TaskTreeNode["run"]["state"] }> }) {
  const MAX_VISIBLE = 8;
  const visible = children.slice(0, MAX_VISIBLE);
  const rest = children.length - visible.length;
  return (
    <div data-testid="archive-children" className="mt-2">
      <p className="font-normal">These tasks stay and become top-level:</p>
      <ul className="mt-1 list-disc pl-5">
        {visible.map((child) => (
          <li key={child.id}>
            {child.title}
            {child.runState && <> · {runStateWord(child.runState)}</>}
          </li>
        ))}
        {rest > 0 && <li>+{rest} more</li>}
      </ul>
    </div>
  );
}

export function ArchiveTaskDialog({
  client,
  task,
  open,
  onOpenChange,
  onArchived,
}: {
  client: WsClient | null;
  task: Task | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onArchived: () => void;
}) {
  const action = useConfirmAction(open);
  // The target is cleared on close; keep its name on screen while the dialog animates out.
  const shown = useLastDefined(task);
  // null = fetch pending/never ran, [] = no children (today's dialog).
  const [childTasks, setChildTasks] = useState<Array<{ id: number; title: string; runState?: TaskTreeNode["run"]["state"] }> | null>(null);
  const [childError, setChildError] = useState(false);

  useEffect(() => {
    if (!open || !task || !client) return;
    // Capture the world at open: a result landing after close or after the
    // task prop changed belongs to a different question.
    const reqTaskId = task.ID;
    let cancelled = false;
    setChildTasks(null);
    setChildError(false);
    // task.tree first: it carries each child's live run state (Task.Status
    // is the wrong field -- the lifecycle "has ever run"). On an older
    // daemon without it, fall back to task.list and show titles only.
    client
      .call<{ nodes: TaskTreeNode[] }>("task.tree", { taskId: task.ID })
      .then((result) => {
        if (cancelled || task.ID !== reqTaskId) return;
        setChildTasks((result.nodes ?? []).filter((n) => n.parentId === task.ID).map((n) => ({ id: n.id, title: n.title, runState: n.run.state })));
      })
      .catch(async (treeErr) => {
        console.error("task.tree failed, falling back to task.list (titles only)", treeErr);
        try {
          const list = await client.call<Task[]>("task.list", { workspaceId: task.WorkspaceID, parentTaskId: task.ID });
          if (cancelled || task.ID !== reqTaskId) return;
          setChildTasks((list ?? []).map((t) => ({ id: t.ID, title: t.Title })));
        } catch (listErr) {
          if (cancelled) return;
          console.error("task.list (children) failed, archive stays possible", listErr);
          setChildError(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, task, client]);

  const hasChildren = childTasks !== null && childTasks.length > 0;
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Archive task?"
      description={
        <>
          <span className="font-medium text-foreground">{shown?.Title}</span>
          <p className="mt-2">
            Uncommitted work is checkpointed to the task branch before the
            worktree is removed.
          </p>
          {hasChildren && <ArchiveChildren>{childTasks}</ArchiveChildren>}
          {childError && <p className="mt-2 font-normal text-foreground-muted">Could not check for child tasks.</p>}
        </>
      }
      confirmLabel={hasChildren ? "Archive parent only" : "Archive"}
      pendingLabel="Archiving…"
      pending={action.pending}
      error={action.error}
      testId="dialog-archive-task"
      confirmTestId="dialog-archive-task-confirm"
      onConfirm={async () => {
        if (!task) return;
        if (await action.run(() => client!.call("task.archive", { id: task.ID }))) {
          onArchived();
          onOpenChange(false);
        }
      }}
    />
  );
}

/**
 * ADR-0016 P3's "explicit action with a confirm" for archiving a chat --
 * mirrors ArchiveTaskDialog's own shape exactly (a chat.archive rejected
 * while the chat has a running run, internal/wsapi's own P1.2 guard,
 * surfaces here as this dialog's FormError, same as any other daemon
 * error). Closing the chat's tab afterwards is the caller's job (App.tsx's
 * onArchived), not this dialog's -- it only ever calls the RPC.
 */
export function ArchiveChatDialog({
  client,
  chat,
  open,
  onOpenChange,
  onArchived,
}: {
  client: WsClient | null;
  chat: Pick<Chat, "ID" | "Title"> | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onArchived: () => void;
}) {
  const action = useConfirmAction(open);
  // The target is cleared on close; keep its name on screen while the dialog animates out.
  const shown = useLastDefined(chat);

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Archive chat?"
      description={
        <>
          <span className="font-medium text-foreground">{shown?.Title || "Chat"}</span>
          <p className="mt-2">
            The chat's history stays on the task; its tab closes and it stops appearing in "New chat"'s reopen list.
          </p>
        </>
      }
      confirmLabel="Archive"
      pendingLabel="Archiving…"
      pending={action.pending}
      error={action.error}
      testId="dialog-archive-chat"
      confirmTestId="dialog-archive-chat-confirm"
      onConfirm={async () => {
        if (!chat) return;
        if (await action.run(() => client!.call("chat.archive", { id: chat.ID }))) {
          onArchived();
          onOpenChange(false);
        }
      }}
    />
  );
}
