import type { ReactNode } from "react";
import { AlertCircle, ChevronRight, File, Folder, FolderOpen, Loader2 } from "lucide-react";

import { FileStatusMarker } from "@/components/file-status-marker";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { useFileExplorer, type DirNode } from "@/hooks/use-file-explorer";
import { useTaskFileStatus } from "@/hooks/use-task-file-status";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import type { Task, TaskFile } from "@/lib/types";

/**
 * The tree half of the file explorer: a lazily expanding directory tree
 * (file.list) over the task's worktree. Clicking a file now delegates to
 * onOpenFile instead of selecting an inline editor -- the editor half
 * moved to FileEditorPane, mounted per open file tab (see
 * docs/plans/active/tab-registry-side-dock.md). The explorer hook still
 * keeps selectedPath for row highlighting only.
 */
export function FileExplorerPane({
  client,
  task,
  onOpenFile,
}: {
  client: WsClientLike | null;
  task: Task;
  /** Called with the clicked file's worktree-relative path; App.tsx opens/activates its file tab. */
  onOpenFile?: (path: string) => void;
}) {
  const explorer = useFileExplorer(client, task);
  const { statusByPath } = useTaskFileStatus(client, task, events);
  const dirStatus = rollUpDirStatus(statusByPath);

  function revealInDiff(path: string): void {
    requestDiffReveal(task.ID, path);
    onRevealInDiff?.(path);
  }

  return (
    <div className="h-full min-h-0 overflow-y-auto" data-testid="file-explorer-pane">
      <DirChildren
        path=""
        depth={0}
        dirs={explorer.dirs}
        selectedPath={explorer.selectedPath}
        onToggleDir={explorer.toggleDir}
        onSelectFile={(path) => {
          explorer.selectFile(path);
          onOpenFile?.(path);
        }}
      />
    </div>
  );
}

/**
 * Rolls each changed path's status up its ancestor directories. A folder
 * whose descendants are all added reads as added; any mix reads as
 * modified, which is the honest summary for "something under here
 * changed" without inventing a fourth status. Recomputed per render off a
 * Map that only changes when task.files does.
 */
function rollUpDirStatus(statusByPath: Map<string, TaskFile["status"]>): Map<string, TaskFile["status"]> {
  const out = new Map<string, TaskFile["status"]>();
  for (const [path, status] of statusByPath) {
    const segments = path.split("/");
    for (let i = 1; i < segments.length; i++) {
      const dir = segments.slice(0, i).join("/");
      const existing = out.get(dir);
      out.set(dir, existing === undefined || existing === status ? status : "modified");
    }
  }
  return out;
}

/** Renders `dirs.get(path)`'s children -- files and, for each subdirectory, a toggleable row plus (if expanded) a recursive DirChildren for it. */
function DirChildren({
  path,
  depth,
  dirs,
  selectedPath,
  statusByPath,
  dirStatus,
  onToggleDir,
  onSelectFile,
  onRevealInDiff,
}: {
  path: string;
  depth: number;
  dirs: Map<string, DirNode>;
  selectedPath: string | null;
  statusByPath: Map<string, TaskFile["status"]>;
  dirStatus: Map<string, TaskFile["status"]>;
  onToggleDir: (path: string) => void;
  onSelectFile: (path: string) => void;
  onRevealInDiff: (path: string) => void;
}) {
  const node = dirs.get(path);
  if (!node) return null;

  if (node.error) {
    return <TreeRow depth={depth} icon={<AlertCircle className="size-3.5" />} label={node.error} className="text-destructive" />;
  }
  if (node.entries === null) {
    if (node.loading) {
      return <TreeRow depth={depth} icon={<Loader2 className="size-3.5 animate-spin" />} label="Loading…" />;
    }
    return null;
  }
  if (node.entries.length === 0) {
    // Sentence case, no parens/period -- docs/design.md's copy convention
    // (ui-redesign-parity plan, Item 2), same rule applied to every other
    // pane's empty/loading/error strings.
    return <TreeRow depth={depth} label="Empty" className="text-muted-foreground" />;
  }

  return (
    <>
      {node.entries.map((entry) => {
        const childPath = path ? `${path}/${entry.name}` : entry.name;

        if (!entry.isDir) {
          return (
            <RowContextMenu
              key={childPath}
              path={childPath}
              onRevealInDiff={onRevealInDiff}
              changed={statusByPath.has(childPath)}
            >
              <TreeRow
                depth={depth}
                icon={<FileIcon path={childPath} />}
                label={entry.name}
                status={statusByPath.get(childPath)}
                active={selectedPath === childPath}
                onClick={() => onSelectFile(childPath)}
                testId="file-row"
                dataPath={childPath}
              />
            </RowContextMenu>
          );
        }

        const childNode = dirs.get(childPath);
        const expanded = childNode?.expanded ?? false;
        return (
          <div key={childPath}>
            <TreeRow
              depth={depth}
              icon={
                <>
                  <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", expanded && "rotate-90")} />
                  {expanded ? <FolderOpen className="size-3.5" /> : <Folder className="size-3.5" />}
                </>
              }
              label={entry.name}
              status={dirStatus.get(childPath)}
              onClick={() => onToggleDir(childPath)}
              testId="dir-row"
              dataPath={childPath}
            />
            {expanded && (
              <DirChildren
                path={childPath}
                depth={depth + 1}
                dirs={dirs}
                selectedPath={selectedPath}
                statusByPath={statusByPath}
                dirStatus={dirStatus}
                onToggleDir={onToggleDir}
                onSelectFile={onSelectFile}
                onRevealInDiff={onRevealInDiff}
              />
            )}
          </div>
        );
      })}
    </>
  );
}

/**
 * The right-click menu on a file row. "Reveal in diff" is disabled for a
 * path with no changes rather than hidden, so the menu's shape doesn't
 * shift between rows. "Open to side" is deliberately absent until Item 6
 * lands the side dock -- a menu item that can't do anything is worse than
 * one that isn't there yet (see the plan's Item 17 notes).
 *
 * Clipboard writes go through navigator.clipboard when it exists and are
 * a silent no-op when it doesn't (jsdom, and any non-secure-context
 * browser) -- copying a path is not worth an error surface.
 */
function RowContextMenu({
  path,
  changed,
  onRevealInDiff,
  children,
}: {
  path: string;
  changed: boolean;
  onRevealInDiff: (path: string) => void;
  children: ReactNode;
}) {
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent data-testid="file-row-menu" data-path={path}>
        <ContextMenuItem
          disabled={!changed}
          onSelect={() => onRevealInDiff(path)}
          data-testid="file-menu-reveal-in-diff"
        >
          <GitCompare />
          Reveal in diff
        </ContextMenuItem>
        <ContextMenuItem
          onSelect={() => {
            void navigator.clipboard?.writeText(path).catch(() => {});
          }}
          data-testid="file-menu-copy-path"
        >
          <Copy />
          Copy path
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

function TreeRow({
  depth,
  icon,
  label,
  status,
  onClick,
  active,
  className,
  testId,
  dataPath,
  ...props
}: {
  depth: number;
  icon?: ReactNode;
  label: string;
  /** This path's git status, when it's one of the task's changed files -- rendered as a trailing A/M/D marker. */
  status?: TaskFile["status"];
  onClick?: () => void;
  active?: boolean;
  className?: string;
  testId?: string;
  dataPath?: string;
  // The rest is what Radix's ContextMenuTrigger injects through `asChild`
  // (onContextMenu, onPointerDown, ref, ...) -- the row's own props are
  // omitted from it so this isn't an unsatisfiable intersection.
} & Omit<ComponentProps<"div">, "onClick" | "onKeyDown" | "className">) {
  return (
    <div
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
      data-testid={testId}
      data-path={dataPath}
      onClick={onClick}
      onKeyDown={
        onClick
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onClick();
              }
            }
          : undefined
      }
      className={cn(
        "flex w-full items-center gap-1.5 truncate px-2 py-1 text-left text-sm",
        onClick && "cursor-pointer hover:bg-accent",
        active && "bg-accent font-medium",
        className,
      )}
      style={{ paddingLeft: `${depth * 14 + 8}px` }}
      {...props}
    >
      {icon}
      <span className="truncate">{label}</span>
    </div>
  );
}
