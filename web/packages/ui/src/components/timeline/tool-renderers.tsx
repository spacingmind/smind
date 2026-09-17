import {
  FileDiff,
  FileText,
  Globe,
  Search,
  SquareTerminal,
  Wrench,
  type LucideIcon,
} from "lucide-react";

import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";

/**
 * The shared render intents Item 9 names. A tool is rendered by what it
 * *does*, not by which provider named it: Claude's `Bash` and ACP's
 * `execute` are one intent with one card body, so the transcript looks
 * the same whichever backend produced it.
 */
export type ToolIntent = "terminal" | "read" | "edit" | "search" | "fetch" | "generic";

export interface ToolRenderer {
  intent: ToolIntent;
  icon: LucideIcon;
  /** Display name override; defaults to the wire tool name. */
  label?: string;
  /** The card's one-line summary, from the call's own input. Return "" to fall back to the daemon-supplied title. */
  summary?: (input: Record<string, unknown>) => string;
  /** The absolute file path this call is about, if any -- what makes the card click-through. */
  filePath?: (input: Record<string, unknown>) => string | undefined;
}

/**
 * The registry, keyed by **wire tool name** across both vocabularies
 * (Claude's `Bash`/`Read`/…, ACP's `execute`/`read`/…), following
 * `audit-deepseek-harness.md` §2.
 *
 * Adding a renderer is a `registerToolRenderer` call — from this module,
 * a future one, or a test — and never an edit to a central switch. That
 * is the whole point of the shape: the card component below resolves
 * through this map and knows nothing about any specific tool.
 */
const RENDERERS = new Map<string, ToolRenderer>();

/** Lower-cased so `Bash`/`bash` and `Read`/`read` resolve to the same entry across the two vocabularies. */
function key(name: string): string {
  return name.trim().toLowerCase();
}

export function registerToolRenderer(name: string, renderer: ToolRenderer): void {
  RENDERERS.set(key(name), renderer);
}

/** Test seam: drops a registration so a test's fixture doesn't leak into the next one. */
export function unregisterToolRenderer(name: string): void {
  RENDERERS.delete(key(name));
}

function str(input: Record<string, unknown>, ...names: string[]): string | undefined {
  for (const name of names) {
    const value = input[name];
    if (typeof value === "string" && value !== "") return value;
  }
  return undefined;
}

export function num(input: Record<string, unknown>, ...names: string[]): number | undefined {
  for (const name of names) {
    const value = input[name];
    if (typeof value === "number" && Number.isFinite(value)) return value;
  }
  return undefined;
}

/** A tool call's input as a plain object -- providers send whatever they like, so anything else is "no usable input". */
export function toolInput(item: TimelineToolCallItem): Record<string, unknown> {
  const input = item.input;
  if (input && typeof input === "object" && !Array.isArray(input)) return input as Record<string, unknown>;
  return {};
}

const TERMINAL: ToolRenderer = {
  intent: "terminal",
  icon: SquareTerminal,
  summary: (input) => str(input, "command", "cmd") ?? "",
};

const READ: ToolRenderer = {
  intent: "read",
  icon: FileText,
  summary: (input) => {
    const path = str(input, "file_path", "filePath", "path", "abs_path");
    if (!path) return "";
    const offset = num(input, "offset", "start_line");
    const limit = num(input, "limit", "line_count");
    if (offset === undefined && limit === undefined) return path;
    const from = offset ?? 1;
    return limit === undefined ? `${path}:${from}` : `${path}:${from}-${from + limit - 1}`;
  },
  filePath: (input) => str(input, "file_path", "filePath", "path", "abs_path"),
};

const EDIT: ToolRenderer = {
  intent: "edit",
  icon: FileDiff,
  summary: (input) => str(input, "file_path", "filePath", "path", "abs_path") ?? "",
  filePath: (input) => str(input, "file_path", "filePath", "path", "abs_path"),
};

const SEARCH: ToolRenderer = {
  intent: "search",
  icon: Search,
  summary: (input) => {
    const query = str(input, "pattern", "query", "regex");
    const scope = str(input, "path", "glob", "include");
    if (!query) return scope ?? "";
    return scope ? `${query} in ${scope}` : query;
  },
};

const FETCH: ToolRenderer = {
  intent: "fetch",
  icon: Globe,
  summary: (input) => str(input, "url", "query") ?? "",
};

export const GENERIC_RENDERER: ToolRenderer = { intent: "generic", icon: Wrench };

// The built-ins, registered rather than switched on. Both vocabularies
// map onto the same six intents: Claude's explicit tool names and ACP's
// ToolKind strings (internal/taskrunner/runner.go sends u.Kind as the
// tool name, since ACP has no separate name field).
for (const name of ["Bash", "BashOutput", "KillShell", "execute"]) registerToolRenderer(name, TERMINAL);
for (const name of ["Read", "NotebookRead", "read"]) registerToolRenderer(name, READ);
for (const name of ["Edit", "MultiEdit", "Write", "NotebookEdit", "edit", "delete", "move"]) {
  registerToolRenderer(name, EDIT);
}
for (const name of ["Grep", "Glob", "search"]) registerToolRenderer(name, SEARCH);
for (const name of ["WebFetch", "WebSearch", "fetch"]) registerToolRenderer(name, FETCH);

/**
 * Classifies an unregistered tool by the *shape* of its arguments, which
 * is what Item 9 asks for before falling back to the generic card: a
 * `command` is a terminal call whatever it's named, a `file_path` plus
 * replacement text is an edit, and so on. Order matters — edit is checked
 * before read, since an edit's input also carries a path.
 */
export function classifyByShape(input: Record<string, unknown>): ToolRenderer | null {
  if (str(input, "command", "cmd")) return TERMINAL;
  if (str(input, "old_string", "new_string", "new_text", "content", "contents")) return EDIT;
  if (str(input, "file_path", "filePath", "abs_path")) return READ;
  if (str(input, "pattern", "query", "regex")) return SEARCH;
  if (str(input, "url")) return FETCH;
  if (str(input, "path")) return READ;
  return null;
}

/**
 * The renderer for one tool call: registry first, shape-based
 * classification second, generic card last. Never returns null — an
 * unknown tool is a rendering problem, not an error.
 */
export function resolveToolRenderer(item: TimelineToolCallItem): ToolRenderer {
  const registered = item.toolName ? RENDERERS.get(key(item.toolName)) : undefined;
  if (registered) return registered;
  return classifyByShape(toolInput(item)) ?? GENERIC_RENDERER;
}
