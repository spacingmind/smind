import { StatusDot } from "@/components/ui/status-dot";
import { pillStatusWithSubtree, statusSummary, usageChipText } from "@/lib/task-lineage";
import type { TaskLineage } from "@/hooks/use-task-lineage";

/**
 * The task header's hierarchy strip (task-hierarchy-tiers PR-U2): where
 * this task sits -- a "Spawned by" breadcrumb to its parent, a status
 * summary and usage rollup for its subtree, and one status pill per live
 * child, each jumping to that child. Renders nothing for a task with
 * neither parent nor children, so root leaf tasks keep today's header.
 */
export function TaskLineageHeader({
  lineage,
  onJumpToTask,
}: {
  lineage: TaskLineage;
  onJumpToTask?: (taskId: number) => void;
}) {
  const { parent, node, children } = lineage;
  const summary = node ? statusSummary(node.counts) : "";
  const usage = node ? usageChipText(node.usage.subtree) : null;
  if (!parent && children.length === 0 && !summary && !usage) return null;

  return (
    <div data-testid="task-lineage-header" className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-1.5 text-ui-sm text-foreground-muted">
      {parent && !parent.archived && (
        <button
          type="button"
          data-testid="task-spawned-by"
          onClick={() => onJumpToTask?.(parent.task.ID)}
          className="truncate rounded-full border px-2 py-0.5 transition-colors hover:bg-hover hover:text-foreground"
        >
          Spawned by {parent.task.Title}
        </button>
      )}
      {parent?.archived && (
        // An archived parent can't be selected -- inert muted text, not a
        // dead-looking button.
        <span data-testid="task-spawned-by" className="truncate">
          {parent.task.Title} (archived)
        </span>
      )}
      {summary && (
        <span data-testid="task-tree-summary" className="truncate">
          {summary}
        </span>
      )}
      {usage && node && (
        <span
          data-testid="task-tree-usage"
          title={`own: ${usageChipText(node.usage.own) ?? "—"} · tree: ${usageChipText(node.usage.subtree) ?? "—"}`}
          className="truncate rounded-full border px-2 py-0.5"
        >
          {usage}
        </span>
      )}
      {children.length > 0 && (
        <span className="flex min-w-0 flex-wrap items-center gap-1">
          {children.map((child) => (
            <button
              key={child.id}
              type="button"
              data-testid="task-child-pill"
              data-child-id={child.id}
              data-child-state={child.run.state}
              data-child-urgency={pillStatusWithSubtree(child)}
              onClick={() => onJumpToTask?.(child.id)}
              className="flex max-w-[12rem] items-center gap-1.5 truncate rounded-full border px-2 py-0.5 transition-colors hover:bg-hover hover:text-foreground"
            >
              <StatusDot status={pillStatusWithSubtree(child)} />
              <span className="truncate">{child.title}</span>
              {child.tier && <span className="shrink-0">{child.tier}</span>}
            </button>
          ))}
        </span>
      )}
    </div>
  );
}
