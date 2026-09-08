/**
 * The tab registry: the vocabulary of tab kinds App.tsx's tab strip is
 * rendered from, per docs/plans/active/tab-registry-side-dock.md. A tab is
 * data ({kind, key, taskId, title, closable}), not hardcoded JSX -- the
 * strip maps over a list of these, and adding a new kind later means
 * adding a descriptor here plus a renderer entry in App.tsx, never editing
 * the strip's layout logic.
 *
 * Keys are globally unique *and* task-scoped (docs/decisions/0004-per-task-editor-tabs.md):
 * `${taskId}:task`, `${taskId}:files`, `${taskId}:diff`,
 * `${taskId}:terminal`, `${taskId}:file:${path}` -- so the same file path
 * open in two tasks' tabs never collides, and per-task tab sets fall out
 * of the key structure naturally.
 */
export type TabKind = "task" | "files" | "file" | "diff" | "terminal";

/** One entry in a task's tab strip. */
export interface TabEntry {
  kind: TabKind;
  /** Globally unique, task-scoped -- see the file doc comment for the exact formats. */
  key: string;
  taskId: number;
  title: string;
  /** Falls back to TAB_KINDS[kind].closable when absent. */
  closable?: boolean;
}

/** Per-kind defaults: whether tabs of this kind get a close affordance, and their strip title when nothing better is known. */
export interface TabKindDescriptor {
  closable: boolean;
  defaultTitle: string;
}

export const TAB_KINDS: Record<TabKind, TabKindDescriptor> = {
  task: { closable: false, defaultTitle: "Chat" },
  files: { closable: false, defaultTitle: "Files" },
  diff: { closable: false, defaultTitle: "Diff" },
  terminal: { closable: false, defaultTitle: "Terminal" },
  file: { closable: true, defaultTitle: "File" },
};

/** The four always-present base tabs every task starts with (ADR 0004's per-task tab set), in strip order. */
export function defaultTabsForTask(taskId: number): TabEntry[] {
  return (["task", "files", "diff", "terminal"] as const).map((kind) => {
    const descriptor = TAB_KINDS[kind];
    return { kind, key: `${taskId}:${kind}`, taskId, title: descriptor.defaultTitle, closable: descriptor.closable };
  });
}

/** A closable editor tab for one file path (wire path, "/"-joined relative to the task's worktree root). */
export function fileTab(taskId: number, path: string): TabEntry {
  const segments = path.split("/");
  return {
    kind: "file",
    key: `${taskId}:file:${path}`,
    taskId,
    title: segments[segments.length - 1] || path,
    closable: TAB_KINDS.file.closable,
  };
}
