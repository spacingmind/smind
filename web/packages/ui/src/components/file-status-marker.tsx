import { cn } from "@/lib/utils";
import type { TaskFile } from "@/lib/types";

/**
 * The git decoration for one path in the explorer tree
 * (ui-redesign-parity plan, Item 17), sourced from the same `task.files`
 * data the diff pane fetches (see hooks/use-task-file-status.ts).
 *
 * A note on "untracked": `internal/workspace/git.go`'s taskChangedFiles
 * diffs a *snapshot index* against the task's base commit, which reports a
 * brand-new untracked file as `A` -- so the wire never says "untracked",
 * and the tree's meaningful distinction is added (A, green) vs modified
 * (M, amber) vs deleted (D, red). Any other git code (R for a rename,
 * say) is carried through lowercased by the daemon rather than collapsed,
 * so it's rendered as its own uppercase letter in the muted tier instead
 * of being mislabelled as one of the three.
 *
 * Colors come from the git-status token family (`--color-git-*`, ported
 * in P1 from ZCode's `workspace-file-tree/statusStyles.ts`), not the
 * generic success/warning/destructive tokens -- refs/zcode/DESIGN.md:
 * "Diff UI must use diff-specific semantic colors, not generic
 * success/destructive colors" applies the same way to git status.
 */
const STATUS_STYLE: Record<string, { letter: string; className: string; label: string }> = {
  added: { letter: "A", className: "text-git-added", label: "added" },
  modified: { letter: "M", className: "text-git-modified", label: "modified" },
  deleted: { letter: "D", className: "text-git-deleted", label: "deleted" },
};

export function FileStatusMarker({ status, className }: { status: TaskFile["status"]; className?: string }) {
  const style = STATUS_STYLE[status] ?? {
    letter: status.slice(0, 1).toUpperCase() || "?",
    className: "text-git-renamed",
    label: status,
  };
  return (
    <span
      data-testid="file-status-marker"
      data-status={status}
      title={style.label}
      aria-label={style.label}
      className={cn("shrink-0 font-mono text-ui-sm font-medium tabular-nums", style.className, className)}
    >
      {style.letter}
    </span>
  );
}
