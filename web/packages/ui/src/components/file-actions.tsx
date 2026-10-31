import { useCallback, useState } from "react";

import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import { downloadFile, PROGRESS_THRESHOLD_BYTES } from "@/lib/download-file";
import { FileChangedError, FileTooLargeError } from "@/lib/file-bytes";
import { desktop, isDesktop } from "@/lib/platform";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * The extensions the desktop app may open in the OS default application (ADR-0049 S15): the same allow-list `open_path.rs` enforces in
 * Rust (a test compares the two). Offering "Open in app" only for these keeps the button from promising what Rust will refuse.
 */
export const DESKTOP_OPENABLE_EXTENSIONS: readonly string[] = [
  "pdf", "docx", "doc", "xlsx", "xls", "pptx", "ppt", "odt", "ods", "odp", "rtf", "csv", "txt", "md", "png", "jpg", "jpeg", "gif", "webp",
];

export type FileActionId = "open-in-app" | "download" | "reveal" | "copy-path";

/**
 * Which actions a file offers here: Open in app and Reveal on a desktop build attached to the local daemon only (a remote daemon's
 * paths are not on this machine, so they are absent rather than disabled); Download on the web build only (the desktop replaces it
 * with Open in app and Reveal); Copy path everywhere.
 */
export function availableFileActions(o: { desktop: boolean; local: boolean; ext: string }): FileActionId[] {
  const ids: FileActionId[] = [];
  if (o.desktop && o.local && DESKTOP_OPENABLE_EXTENSIONS.includes(o.ext.toLowerCase())) ids.push("open-in-app");
  if (!o.desktop) ids.push("download");
  if (o.desktop && o.local) ids.push("reveal");
  ids.push("copy-path");
  return ids;
}

/** The absolute path of a worktree-relative file, in the separator style of the worktree root (best effort: for display and copy only). */
export function absolutePathOf(worktreePath: string | undefined, rel: string): string {
  if (!worktreePath) return rel;
  const sep = worktreePath.includes("\\") ? "\\" : "/";
  return `${worktreePath.replace(/[\\/]+$/, "")}${sep}${rel.split("/").join(sep)}`;
}

export interface FileActionTarget {
  client: WsClientLike | null;
  taskId: number;
  worktreePath?: string;
  /** Worktree-relative, forward slashes. */
  path: string;
  /** Lower-case, no dot. */
  ext: string;
  size: number;
  mtime: string;
}

/** The buttons for one file: Open in app, Download, Reveal, Copy path, filtered by `availableFileActions`. */
export function FileActionButtons({
  target,
  pathActions,
  size = "sm",
  testId = "file-actions",
}: {
  target: FileActionTarget;
  /** `useLocalPathActions()` of the surface (null on the web and on a remote connection). */
  pathActions: LocalPathActions | null;
  size?: "sm" | "default";
  testId?: string;
}) {
  const ids = availableFileActions({ desktop: isDesktop, local: pathActions !== null, ext: target.ext });
  const [progress, setProgress] = useState<number | null>(null);
  const abs = absolutePathOf(target.worktreePath, target.path);

  const download = useCallback(async () => {
    if (!target.client || progress !== null) return;
    setProgress(0);
    try {
      await downloadFile(target.client, {
        taskId: target.taskId,
        path: target.path,
        size: target.size,
        mtime: target.mtime,
        onProgress: target.size > PROGRESS_THRESHOLD_BYTES ? (read, total) => setProgress(Math.min(99, Math.floor((read / Math.max(1, total)) * 100))) : undefined,
      });
    } catch (err) {
      const description =
        err instanceof FileTooLargeError
          ? "This file is over the 50 MiB download limit."
          : err instanceof FileChangedError
            ? "The file changed while it was being downloaded. Try again."
            : err instanceof Error
              ? err.message
              : String(err);
      toast({ variant: "error", title: "Couldn't download the file", description });
    } finally {
      setProgress(null);
    }
  }, [target, progress]);

  const openInApp = useCallback(() => {
    if (!target.worktreePath) return;
    desktop.openPathDefault(target.worktreePath, target.path).catch((err: unknown) => {
      toast({ variant: "error", title: "Couldn't open the file", description: err instanceof Error ? err.message : String(err) });
    });
  }, [target.worktreePath, target.path]);

  const copyPath = useCallback(() => {
    navigator.clipboard
      .writeText(abs)
      .then(() => toast({ variant: "success", title: "Path copied" }))
      .catch(() => toast({ variant: "error", title: "Couldn't copy the path" }));
  }, [abs]);

  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid={testId}>
      {ids.map((id) => {
        switch (id) {
          case "open-in-app":
            return (
              <Button key={id} type="button" variant="outline" size={size} data-action={id} onClick={openInApp}>
                Open in app
              </Button>
            );
          case "download":
            return (
              <Button key={id} type="button" variant="outline" size={size} data-action={id} disabled={progress !== null || !target.client} onClick={() => void download()}>
                {progress === null ? "Download" : target.size > PROGRESS_THRESHOLD_BYTES ? `Downloading ${progress}%` : "Downloading…"}
              </Button>
            );
          case "reveal":
            return (
              <Button key={id} type="button" variant="outline" size={size} data-action={id} onClick={() => pathActions?.reveal(abs)}>
                {pathActions?.revealLabel ?? "Reveal in folder"}
              </Button>
            );
          case "copy-path":
            return (
              <Button key={id} type="button" variant="ghost" size={size} data-action={id} onClick={copyPath}>
                Copy path
              </Button>
            );
        }
      })}
    </div>
  );
}
