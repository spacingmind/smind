import { useRef, useState } from "react";

import { expandHome, normalizeProjectPath, parentOf, pathProblem, samePath } from "@/lib/project-path";
import type { FsListDirResult, Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * What the folder check concluded about a typed path (C1 D3/D5), plus the
 * normalised absolute path it judged -- the same string submit sends. A
 * conclusion that cannot be reached is "unknown", never a refusal: the
 * daemon re-validates everything at create.
 */
export type FolderStatus = (
  | { kind: "empty" }
  | { kind: "relative" }
  | { kind: "checking" }
  | { kind: "git" }
  | { kind: "not-git" }
  | { kind: "missing" }
  | { kind: "unknown" }
  | { kind: "duplicate"; workspace: Workspace }
) & { path: string };

/** A listing failure meaning "that directory is not there" (the daemon's wording varies). */
const MISSING = /does not exist|no such file/i;

/** The typed path's own last segment (`/a/.b` -> `.b`), separator-agnostic. */
function lastSegment(path: string): string {
  return path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1);
}

/**
 * The Add-project dialog's folder check (C1 D3): judges a typed path before
 * submit. The daemon's `fs.listDir` says nothing about the *listed* directory
 * itself, so a typed path is judged from its parent's listing (the entry whose
 * path is the typed one). The client-side parts come first and cost no call:
 * empty and relative paths, `~` expansion (the daemon user's home, fetched
 * once per hook with `fs.listDir {}`), and a duplicate of a current workspace.
 * Every check increments a request id, and only the newest one may update
 * `status`, so a slow earlier answer cannot overwrite a later one.
 */
export function useFolderCheck(client: WsClient | null, workspaces: readonly Workspace[]) {
  const [status, setStatus] = useState<FolderStatus>({ kind: "empty", path: "" });
  const home = useRef<string | null>(null);
  const latest = useRef(0);

  async function check(raw: string): Promise<FolderStatus> {
    const request = ++latest.current;
    const settle = (next: FolderStatus): FolderStatus => {
      if (request === latest.current) setStatus(next);
      return next;
    };

    const trimmed = raw.trim();
    const problem = pathProblem(trimmed);
    if (problem === "empty") return settle({ kind: "empty", path: "" });
    if (problem === "relative") return settle({ kind: "relative", path: trimmed });

    let candidate = trimmed;
    if (trimmed.startsWith("~")) {
      let daemonHome = home.current;
      if (daemonHome === null) {
        settle({ kind: "checking", path: trimmed });
        try {
          daemonHome = (await client?.call<FsListDirResult>("fs.listDir", {}))?.path ?? null;
        } catch {
          daemonHome = null;
        }
        if (daemonHome === null) return settle({ kind: "unknown", path: trimmed });
        home.current = daemonHome;
      }
      candidate = expandHome(trimmed, daemonHome);
    }
    const path = normalizeProjectPath(candidate);
    settle({ kind: "checking", path });

    // A duplicate is decided against the current workspaces, before any fs call.
    const duplicate = workspaces.find((workspace) => samePath(workspace.Path, path));
    if (duplicate) return settle({ kind: "duplicate", workspace: duplicate, path });

    if (!client) return settle({ kind: "unknown", path });
    let listing: FsListDirResult;
    try {
      listing = await client.call<FsListDirResult>("fs.listDir", { path: parentOf(path) });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return settle(MISSING.test(message) ? { kind: "missing", path } : { kind: "unknown", path });
    }
    const entry = listing.entries.find((listed) => samePath(listed.path, path));
    if (entry) return settle({ kind: entry.isGitRepo ? "git" : "not-git", path });
    // The listing hides dot entries, so a hidden last segment cannot be judged here; the daemon decides at create.
    return settle(lastSegment(path).startsWith(".") ? { kind: "unknown", path } : { kind: "missing", path });
  }

  return { status, check, home: home.current };
}
