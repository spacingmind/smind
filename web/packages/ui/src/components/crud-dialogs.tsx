import { useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { Space, Task, Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** Inline, per-dialog error text for a failed submit -- the daemon's message verbatim (e.g. workspace.create's "not a git repository"). */
function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return <p className="text-sm text-destructive">{message}</p>;
}

/** Shared submit row: Create disabled while in flight, Cancel always closes. */
function FormActions({
  label,
  pending,
  onCancel,
}: {
  label: string;
  pending: boolean;
  onCancel: () => void;
}) {
  return (
    <DialogFooter>
      <Button type="button" variant="outline" onClick={onCancel}>
        Cancel
      </Button>
      <Button type="submit" disabled={pending}>
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
}: {
  title: string;
  description?: string;
  submitLabel: string;
  onSubmit: () => Promise<boolean>;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
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
        <FormActions label={submitLabel} pending={pending} onCancel={() => onOpenChange(false)} />
      </form>
    </DialogContent>
  );
}

export function CreateWorkspaceDialog({
  client,
  open,
  onOpenChange,
  onCreated,
}: {
  client: WsClient | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the created workspace so the sidebar can refresh the tree (and expand the newcomer). */
  onCreated: (workspace: Workspace) => void;
}) {
  const [path, setPath] = useState("");
  const [title, setTitle] = useState("");
  const [error, setError] = useState<string | null>(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open && (
        <CrudForm
          title="New workspace"
          description="Point smind at an existing local git repository."
          submitLabel="Create workspace"
          onOpenChange={onOpenChange}
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
            <label htmlFor="workspace-path" className="text-sm font-medium">
              Path
            </label>
            <Input
              id="workspace-path"
              placeholder="/path/to/repo"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              autoFocus
            />
          </div>
          <div className="grid gap-2">
            <label htmlFor="workspace-title" className="text-sm font-medium">
              Title <span className="font-normal text-muted-foreground">(optional)</span>
            </label>
            <Input
              id="workspace-title"
              placeholder="Defaults to the repository directory name"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
          <FormError message={error} />
        </CrudForm>
      )}
    </Dialog>
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
          description="A grouping layer for tasks within the workspace."
          submitLabel="Create space"
          onOpenChange={onOpenChange}
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
            <label htmlFor="space-title" className="text-sm font-medium">
              Title
            </label>
            <Input
              id="space-title"
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
            <label htmlFor="task-title" className="text-sm font-medium">
              Title
            </label>
            <Input
              id="task-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoFocus
            />
          </div>
          {fixedSpaceId === null && (
            <div className="grid gap-2">
              <label htmlFor="task-space" className="text-sm font-medium">
                Space <span className="font-normal text-muted-foreground">(optional)</span>
              </label>
              <Select value={spaceId} onValueChange={setSpaceId}>
                <SelectTrigger id="task-space" className="w-full">
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
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Archive task?</DialogTitle>
          <DialogDescription asChild>
            <div>
              <span className="font-medium text-foreground">{task?.Title}</span>
              <p className="mt-2">
                Uncommitted work is checkpointed to the task branch before the
                worktree is removed.
              </p>
            </div>
          </DialogDescription>
        </DialogHeader>
        <FormError message={error} />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={pending}
            onClick={async () => {
              if (!task) return;
              setPending(true);
              setError(null);
              try {
                await client!.call("task.archive", { id: task.ID });
                onArchived();
                onOpenChange(false);
              } catch (err) {
                setError(err instanceof Error ? err.message : String(err));
              } finally {
                setPending(false);
              }
            }}
          >
            {pending ? "Archiving…" : "Archive"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
