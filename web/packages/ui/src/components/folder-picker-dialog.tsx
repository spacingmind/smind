import { AlertCircle, ChevronUp, Folder, FolderGit2, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { FsListDirResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * Server-side folder browser for "New workspace"'s Path field -- see
 * docs/plans/active/workspace-folder-picker.md. The web UI runs in a
 * browser, so it can't offer a real OS path string any other way: the File
 * System Access API's showDirectoryPicker() returns a sandboxed handle, not
 * a path workspace.create can use. This walks internal/hostfs's fs.listDir
 * instead -- the daemon's own host filesystem, unsandboxed, because there's
 * no workspace/worktree yet at the point this runs.
 *
 * "Use this folder" always acts on whatever path is currently displayed
 * (result.path), not a separate row-selection state -- clicking a row
 * navigates into it rather than "selecting" it. It only calls onSelect; it
 * deliberately never calls workspace.create itself, so the existing Create
 * flow's validation/error surfacing stays exactly as it is today.
 */
export function FolderPickerDialog({
  client,
  open,
  onOpenChange,
  onSelect,
}: {
  client: WsClientLike | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the currently displayed absolute path when "Use this folder" is clicked. */
  onSelect: (path: string) => void;
}) {
  const [result, setResult] = useState<FsListDirResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function navigate(path?: string): Promise<void> {
    if (!client) return;
    setLoading(true);
    setError(null);
    try {
      const r = path
        ? await client.call<FsListDirResult>("fs.listDir", { path })
        : await client.call<FsListDirResult>("fs.listDir");
      setResult(r);
    } catch (err) {
      // Deliberately don't clear `result` here: a failed navigation (e.g.
      // permission denied clicking into a row) shows the error inline but
      // leaves the last successful listing on screen, not a blank dialog.
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  // Opens at the daemon's home directory (no path param) every time the
  // dialog is opened -- mirroring AccountsDialog's same open-triggers-fetch
  // pattern.
  useEffect(() => {
    if (!open) return;
    setResult(null);
    setError(null);
    void navigate();
    // navigate is intentionally omitted: it's recreated every render but
    // only client/open should re-trigger the fetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, client]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Choose a folder</DialogTitle>
          <DialogDescription>
            Browse the daemon's filesystem for a local git repository.
          </DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="icon-sm"
            aria-label="Up"
            disabled={!result?.parent}
            onClick={() => result?.parent && void navigate(result.parent)}
          >
            <ChevronUp className="size-4" />
          </Button>
          <p className="min-w-0 flex-1 truncate text-sm font-medium" title={result?.path ?? ""}>
            {result?.path ?? "…"}
          </p>
        </div>

        {error && (
          <p className="flex items-center gap-1.5 text-sm text-destructive">
            <AlertCircle className="size-3.5 shrink-0" />
            {error}
          </p>
        )}

        <ScrollArea className="h-64 rounded-md border">
          {loading && !result ? (
            <p className="flex items-center gap-1.5 p-3 text-sm text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" />
              Loading…
            </p>
          ) : result && result.entries.length === 0 ? (
            <p className="p-3 text-sm text-muted-foreground">No subdirectories.</p>
          ) : (
            <ul>
              {result?.entries.map((entry) => (
                <li key={entry.path}>
                  <button
                    type="button"
                    data-testid="folder-row"
                    data-path={entry.path}
                    onClick={() => void navigate(entry.path)}
                    className="flex w-full items-center gap-1.5 px-3 py-1.5 text-left text-sm hover:bg-accent"
                  >
                    {entry.isGitRepo ? (
                      <FolderGit2 className="size-3.5 shrink-0 text-primary" />
                    ) : (
                      <Folder className="size-3.5 shrink-0" />
                    )}
                    <span className="min-w-0 truncate">{entry.name}</span>
                    {entry.isGitRepo && (
                      <span
                        data-testid="git-repo-indicator"
                        className="ml-auto shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[0.65rem] text-muted-foreground"
                      >
                        git
                      </span>
                    )}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </ScrollArea>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            type="button"
            disabled={!result}
            onClick={() => {
              if (!result) return;
              onSelect(result.path);
              onOpenChange(false);
            }}
          >
            Use this folder
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
