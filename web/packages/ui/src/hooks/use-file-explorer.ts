import { useCallback, useEffect, useRef, useState } from "react";

import type { WsClientLike } from "@/lib/ws-client";
import type { FileEntry, FileReadResult, Task } from "@/lib/types";

/**
 * Lazy-loaded state of one directory in the tree, keyed by its path
 * relative to the task's worktree root ("" is the root itself, joined
 * child paths use "/" regardless of platform since these are wire paths,
 * not filesystem paths).
 */
export interface DirNode {
  /** null while file.list for this directory hasn't resolved yet (or hasn't been requested at all). */
  entries: FileEntry[] | null;
  loading: boolean;
  error: string | null;
  expanded: boolean;
}

interface FileExplorerState {
  /** Every directory node seen so far. The root ("") is loaded eagerly on mount/task change; others load lazily on first expand -- see toggleDir. */
  dirs: Map<string, DirNode>;
  /** The currently-open file's path, or null if none is selected. */
  selectedPath: string | null;
  /** The editor's current buffer for selectedPath -- starts equal to the loaded content, then tracks local edits until save() (or a new selection) supersedes it. */
  content: string;
  /** True once content diverges from what file.read last returned (or what the last successful save() wrote). */
  dirty: boolean;
  /** True while file.read for the current selection is in flight. */
  contentLoading: boolean;
  contentError: string | null;
  /** True while file.write is in flight. */
  saving: boolean;
  saveError: string | null;
  /** Expands/collapses path, fetching file.list for it the first time it's expanded. */
  toggleDir: (path: string) => void;
  /** Selects path, fetching its content via file.read. */
  selectFile: (path: string) => void;
  /** Updates the local edit buffer (does not touch the server). */
  setContent: (content: string) => void;
  /** Writes content to selectedPath via file.write. Throws (leaving content untouched) on failure -- see saveError. */
  save: () => Promise<void>;
}

/** Guards an async continuation against a superseded selection -- same cancelled-flag pattern as useRunTimeline's Session. */
interface Session {
  cancelled: boolean;
}

const emptyDirNode: DirNode = { entries: null, loading: false, error: null, expanded: false };

function withDir(prev: Map<string, DirNode>, path: string, patch: Partial<DirNode>): Map<string, DirNode> {
  const next = new Map(prev);
  const existing = next.get(path) ?? emptyDirNode;
  next.set(path, { ...existing, ...patch });
  return next;
}

/**
 * Owns file.list/file.read/file.write for FileExplorerPane: a lazily
 * expanded directory tree plus a single open file's edit buffer. Resets
 * entirely whenever client or task.ID changes (a stale in-flight fetch from
 * a superseded task can never clobber the current one, via per-effect
 * Sessions -- the same guard useRunTimeline's task-switch handling uses).
 */
export function useFileExplorer(client: WsClientLike | null, task: Task | null): FileExplorerState {
  const [dirs, setDirs] = useState<Map<string, DirNode>>(new Map());
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [content, setContentState] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [contentLoading, setContentLoading] = useState(false);
  const [contentError, setContentError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Always-current client/task, readable from callbacks without needing
  // them in every useCallback's dependency array (which would otherwise
  // recreate toggleDir/selectFile/save on every unrelated render).
  const clientRef = useRef(client);
  clientRef.current = client;
  const taskRef = useRef(task);
  taskRef.current = task;

  const treeSessionRef = useRef<Session | null>(null);
  const fileSessionRef = useRef<Session | null>(null);
  const dirsRef = useRef(dirs);
  dirsRef.current = dirs;

  const loadDir = useCallback((path: string, session: Session) => {
    const c = clientRef.current;
    const t = taskRef.current;
    if (!c || !t) return;

    setDirs((prev) => withDir(prev, path, { loading: true, error: null }));

    c.call<FileEntry[]>("file.list", { taskId: t.ID, path })
      .then((entries) => {
        if (session.cancelled) return;
        setDirs((prev) => withDir(prev, path, { entries, loading: false, error: null }));
      })
      .catch((err: unknown) => {
        if (session.cancelled) return;
        setDirs((prev) => withDir(prev, path, { loading: false, error: err instanceof Error ? err.message : String(err) }));
      });
  }, []);

  useEffect(() => {
    const treeSession: Session = { cancelled: false };
    const fileSession: Session = { cancelled: false };
    treeSessionRef.current = treeSession;
    fileSessionRef.current = fileSession;

    setDirs(new Map());
    setSelectedPath(null);
    setContentState("");
    setSavedContent("");
    setContentLoading(false);
    setContentError(null);
    setSaving(false);
    setSaveError(null);

    if (client && task) {
      setDirs(new Map([["", { entries: null, loading: false, error: null, expanded: true }]]));
      loadDir("", treeSession);
    }

    return () => {
      treeSession.cancelled = true;
      fileSession.cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- task.ID (not task) is the intended dependency: a fresh Task object with the same ID (e.g. reloaded from task.get) shouldn't reset an already-loaded tree.
  }, [client, task?.ID, loadDir]);

  const toggleDir = useCallback(
    (path: string) => {
      // Read the pre-toggle snapshot from dirsRef (kept in sync every
      // render, like clientRef/taskRef above) rather than inside the
      // setDirs updater below: updater functions must stay pure, and
      // deciding whether to kick off loadDir is a side effect that
      // belongs outside it.
      const existing = dirsRef.current.get(path);
      const nowExpanded = !(existing?.expanded ?? false);

      setDirs((prev) => withDir(prev, path, { expanded: nowExpanded }));

      const session = treeSessionRef.current;
      if (session && nowExpanded && !existing?.entries && !existing?.loading) {
        loadDir(path, session);
      }
    },
    [loadDir],
  );

  const selectFile = useCallback((path: string) => {
    const c = clientRef.current;
    const t = taskRef.current;
    if (!c || !t) return;

    const session: Session = { cancelled: false };
    fileSessionRef.current = session;

    setSelectedPath(path);
    setContentState("");
    setSavedContent("");
    setContentLoading(true);
    setContentError(null);
    setSaveError(null);

    c.call<FileReadResult>("file.read", { taskId: t.ID, path })
      .then((result) => {
        if (session.cancelled) return;
        setContentState(result.content);
        setSavedContent(result.content);
        setContentLoading(false);
      })
      .catch((err: unknown) => {
        if (session.cancelled) return;
        setContentLoading(false);
        setContentError(err instanceof Error ? err.message : String(err));
      });
  }, []);

  const setContent = useCallback((next: string) => {
    setContentState(next);
  }, []);

  const save = useCallback(async () => {
    const c = clientRef.current;
    const t = taskRef.current;
    if (!c || !t || selectedPath === null) {
      throw new Error("no file selected");
    }

    setSaving(true);
    setSaveError(null);
    try {
      await c.call("file.write", { taskId: t.ID, path: selectedPath, content });
      setSavedContent(content);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
      throw err;
    } finally {
      setSaving(false);
    }
    // `content` must be read fresh at call time (the buffer at the moment
    // Save was clicked), so it's a real dependency here, not stale via a
    // ref -- unlike client/task, which don't need this callback to change
    // identity when they're unrelated to what's being saved.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedPath, content]);

  return {
    dirs,
    selectedPath,
    content,
    dirty: content !== savedContent,
    contentLoading,
    contentError,
    saving,
    saveError,
    toggleDir,
    selectFile,
    setContent,
    save,
  };
}
