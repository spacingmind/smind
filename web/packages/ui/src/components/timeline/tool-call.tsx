import { ToolCallCard } from "@/components/timeline/tool-call-card";
import { countHits, toolResultText, ToolCallDetail } from "@/components/timeline/tool-call-detail";
import { resolveToolRenderer, toolInput } from "@/components/timeline/tool-renderers";
import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";

/** Turns an absolute tool-call path into the worktree-relative wire path the file tab wants, or null when it points outside. */
export function worktreeRelativePath(absolute: string, worktreePath: string): string | null {
  if (!absolute) return null;
  if (!absolute.startsWith("/")) return absolute.replace(/^\.\//, "") || null;
  if (!worktreePath) return null;
  const root = worktreePath.endsWith("/") ? worktreePath : `${worktreePath}/`;
  if (!absolute.startsWith(root)) return null;
  const relative = absolute.slice(root.length);
  // A traversal that climbs back out isn't inside the worktree, whatever
  // the prefix says.
  return relative && !relative.split("/").includes("..") ? relative : null;
}

/**
 * One tool call, rendered through the registry: the renderer supplies the
 * icon, display name, summary and file path; `ToolCallDetail` supplies
 * the intent-specific body. Nothing here is keyed to a particular tool.
 *
 * Click-through calls `onOpenFile` with the worktree-relative path;
 * App.tsx wires that straight to the same `openFileTab` the file
 * explorer's row click uses, so it inherits Item 6's "prefer" placement
 * for free -- it opens in the side pane when the task already has one,
 * and in primary otherwise, without this file knowing the side dock
 * exists.
 */
export function ToolCall({
  item,
  worktreePath,
  onOpenFile,
}: {
  item: TimelineToolCallItem;
  worktreePath?: string;
  onOpenFile?: (path: string) => void;
}) {
  const renderer = resolveToolRenderer(item);
  const input = toolInput(item);
  let summary = renderer.summary?.(input) || undefined;
  // A search's card reads as a raw query otherwise -- the hit count folded
  // into the same summary line (not just the expanded detail) is what
  // makes it a semantic "N hits" title rather than an opaque pattern.
  if (renderer.intent === "search" && summary) {
    const hits = countHits(toolResultText(item));
    if (hits !== null) summary = `${summary} — ${hits} ${hits === 1 ? "hit" : "hits"}`;
  }

  const absolute = renderer.filePath?.(input);
  const relative = absolute && worktreePath ? worktreeRelativePath(absolute, worktreePath) : null;
  const onOpenPath = relative && onOpenFile ? () => onOpenFile(relative) : undefined;

  return (
    <ToolCallCard
      item={item}
      icon={renderer.icon}
      label={renderer.label}
      summary={summary}
      onOpenPath={onOpenPath}
    >
      <ToolCallDetail item={item} intent={renderer.intent} />
    </ToolCallCard>
  );
}
