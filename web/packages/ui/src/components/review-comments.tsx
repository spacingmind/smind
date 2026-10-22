import { useEffect, useRef, useState, type ReactNode } from "react";
import { Icons } from "@/lib/icons";

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

/*
 * Review comments (ui-redesign-parity plan, Item 19): the drafts written against a diff and the composer opened by
 * clicking a line. The diff shows them inline under their lines (components/diff-render.tsx places them); the drafts
 * themselves live in lib/review-drafts.ts, not here, so they survive this component unmounting when its file is
 * collapsed or its tab switched away.
 */
/** The frame every review comment is shown in, inline under its line or in the fallback list: an amber rule on the left. */
export function ReviewAnnotationFrame({ children }: { children: ReactNode }) {
  return <div className="my-1 border-l-2 border-warning/40 bg-background py-0.5 pl-3 pr-1">{children}</div>;
}

/** One saved comment draft, with its remove button. */
export function ReviewDraftCard({ taskId, draft }: { taskId: number; draft: ReviewDraft }) {
  return (
    <div
      data-testid="review-draft"
      data-path={draft.path}
      data-line={draft.line ?? ""}
      className="rounded-md bg-card px-3 py-2 text-ui-base"
    >
      <div className="flex items-start justify-between gap-2">
        <span className="text-ui-sm text-foreground-muted">{lineLabel(draft.line, draft.side)}</span>
        <button
          type="button"
          aria-label={`Remove comment on ${draft.path}`}
          data-testid="review-draft-remove"
          onClick={() => removeReviewDraft(taskId, draft.id)}
          className="shrink-0 rounded p-0.5 text-foreground-muted hover:bg-hover hover:text-foreground"
        >
          <Icons.close className="size-3" />
        </button>
      </div>
      {draft.snippet.trim() && (
        <pre className="mt-1 overflow-x-auto rounded bg-surface px-2 py-1 font-mono text-ui-sm text-foreground-muted">
          {draft.snippet.trim()}
        </pre>
      )}
      <p className="mt-1 whitespace-pre-wrap">{draft.body}</p>
    </div>
  );
}

/**
 * The list form of a file's comments: every draft and the composer, one after another. The diff shows them inline under
 * their lines (components/diff-render.tsx); this is what remains for a file whose diff is not on screen (collapsed, or
 * not loaded yet), so a draft never looks lost.
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
    <div className="relative z-10 mt-2 space-y-2" data-testid="review-comments">
      {drafts.map((draft) => (
        <ReviewAnnotationFrame key={draft.id}>
          <ReviewDraftCard taskId={taskId} draft={draft} />
        </ReviewAnnotationFrame>
      ))}
      {pending && (
        <ReviewAnnotationFrame>
          <CommentComposer pending={pending} onCancel={onCancelPending} onAdd={onAddDraft} />
        </ReviewAnnotationFrame>
      )}
    </div>
  );
}

export function CommentComposer({
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
    const node = textareaRef.current;
    if (!node) return;
    // preventScroll: focusing must not jump the pane; bring the composer into view only if it is off screen.
    node.focus({ preventScroll: true });
    node.scrollIntoView?.({ block: "nearest" });
  }, [pending.path, pending.line, pending.side]);

  function submit(): void {
    const trimmed = body.trim();
    if (!trimmed) return;
    onAdd(trimmed);
    setBody("");
  }

  return (
    <div className="rounded-md border bg-card px-3 py-2" data-testid="review-composer" data-path={pending.path}>
      <div className="mb-1 flex items-center gap-1.5 text-ui-sm text-foreground-muted">
        <Icons.addComment className="size-3" />
        {lineLabel(pending.line, pending.side)}
      </div>
      {pending.snippet.trim() && (
        <pre className="mb-2 overflow-x-auto rounded bg-surface px-2 py-1 font-mono text-ui-sm text-foreground-muted">
          {pending.snippet.trim()}
        </pre>
      )}
      <textarea
        ref={textareaRef}
        className="w-full rounded border bg-background p-2 text-ui-base"
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
