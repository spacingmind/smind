import { StatusDot } from "@/components/ui/status-dot";
import { pillStatusWithSubtree, rateLimitHint, statusSummary, usageChipText } from "@/lib/task-lineage";
import type { TaskLineage } from "@/hooks/use-task-lineage";

/**
 * The task's hierarchy chips (task-hierarchy-tiers PR-U2): the subtree usage
 * total (labelled "Task", so it reads apart from the chat's own usage pill
 * beside it), a "Spawned by" breadcrumb to the parent, a status summary for
 * the subtree, and one status pill per live child, each jumping to that
 * child. A fragment of chips with no row chrome of its own, so the chat pane
 * can put them in its single meta row (chat-pane-single-meta-row); renders
 * nothing for a task with neither parent, children nor usage.
 */
export function TaskLineageChips({
  lineage,
  onJumpToTask,
}: {
  lineage: TaskLineage;
  onJumpToTask?: (taskId: number) => void;
}) {
  const { parent, node, children, nodes } = lineage;
  const summary = node ? statusSummary(node.counts) : "";
  const usage = node ? usageChipText(node.usage.subtree) : null;
  // ADR-0041: this task's own latest run hit a provider quota.
  const limitHint = node ? rateLimitHint(node, new Date()) : null;
  if (!parent && children.length === 0 && !summary && !usage && !limitHint) return null;

  return (
    <>
      {usage && node && (
        <span
          data-testid="task-tree-usage"
          title={`Task total (this task and its subtree) -- own: ${usageChipText(node.usage.own) ?? "—"} · tree: ${usageChipText(node.usage.subtree) ?? "—"}`}
          className="truncate rounded-full border px-2 py-0.5"
        >
          Task {usage}
        </span>
      )}
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
      {limitHint && (
        <span data-testid="task-rate-limit" className="truncate text-warning">
          {limitHint}
        </span>
      )}
      {/* Below sm the children are one tap away in the sidebar tree; keeping their pills would turn the meta row into a column. */}
      {children.length > 0 && (
        <span className="flex min-w-0 flex-wrap items-center gap-1 max-sm:hidden">
          {children.map((child) => {
            const childHint = rateLimitHint(child, new Date());
            return (
            <button
              key={child.id}
              type="button"
              data-testid="task-child-pill"
              data-child-id={child.id}
              data-child-state={child.run.state}
              data-child-urgency={pillStatusWithSubtree(child, nodes)}
              data-child-rate-limited={childHint ? "true" : undefined}
              title={childHint ?? undefined}
              onClick={() => onJumpToTask?.(child.id)}
              className="flex max-w-[12rem] items-center gap-1.5 truncate rounded-full border px-2 py-0.5 transition-colors hover:bg-hover hover:text-foreground"
            >
              <StatusDot status={pillStatusWithSubtree(child, nodes)} />
              <span className="truncate">{child.title}</span>
              {child.tier && <span className="shrink-0">{child.tier}</span>}
            </button>
            );
          })}
        </span>
      )}
    </>
  );
}

/** The chips as their own bordered strip (kept for callers that want a standalone row). */
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
      <TaskLineageChips lineage={lineage} onJumpToTask={onJumpToTask} />
    </div>
  );
}
