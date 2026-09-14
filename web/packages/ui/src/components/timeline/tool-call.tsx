import { ToolCallCard } from "@/components/timeline/tool-call-card";
import { ToolCallDetail } from "@/components/timeline/tool-call-detail";
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
 * Click-through opens the file in the task's primary tab set. Item 6's
 * side dock (and its `prefer` placement) hasn't landed, so this
 * deliberately degrades to "open the tab" rather than blocking on it.
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
  const summary = renderer.summary?.(input) || undefined;

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
