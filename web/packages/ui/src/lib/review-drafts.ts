import { useSyncExternalStore } from "react";

/** One unsent review comment: where it is, what line it's about, and what the reviewer wrote. */
export interface ReviewDraft {
  /** Stable within a session -- React key, and what removeReviewDraft takes. */
  id: string;
  /** Worktree-relative path of the file being reviewed. */
  path: string;
  /** The commented line's number on its side of the diff, or null for a line the diff doesn't number. */
  line: number | null;
  side: "old" | "new";
  /** The line's own text, quoted back in the submitted prompt so the agent sees what was meant without re-deriving it. */
  snippet: string;
  body: string;
}

/**
 * Per-task review drafts (ui-redesign-parity plan, Item 19).
 *
 * Held outside React for the reason the plan's own scenario names:
 * *"a draft comment survives collapsing the file and switching tabs"* --
 * and switching tabs **unmounts the diff pane** (App.tsx's Radix Tabs
 * don't force-mount inactive content), so component state could not
 * survive it by construction. Mirrored into localStorage on every
 * mutation, which also gets survival across a reload for free; Paseo
 * persists its review drafts for the same reason (`audit-paseo.md` §2).
 *
 * Keyed by task id, so drafts written against one task's diff can never
 * be submitted against another's.
 */
export const REVIEW_DRAFTS_STORAGE_KEY = "smind.review-drafts";

type DraftsByTask = Record<string, ReviewDraft[]>;

const listeners = new Set<() => void>();
const EMPTY: ReviewDraft[] = [];

let drafts: DraftsByTask = load();

function load(): DraftsByTask {
  try {
    const raw = window.localStorage.getItem(REVIEW_DRAFTS_STORAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    // Anything not shaped like the store is discarded rather than trusted:
    // this is user-visible state read back from a previous version of the
    // app, and a half-valid object would surface as a crash mid-render.
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    const out: DraftsByTask = {};
    for (const [taskId, value] of Object.entries(parsed as Record<string, unknown>)) {
      if (Array.isArray(value)) out[taskId] = value.filter(isDraft);
    }
    return out;
  } catch {
    // No localStorage (or unparseable JSON) -- drafts are a convenience,
    // never a correctness requirement.
    return {};
  }
}

function isDraft(value: unknown): value is ReviewDraft {
  const d = value as Partial<ReviewDraft> | null;
  return !!d && typeof d.id === "string" && typeof d.path === "string" && typeof d.body === "string";
}

function persist(): void {
  try {
    window.localStorage.setItem(REVIEW_DRAFTS_STORAGE_KEY, JSON.stringify(drafts));
  } catch {
    // Private-mode/quota failures must not break the in-memory store.
  }
}

function commit(next: DraftsByTask): void {
  drafts = next;
  persist();
  for (const listener of listeners) listener();
}

export function getReviewDrafts(taskId: number): ReviewDraft[] {
  return drafts[String(taskId)] ?? EMPTY;
}

export function addReviewDraft(taskId: number, draft: Omit<ReviewDraft, "id">): ReviewDraft {
  const entry: ReviewDraft = { ...draft, id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}` };
  const key = String(taskId);
  commit({ ...drafts, [key]: [...(drafts[key] ?? []), entry] });
  return entry;
}

export function removeReviewDraft(taskId: number, id: string): void {
  const key = String(taskId);
  const existing = drafts[key];
  if (!existing) return;
  const next = existing.filter((d) => d.id !== id);
  if (next.length === existing.length) return;
  commit({ ...drafts, [key]: next });
}

/** Drops every draft for a task -- what a successful submit does, so the same comments can't be sent twice. */
export function clearReviewDrafts(taskId: number): void {
  const key = String(taskId);
  if (!drafts[key]?.length) return;
  const next = { ...drafts };
  delete next[key];
  commit(next);
}

/** Test-only reset (in-memory and persisted both), so one test's drafts can't leak into the next. */
export function resetReviewDrafts(): void {
  commit({});
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * One task's drafts, re-rendering when they change. The snapshot is the
 * stored array itself, which is replaced (never mutated) on every write,
 * so useSyncExternalStore's identity check is the right comparison and a
 * task with no drafts always returns the same frozen empty array rather
 * than a fresh `[]` per call -- the latter would loop forever.
 */
export function useReviewDrafts(taskId: number): ReviewDraft[] {
  return useSyncExternalStore(
    subscribe,
    () => getReviewDrafts(taskId),
    () => EMPTY,
  );
}

/**
 * Renders every draft as the single prompt Item 19 asks for: one message
 * covering the whole review, grouped by file and quoting each commented
 * line, rather than one run per comment.
 */
export function buildReviewPrompt(drafts: ReviewDraft[]): string {
  const byPath = new Map<string, ReviewDraft[]>();
  for (const draft of drafts) {
    const list = byPath.get(draft.path);
    if (list) list.push(draft);
    else byPath.set(draft.path, [draft]);
  }

  const sections: string[] = [];
  for (const [path, list] of byPath) {
    const lines = list.map((d) => {
      const where = d.line === null ? path : `${path}:${d.line}`;
      const snippet = d.snippet.trim();
      const quoted = snippet ? `\n  > ${snippet}` : "";
      return `- ${where}${quoted}\n  ${d.body.trim()}`;
    });
    sections.push(`### ${path}\n${lines.join("\n")}`);
  }

  return [
    "Please address the following review comments on the current diff.",
    "",
    ...sections,
  ].join("\n");
}
