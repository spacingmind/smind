// The Add-project dialog's recents (C1, D4). One list per host (the desktop connection id or the
// page origin), persisted in localStorage, so switching daemons never mixes machines' folders. The
// host's current workspaces are merged in on every open -- that is what keeps a project in recents
// after it is removed from the daemon: the entry is already here, so removing the workspace removes
// nothing. The only way out is "remove from recents", which writes this store alone: the module
// imports nothing that can call an RPC, so it can never delete a project.

import { normalizeProjectPath } from "@/lib/project-path";

/** One remembered project: its normalised path, its title, and when it was last used (epoch ms). */
export interface RecentProject {
  path: string;
  title: string;
  at: number;
}

/** The workspace.list fields the merge and the visibility filter read (lib/types.ts's Workspace satisfies this). */
export interface WorkspaceSummary {
  Path: string;
  Title: string;
  CreatedAt: string;
}

interface StoredRecents {
  v: 1;
  items: RecentProject[];
  dismissed: string[];
}

const MAX_RECENTS = 8;

function storageKey(hostKey: string): string {
  return `smind:project-recents:${hostKey}`;
}

function isRecentProject(value: unknown): value is RecentProject {
  if (typeof value !== "object" || value === null) return false;
  const r = value as { path?: unknown; title?: unknown; at?: unknown };
  return typeof r.path === "string" && typeof r.title === "string" && typeof r.at === "number";
}

/** Parses and shape-checks a stored payload; anything unexpected (including a stale build's shape) reads as absent. */
function parseStored(raw: string | null): StoredRecents | null {
  if (raw === null) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return null;
    const s = parsed as { v?: unknown; items?: unknown; dismissed?: unknown };
    if (s.v !== 1 || !Array.isArray(s.items) || !Array.isArray(s.dismissed)) return null;
    if (!s.items.every(isRecentProject) || !s.dismissed.every((d) => typeof d === "string")) return null;
    return { v: 1, items: s.items, dismissed: s.dismissed };
  } catch {
    return null;
  }
}

/** The session's live state per host, seeded from storage on first use so a blocked write still applies for the session. */
const session = new Map<string, StoredRecents>();

function stateFor(hostKey: string): StoredRecents {
  const cached = session.get(hostKey);
  if (cached) return cached;
  let loaded: StoredRecents | null = null;
  if (typeof window !== "undefined") {
    try {
      loaded = parseStored(window.localStorage.getItem(storageKey(hostKey)));
    } catch {
      loaded = null;
    }
  }
  const state = loaded ?? { v: 1, items: [], dismissed: [] };
  session.set(hostKey, state);
  return state;
}

function persist(hostKey: string): void {
  const state = session.get(hostKey);
  if (state === undefined || typeof window === "undefined") return;
  try {
    window.localStorage.setItem(storageKey(hostKey), JSON.stringify(state));
  } catch {
    // Blocked storage (private browsing, quota, disabled): the session state above keeps the value until reload.
  }
}

function byNewestFirst(a: RecentProject, b: RecentProject): number {
  return b.at - a.at;
}

/** The host's recents, newest first. */
export function loadRecents(hostKey: string): RecentProject[] {
  return [...stateFor(hostKey).items].sort(byNewestFirst);
}

/** Upserts a project by normalised path: to the top, with `at` = `now`, and no longer dismissed. */
export function recordRecent(hostKey: string, entry: { path: string; title: string }, now: number = Date.now()): void {
  const state = stateFor(hostKey);
  const path = normalizeProjectPath(entry.path);
  state.items = state.items.filter((item) => item.path !== path);
  state.items.push({ path, title: entry.title, at: now });
  state.dismissed = state.dismissed.filter((d) => d !== path);
  state.items.sort(byNewestFirst);
  state.items = state.items.slice(0, MAX_RECENTS);
  persist(hostKey);
}

/**
 * Merges the host's current workspaces in: each unknown one becomes a recent (dated by CreatedAt,
 * unless it was dismissed), known ones keep their `at`. Capped at the newest 8.
 */
export function mergeWorkspaces(hostKey: string, workspaces: WorkspaceSummary[]): void {
  const state = stateFor(hostKey);
  for (const workspace of workspaces) {
    const path = normalizeProjectPath(workspace.Path);
    if (state.items.some((item) => item.path === path) || state.dismissed.includes(path)) continue;
    const created = Date.parse(workspace.CreatedAt);
    state.items.push({ path, title: workspace.Title, at: Number.isNaN(created) ? Date.now() : created });
  }
  state.items.sort(byNewestFirst);
  state.items = state.items.slice(0, MAX_RECENTS);
  persist(hostKey);
}

/** loadRecents minus the projects that are current workspaces (they are in the sidebar already). */
export function visibleRecents(hostKey: string, workspaces: WorkspaceSummary[]): RecentProject[] {
  const current = new Set(workspaces.map((workspace) => normalizeProjectPath(workspace.Path)));
  return loadRecents(hostKey).filter((item) => !current.has(item.path));
}

/** Deletes one entry and dismisses its path, so a later mergeWorkspaces does not bring it back. Never touches the daemon. */
export function removeRecent(hostKey: string, path: string): void {
  const state = stateFor(hostKey);
  const normalized = normalizeProjectPath(path);
  state.items = state.items.filter((item) => item.path !== normalized);
  if (!state.dismissed.includes(normalized)) state.dismissed.push(normalized);
  persist(hostKey);
}
