import type { TabKind } from "@/components/tab-registry";

/**
 * The URL shape: `#/workspace/<id>/task/<id>/<tabKind>[/<path-or-chatId>]`.
 *
 * Hash routing, per the plan: the daemon serves one embedded SPA
 * (`internal/server/web.go`), and a hash never leaves the browser, so no
 * server-side route table is needed to make a deep link work. workspaceId
 * is carried for a legible URL and so a future notification/CLI deep link
 * can name it; restoring a task only ever needs `taskId`, which is
 * globally unique, so a mismatched workspaceId segment is not treated as
 * an error.
 *
 * ADR-0016 replaced the bare `task` tab kind with per-chat tabs -- a
 * `chat` route segment carries the chat's own id (`.../chat/<chatId>`),
 * mirroring `file`'s own "kind carries an id" shape. ADR-0042's plugin
 * panels are deliberately not routable: a panel tab's identity lives in
 * the tab entry, not the URL.
 */
export interface Route {
  workspaceId: number;
  taskId: number;
  tab:
    | { kind: Exclude<TabKind, "file" | "chat" | "plugin"> }
    | { kind: "file"; path: string }
    | { kind: "chat"; chatId: number; requestId?: string };
}

const BASE_KINDS: readonly Exclude<TabKind, "file" | "chat" | "plugin">[] = ["files", "diff", "terminal", "pr"];

function isBaseKind(value: string): value is Exclude<TabKind, "file" | "chat" | "plugin"> {
  return (BASE_KINDS as readonly string[]).includes(value);
}

/** The `?card=` value's length cap: request ids are short opaque strings, so anything long is junk, not a request. */
const MAX_CARD_ID_CHARS = 200;

/** Parses `location.hash` (with or without the leading `#`) into a Route, or null for anything that doesn't parse. */
export function parseRoute(hash: string): Route | null {
  // A chat route may carry `?card=<requestId>` (a notification click-through's
  // exact target). Only the part before the first `?` is the route proper; the
  // query is read separately and only `card` is honoured, and only for a chat.
  const [routePart, queryPart] = hash.replace(/^#/, "").split("?", 2) as [string, string?];
  const segments = routePart.split("/").filter((s) => s !== "");
  // ["workspace", id, "task", id, kind, ...path-or-chatId?]
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
  if (kindRaw === "chat") {
    const chatId = Number(rest[0]);
    if (rest.length !== 1 || !Number.isFinite(chatId)) return null;
    const card = queryPart === undefined ? undefined : new URLSearchParams(queryPart).get("card");
    const requestId = card !== null && card !== "" && card.length <= MAX_CARD_ID_CHARS ? card : undefined;
    return requestId === undefined
      ? { workspaceId, taskId, tab: { kind: "chat", chatId } }
      : { workspaceId, taskId, tab: { kind: "chat", chatId, requestId } };
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
  if (route.tab.kind === "chat") {
    return route.tab.requestId === undefined
      ? `${base}/chat/${route.tab.chatId}`
      : `${base}/chat/${route.tab.chatId}?card=${encodeURIComponent(route.tab.requestId)}`;
  }
  return `${base}/${route.tab.kind}`;
}
