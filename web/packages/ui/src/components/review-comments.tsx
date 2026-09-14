import { useEffect, useRef, useState } from "react";
import { MessageSquarePlus, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { DiffLineRef } from "@/lib/diff-lines";
import { removeReviewDraft, type ReviewDraft } from "@/lib/review-drafts";

/** The line a composer is open against, before it becomes a draft. */
export interface PendingComment {
  path: string;
  line: number | null;
  side: DiffLineRef["side"];
  snippet: string;
}

function lineLabel(line: number | null, side: DiffLineRef["side"]): string {
  if (line === null) return side === "old" ? "Removed line" : "Line";
  return `${side === "old" ? "Old line" : "Line"} ${line}`;
}

/**
 * The review-comment surface for one file's diff (ui-redesign-parity
 * plan, Item 19): the drafts already written against it, and -- when a
 * line has just been clicked -- a composer for a new one.
 *
 * It renders *beside* the diff rather than inside it, because diff2html
 * owns that subtree's innerHTML; clicking a line is what connects the
 * two (see components/diff-render.tsx). Drafts themselves live in
 * lib/review-drafts.ts, not here, so they survive this component
 * unmounting when its file is collapsed or its tab switched away.
 */
export function ReviewComments({
  taskId,
  drafts,
  pending,
  onCancelPending,
  onAddDraft,
}: {
  taskId: number;
  /** Every draft for this file, in the order they were written. */
  drafts: ReviewDraft[];
  /** The line a composer is open against, or null for none. */
  pending: PendingComment | null;
  onCancelPending: () => void;
  onAddDraft: (body: string) => void;
}) {
  if (drafts.length === 0 && !pending) return null;

  return (
    <div className="mt-2 space-y-2 border-l-2 border-status-warning/40 pl-3" data-testid="review-comments">
      {drafts.map((draft) => (
        <div
          key={draft.id}
          data-testid="review-draft"
          data-path={draft.path}
          data-line={draft.line ?? ""}
          className="rounded-md bg-surface-1 px-3 py-2 text-sm"
        >
          <div className="flex items-start justify-between gap-2">
            <span className="text-xs text-foreground-muted">{lineLabel(draft.line, draft.side)}</span>
            <button
              type="button"
              aria-label={`Remove comment on ${draft.path}`}
              data-testid="review-draft-remove"
              onClick={() => removeReviewDraft(taskId, draft.id)}
              className="shrink-0 rounded p-0.5 text-foreground-muted hover:bg-accent hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          </div>
          {draft.snippet.trim() && (
            <pre className="mt-1 overflow-x-auto rounded bg-surface-2 px-2 py-1 font-mono text-xs text-foreground-muted">
              {draft.snippet.trim()}
            </pre>
          )}
          <p className="mt-1 whitespace-pre-wrap">{draft.body}</p>
        </div>
      ))}

      {pending && <CommentComposer pending={pending} onCancel={onCancelPending} onAdd={onAddDraft} />}
    </div>
  );
}

function CommentComposer({
  pending,
  onCancel,
  onAdd,
}: {
  pending: PendingComment;
  onCancel: () => void;
  onAdd: (body: string) => void;
}) {
  const [body, setBody] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  // Clicking a line should put the caret where you're about to type, and
  // clicking a *second* line while the composer is open should move it
  // there -- hence keying off the line, not just on mount.
  useEffect(() => {
    textareaRef.current?.focus();
  }, [pending.path, pending.line, pending.side]);

  function submit(): void {
    const trimmed = body.trim();
    if (!trimmed) return;
    onAdd(trimmed);
    setBody("");
  }

  return (
    <div className="rounded-md border bg-surface-1 px-3 py-2" data-testid="review-composer" data-path={pending.path}>
      <div className="mb-1 flex items-center gap-1.5 text-xs text-foreground-muted">
        <MessageSquarePlus className="size-3" />
        {lineLabel(pending.line, pending.side)}
      </div>
      {pending.snippet.trim() && (
        <pre className="mb-2 overflow-x-auto rounded bg-surface-2 px-2 py-1 font-mono text-xs text-foreground-muted">
          {pending.snippet.trim()}
        </pre>
      )}
      <textarea
        ref={textareaRef}
        className="w-full rounded border bg-background p-2 text-sm"
        rows={2}
        placeholder="Leave a comment…"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          // Enter submits, Shift+Enter newlines -- the same contract the
          // composer uses (Item 10). Escape drops the composer without
          // leaving a draft behind.
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            submit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onCancel();
          }
        }}
        data-testid="review-composer-body"
      />
      <div className="mt-1.5 flex items-center gap-2">
        <Button type="button" size="sm" disabled={!body.trim()} onClick={submit} data-testid="review-composer-add">
          Add comment
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={onCancel} data-testid="review-composer-cancel">
          Cancel
        </Button>
      </div>
    </div>
  );
}
