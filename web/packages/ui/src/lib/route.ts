import type { TabKind } from "@/components/tab-registry";

/**
 * The URL shape: `#/workspace/<id>/task/<id>/<tabKind>[/<path>]`.
 *
 * Hash routing, per the plan: the daemon serves one embedded SPA
 * (`internal/server/web.go`), and a hash never leaves the browser, so no
 * server-side route table is needed to make a deep link work. workspaceId
 * is carried for a legible URL and so a future notification/CLI deep link
 * can name it; restoring a task only ever needs `taskId`, which is
 * globally unique, so a mismatched workspaceId segment is not treated as
 * an error.
 */
export interface Route {
  workspaceId: number;
  taskId: number;
  tab: { kind: Exclude<TabKind, "file"> } | { kind: "file"; path: string };
}

const BASE_KINDS: readonly Exclude<TabKind, "file">[] = ["task", "files", "diff", "terminal"];

function isBaseKind(value: string): value is Exclude<TabKind, "file"> {
  return (BASE_KINDS as readonly string[]).includes(value);
}

/** Parses `location.hash` (with or without the leading `#`) into a Route, or null for anything that doesn't parse. */
export function parseRoute(hash: string): Route | null {
  const trimmed = hash.replace(/^#/, "");
  const segments = trimmed.split("/").filter((s) => s !== "");
  // ["workspace", id, "task", id, kind, ...path?]
  if (segments.length < 5) return null;
  const [workspaceLabel, workspaceIdRaw, taskLabel, taskIdRaw, kindRaw, ...rest] = segments;
  if (workspaceLabel !== "workspace" || taskLabel !== "task") return null;

  const workspaceId = Number(workspaceIdRaw);
  const taskId = Number(taskIdRaw);
  if (!Number.isFinite(workspaceId) || !Number.isFinite(taskId)) return null;

  if (kindRaw === "file") {
    if (rest.length === 0) return null;
    // The rest of the segments are the path, re-joined: a file path is
    // itself "/"-separated, so it was encoded one segment at a time and
    // must be decoded and rejoined the same way, not treated as one
    // opaque tail segment.
    const path = rest.map(decodeURIComponent).join("/");
    return { workspaceId, taskId, tab: { kind: "file", path } };
  }
  if (kindRaw === undefined || !isBaseKind(kindRaw) || rest.length > 0) return null;
  return { workspaceId, taskId, tab: { kind: kindRaw } };
}

/** The inverse of {@link parseRoute}: a Route back into a `#/...` string. */
export function formatRoute(route: Route): string {
  const base = `#/workspace/${route.workspaceId}/task/${route.taskId}`;
  if (route.tab.kind === "file") {
    const encoded = route.tab.path.split("/").map(encodeURIComponent).join("/");
    return `${base}/file/${encoded}`;
  }
  return `${base}/${route.tab.kind}`;
}
