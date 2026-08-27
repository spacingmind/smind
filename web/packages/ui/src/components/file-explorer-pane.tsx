import type { ReactNode } from "react";
import { AlertCircle, ChevronRight, File, Folder, FolderOpen, Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";
import { CodeMirrorEditor } from "@/components/code-mirror-editor";
import { useFileExplorer, type DirNode } from "@/hooks/use-file-explorer";
import type { WsClientLike } from "@/lib/ws-client";
import type { Task } from "@/lib/types";

/**
 * A self-contained file explorer + editor pane: a collapsible directory
 * tree on the left (file.list, lazily expanding subdirectories) and a
 * CodeMirror 6 editor on the right for whichever file is selected
 * (file.read on select, file.write via the Save button or Ctrl/Cmd-S).
 *
 * Deliberately not wired into App.tsx/TaskDetailPane -- see
 * docs/plans/active/web-ui-file-explorer.md's Acceptance Criteria. This
 * takes the same `{ client, task }` shape TaskDetailPane does so that
 * follow-up integration is a drop-in once it happens.
 */
export function FileExplorerPane({ client, task }: { client: WsClientLike | null; task: Task }) {
  const explorer = useFileExplorer(client, task);

  return (
    <div className="flex h-full min-h-0" data-testid="file-explorer-pane">
      <div className="w-64 shrink-0 overflow-y-auto border-r">
        <DirChildren
          path=""
          depth={0}
          dirs={explorer.dirs}
          selectedPath={explorer.selectedPath}
          onToggleDir={explorer.toggleDir}
          onSelectFile={explorer.selectFile}
        />
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        {explorer.selectedPath === null ? (
          <div className="flex h-full items-center justify-center px-4 text-center text-sm text-muted-foreground">
            Select a file to view it.
          </div>
        ) : (
          <FileEditorView
            path={explorer.selectedPath}
            content={explorer.content}
            dirty={explorer.dirty}
            loading={explorer.contentLoading}
            error={explorer.contentError}
            saving={explorer.saving}
            saveError={explorer.saveError}
            onChange={explorer.setContent}
            onSave={explorer.save}
          />
        )}
      </div>
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
      {icon}
      <span className="truncate">{label}</span>
    </div>
  );
}

function FileEditorView({
  path,
  content,
  dirty,
  loading,
  error,
  saving,
  saveError,
  onChange,
  onSave,
}: {
  path: string;
  content: string;
  dirty: boolean;
  loading: boolean;
  error: string | null;
  saving: boolean;
  saveError: string | null;
  onChange: (content: string) => void;
  onSave: () => Promise<void>;
}) {
  function handleSave() {
    onSave().catch(() => {
      // saveError is already surfaced via the hook's state; nothing more to do here.
    });
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <span className="truncate text-sm font-medium" data-testid="file-editor-path">
          {path}
          {dirty && <span aria-label="unsaved changes"> *</span>}
        </span>
        <button
          type="button"
          onClick={handleSave}
          disabled={saving || !dirty}
          className="h-7 shrink-0 rounded-lg border border-input bg-background px-2.5 text-xs font-medium hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
      {error && <p className="px-3 py-2 text-sm text-destructive">{error}</p>}
      {saveError && <p className="px-3 py-2 text-sm text-destructive">save failed: {saveError}</p>}
      {loading ? (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</div>
      ) : (
        !error && <CodeMirrorEditor value={content} onChange={onChange} onSave={handleSave} testId="file-editor" />
      )}
    </div>
  );
}
