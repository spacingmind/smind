import type { ReactNode } from "react";
import { AlertCircle, ChevronRight, File, Folder, FolderOpen, Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { useFileExplorer, type DirNode } from "@/hooks/use-file-explorer";
import type { WsClientLike } from "@/lib/ws-client";
import type { Task } from "@/lib/types";

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

/** Renders `dirs.get(path)`'s children -- files and, for each subdirectory, a toggleable row plus (if expanded) a recursive DirChildren for it. */
function DirChildren({
  path,
  depth,
  dirs,
  selectedPath,
  onToggleDir,
  onSelectFile,
}: {
  path: string;
  depth: number;
  dirs: Map<string, DirNode>;
  selectedPath: string | null;
  onToggleDir: (path: string) => void;
  onSelectFile: (path: string) => void;
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
    return <TreeRow depth={depth} label="(empty)" className="text-muted-foreground" />;
  }

  return (
    <>
      {node.entries.map((entry) => {
        const childPath = path ? `${path}/${entry.name}` : entry.name;

        if (!entry.isDir) {
          return (
            <TreeRow
              key={childPath}
              depth={depth}
              icon={<File className="size-3.5" />}
              label={entry.name}
              active={selectedPath === childPath}
              onClick={() => onSelectFile(childPath)}
              testId="file-row"
              dataPath={childPath}
            />
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
                onToggleDir={onToggleDir}
                onSelectFile={onSelectFile}
              />
            )}
          </div>
        );
      })}
    </>
  );
}

function TreeRow({
  depth,
  icon,
  label,
  onClick,
  active,
  className,
  testId,
  dataPath,
}: {
  depth: number;
  icon?: ReactNode;
  label: string;
  onClick?: () => void;
  active?: boolean;
  className?: string;
  testId?: string;
  dataPath?: string;
}) {
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
    >
      <span className="shrink-0">{icon}</span>
      <span className="min-w-0 truncate">{label}</span>
    </div>
  );
}
