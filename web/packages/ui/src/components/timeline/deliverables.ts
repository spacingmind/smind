import { resolveToolRenderer, toolInput } from "@/components/timeline/tool-renderers";
import type { TimelineItem } from "@/hooks/use-run-timeline";
import type { FileStatResult } from "@/lib/types";

/**
 * Deliverable detection (ADR-0049 section 4): which files did this run produce? Client-side and deterministic, no wire change.
 * Candidates come from two places only: the `path` of every *successful* write-intent tool call, and the workspace-relative paths the
 * run's final assistant message names (markdown links, inline-code spans). A shell command's text is never parsed. The daemon then
 * verifies each candidate (`file.stat`, see `isDeliverable`), so a path that merely looks right never becomes a card.
 */

/** The extensions a card may be made for (S14). Lower-case, no dot. */
export const DELIVERABLE_EXTENSIONS: readonly string[] = ["pdf", "docx", "xlsx", "pptx", "md", "txt", "csv", "html", "png", "jpg", "jpeg", "gif", "webp"];

/** At most this many cards per run; the rest are summarised as "+ N more files". */
export const MAX_CARDS = 6;
/** At most this many candidates are checked with `file.stat` per run. */
export const MAX_CANDIDATES = 20;

const CONTROL = /[\u0000-\u001f\u007f]/;
const SCHEME = /^[a-zA-Z][a-zA-Z0-9+.-]*:/;
const WINDOWS_ABSOLUTE = /^[A-Za-z]:\//;
const INLINE_CODE_PATH = /^[\w./@+-][\w ./@+-]*\.[A-Za-z0-9]{1,5}$/;

/** The lower-case extension of a relative path, without the dot; "" when there is none. */
export function extensionOf(path: string): string {
  const name = path.split("/").pop() ?? "";
  const dot = name.lastIndexOf(".");
  return dot <= 0 ? "" : name.slice(dot + 1).toLowerCase();
}

/** The worktree root as a comparable forward-slash string (a verbatim `\\?\C:\...` prefix dropped), or "" when unusable. */
function comparableRoot(worktreePath: string | undefined): string {
  if (!worktreePath) return "";
  let root = worktreePath.replace(/\\/g, "/");
  if (root.startsWith("//?/") && WINDOWS_ABSOLUTE.test(root.slice(4))) root = root.slice(4);
  return root.replace(/\/+$/, "");
}

/**
 * A worktree-relative path (forward slashes, no leading `./`), or null when `raw` cannot be one: empty, control characters, `~`, a URL
 * scheme, a UNC path, an absolute path outside the worktree, a `..` segment, a trailing slash (a directory) or nothing left after
 * normalisation. An absolute path *under* the worktree is converted (case-insensitively for a Windows root).
 */
export function normalizeCandidate(raw: string, worktreePath?: string): string | null {
  let s = raw.trim();
  if (s.startsWith("<") && s.endsWith(">")) s = s.slice(1, -1).trim();
  if (s === "" || s.length > 512 || CONTROL.test(s)) return null;
  if (s.startsWith("~")) return null;
  const forward = s.replace(/\\/g, "/");
  // "C:/x" is a drive path, handled below; any other "word:" is a scheme (https:, file:, data:) or a drive-relative "C:x".
  if (SCHEME.test(forward) && !WINDOWS_ABSOLUTE.test(forward)) return null;
  if (forward.startsWith("//")) return null; // UNC
  if (forward.endsWith("/")) return null;

  let rel = forward;
  if (forward.startsWith("/") || WINDOWS_ABSOLUTE.test(forward)) {
    const root = comparableRoot(worktreePath);
    if (!root) return null;
    const windows = WINDOWS_ABSOLUTE.test(root);
    const [a, b] = windows ? [forward.toLowerCase(), root.toLowerCase()] : [forward, root];
    if (!a.startsWith(b + "/")) return null;
    rel = forward.slice(root.length + 1);
  }
  const parts: string[] = [];
  for (const part of rel.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") return null;
    parts.push(part);
  }
  return parts.length === 0 ? null : parts.join("/");
}

/** Text with fenced code blocks removed: a path in a code fence is an example, not a deliverable. */
function withoutFences(text: string): string {
  return text.replace(/```[\s\S]*?```/g, " ").replace(/~~~[\s\S]*?~~~/g, " ");
}

/** Worktree-relative paths named in an assistant message: markdown link targets, then inline-code spans, in reading order. */
export function pathsInMessage(text: string, worktreePath?: string): string[] {
  const body = withoutFences(text);
  const found: Array<{ at: number; path: string }> = [];
  const link = /\[[^\]]*\]\((<[^>]+>|[^)\s]+)(?:\s+"[^"]*")?\)/g;
  for (let m = link.exec(body); m; m = link.exec(body)) {
    let target = m[1];
    const hash = target.indexOf("#");
    if (hash >= 0) target = target.slice(0, hash);
    try {
      target = decodeURIComponent(target);
    } catch {
      // keep the raw target
    }
    const p = normalizeCandidate(target, worktreePath);
    if (p) found.push({ at: m.index, path: p });
  }
  const code = /`([^`\n]+)`/g;
  for (let m = code.exec(body); m; m = code.exec(body)) {
    const span = m[1];
    if (!INLINE_CODE_PATH.test(span) || span.includes("..") || span.startsWith("/")) continue;
    const p = normalizeCandidate(span, worktreePath);
    if (p) found.push({ at: m.index, path: p });
  }
  return found.sort((x, y) => x.at - y.at).map((f) => f.path);
}

/** The tool names that edit-intent covers but that never leave a new file at their path. */
const NOT_A_WRITE = new Set(["delete", "move"]);

/**
 * Candidate paths for one run, deduplicated, in order: successful write-intent tool calls in transcript order, then the final
 * assistant message's paths (only once the run is over: before that the message is not final). Bash and every other tool's text is
 * never parsed.
 */
export function extractCandidates(items: readonly TimelineItem[], worktreePath: string | undefined, runOver: boolean): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  const add = (p: string | null) => {
    if (p && !seen.has(p)) {
      seen.add(p);
      out.push(p);
    }
  };
  let finalMessage: string | undefined;
  for (const item of items) {
    if (item.kind === "assistant") finalMessage = item.text;
    if (item.kind !== "tool_call" || item.status !== "success") continue;
    if (item.toolName && NOT_A_WRITE.has(item.toolName.toLowerCase())) continue;
    const renderer = resolveToolRenderer(item);
    if (renderer.intent !== "edit") continue;
    const raw = renderer.filePath?.(toolInput(item));
    if (raw) add(normalizeCandidate(raw, worktreePath));
  }
  if (runOver && finalMessage) for (const p of pathsInMessage(finalMessage, worktreePath)) add(p);
  return out;
}

/** The candidates worth a `file.stat`: allow-listed extension, at most MAX_CANDIDATES. */
export function checkable(candidates: readonly string[]): string[] {
  return candidates.filter((p) => DELIVERABLE_EXTENSIONS.includes(extensionOf(p))).slice(0, MAX_CANDIDATES);
}

/**
 * Whether a verified stat makes a card (AC24): the file exists (a stat at all means a regular file inside the worktree), has an
 * allow-listed extension, is not a mismatch, and was written at or after the run started (both clocks are the daemon's).
 */
export function isDeliverable(stat: FileStatResult, runStartedAt: string): boolean {
  if (stat.mismatch) return false;
  if (!DELIVERABLE_EXTENSIONS.includes(stat.ext.replace(/^\./, "").toLowerCase())) return false;
  const modified = Date.parse(stat.mtime);
  const started = Date.parse(runStartedAt);
  if (Number.isNaN(modified) || Number.isNaN(started)) return false;
  return modified >= started;
}

/** A file that earned a card. */
export interface Deliverable {
  path: string;
  name: string;
  ext: string;
  size: number;
  mtime: string;
  kind: FileStatResult["kind"];
}

/** The cards to show and how many more verified files there are (`+ N more files`). */
export function limitCards<T>(verified: readonly T[]): { cards: T[]; more: number } {
  return { cards: verified.slice(0, MAX_CARDS), more: Math.max(0, verified.length - MAX_CARDS) };
}

/** A card's type label ("PDF", "Word", ...), shown with the size: "PDF · 240 KB". */
export function typeLabel(ext: string): string {
  switch (ext) {
    case "pdf":
      return "PDF";
    case "docx":
      return "Word";
    case "xlsx":
      return "Excel";
    case "pptx":
      return "PowerPoint";
    case "md":
      return "Markdown";
    case "txt":
      return "Text";
    case "csv":
      return "CSV";
    case "html":
      return "HTML";
    default:
      return ext.toUpperCase();
  }
}

/** "240 KB", "1.4 MB", "812 bytes". */
export function formatSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(bytes >= 10 * 1024 * 1024 ? 0 : 1)} MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${bytes} bytes`;
}

/** Extensions the app shows in a file tab itself (PDF preview or the editor); the rest need an external app. */
export function hasInAppView(ext: string): boolean {
  return !["docx", "xlsx", "pptx"].includes(ext);
}
