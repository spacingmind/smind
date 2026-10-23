import type { CheckItem, CheckLogTail, PRDetails, ReviewThread } from "@/lib/types";
import { CHECK_LOG_LABEL, PR_REVIEW_LABEL, quoteUntrusted } from "@/lib/untrusted-quote";

/**
 * The prompt text "Send to chat" puts in the composer for a PR thread or check (docs/plans/active/pr-panel.md,
 * decision 8). Pure: everything GitHub-authored (comment text, author, the check's name and log) goes inside the
 * quoted block; the lines built here outside the fence carry only the PR number, the repo, the thread's path and
 * line, our own status words and the daemon-validated URL. The PR title is third-party text too, so it is not
 * repeated outside the fence (the `pr` chip carries it).
 */

export interface PRFileRef {
  path: string;
  line?: number;
}

const TRUNCATED_NOTE = "[truncated]";

function threadStatus(thread: ReviewThread): string {
  if (thread.isResolved) return thread.isOutdated ? "resolved, outdated" : "resolved";
  return thread.isOutdated ? "outdated" : "open";
}

function location(thread: ReviewThread): string {
  return thread.line !== null && thread.line > 0 ? `${thread.path}:${thread.line}` : thread.path;
}

function fileRefOf(thread: ReviewThread): PRFileRef | null {
  if (!thread.path) return null;
  return thread.line !== null && thread.line > 0 ? { path: thread.path, line: thread.line } : { path: thread.path };
}

function threadBlock(details: PRDetails, thread: ReviewThread): string {
  const context = `PR #${details.number} (${details.repo}) — review thread on ${location(thread)} (${threadStatus(thread)})`;
  const comments = thread.comments.map((c) => `${c.author}: ${c.body}${c.bodyTruncated ? ` ${TRUNCATED_NOTE}` : ""}`);
  if (thread.commentsTruncated) comments.push(`${TRUNCATED_NOTE} more comments not shown`);
  return `${context}\n${quoteUntrusted(PR_REVIEW_LABEL, comments.join("\n\n"))}`;
}

/** One review thread: context line, then its comments quoted. */
export function threadToChat(details: PRDetails, thread: ReviewThread): { text: string; fileRef: PRFileRef | null } {
  return { text: threadBlock(details, thread), fileRef: fileRefOf(thread) };
}

/** A check: context line (state, daemon-validated URL), then its name and log tail (or status) quoted. */
export function checkToChat(details: PRDetails, check: CheckItem, logTail?: CheckLogTail | null): { text: string } {
  const where = check.url ? ` — ${check.url}` : "";
  const context = `PR #${details.number} (${details.repo}) — check ${check.state}${where}`;
  const log = logTail?.text.trim() ? `${logTail.text}${logTail.truncated ? `\n${TRUNCATED_NOTE} earlier output not shown` : ""}` : "";
  const status = [check.status, check.conclusion].filter((part) => part !== "").join(" / ");
  const body = log ? `${check.name}\n\n${log}` : status ? `${check.name}\n\n${status}` : check.name;
  return { text: `${context}\n${quoteUntrusted(CHECK_LOG_LABEL, body)}` };
}

/** Every unresolved thread (outdated ones included, labelled), one block each, with the distinct files to reference. */
export function allUnresolvedToChat(details: PRDetails): { text: string; fileRefs: PRFileRef[] } {
  const open = details.threads.items.filter((t) => !t.isResolved);
  const fileRefs: PRFileRef[] = [];
  for (const thread of open) {
    const ref = fileRefOf(thread);
    if (ref && !fileRefs.some((r) => r.path === ref.path && r.line === ref.line)) fileRefs.push(ref);
  }
  const head = `PR #${details.number} (${details.repo}) — ${open.length} unresolved review thread${open.length === 1 ? "" : "s"}`;
  const more = details.threads.truncated ? `\n${TRUNCATED_NOTE} more threads exist than are shown` : "";
  return { text: [head, ...open.map((t) => threadBlock(details, t))].join("\n\n") + more, fileRefs };
}
