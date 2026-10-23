import { useState } from "react";

import { PRSection } from "@/components/pr-panel/pr-section";
import { PRSendButton } from "@/components/pr-panel/pr-send-button";
import { UntrustedText } from "@/components/pr-panel/pr-text";
import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/status-badge";
import { useNow } from "@/hooks/use-now";
import { Icons } from "@/lib/icons";
import { formatRelativeTime } from "@/lib/relative-time";
import type { PRDetails, ReviewThread } from "@/lib/types";
import { cn } from "@/lib/utils";

function ThreadRow({ thread, canSend, onSend }: { thread: ReviewThread; canSend: boolean; onSend: () => void }) {
  const [open, setOpen] = useState(false);
  const now = useNow();
  const latest = thread.comments[thread.comments.length - 1];
  const location = thread.line ? `${thread.path}:${thread.line}` : thread.path;
  return (
    <li data-testid="pr-thread" data-resolved={thread.isResolved || undefined} className="min-w-0 border-t py-2 first:border-t-0">
      <div className="flex min-w-0 items-center gap-1.5">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          data-testid="pr-thread-toggle"
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <Icons.chevronRight aria-hidden className={cn("size-3.5 shrink-0 text-foreground-subtle transition-transform", open && "rotate-90")} />
          {/* rtl + isolate keeps the end of a long path (the file name) visible when it is cut at the start. */}
          <span dir="rtl" className="min-w-0 truncate text-left font-mono text-ui-sm" title={location} data-testid="pr-thread-location">
            <bdi>{location}</bdi>
          </span>
          {thread.isResolved && <StatusBadge status="success">Resolved</StatusBadge>}
          {thread.isOutdated && <StatusBadge status="neutral">Outdated</StatusBadge>}
        </button>
        <PRSendButton label="Send to chat" testId="pr-thread-send" disabled={!canSend} onClick={onSend} />
      </div>
      {!open && latest && (
        <div className="mt-1 pl-5" data-testid="pr-thread-latest">
          <UntrustedText as="p" className="line-clamp-2 text-ui-base">
            {latest.body}
          </UntrustedText>
          <UntrustedText as="p" className="text-ui-xs text-foreground-muted">
            {latest.author}
          </UntrustedText>
        </div>
      )}
      {open && (
        <ul className="mt-1 flex flex-col gap-2 pl-5" data-testid="pr-thread-comments">
          {thread.comments.map((c, i) => (
            <li key={i} className="min-w-0 border-l-2 pl-2" data-testid="pr-thread-comment">
              <div className="flex min-w-0 items-baseline gap-2 text-ui-sm">
                <UntrustedText className="font-medium">{c.author}</UntrustedText>
                <span className="ml-auto shrink-0 text-ui-xs text-foreground-subtle">{formatRelativeTime(c.createdAt, now)}</span>
              </div>
              <UntrustedText as="p" className="text-ui-base">
                {c.body}
              </UntrustedText>
            </li>
          ))}
          {thread.commentsTruncated && <li className="text-ui-sm text-foreground-muted">More comments are not shown.</li>}
        </ul>
      )}
    </li>
  );
}

export function PRThreadsSection({
  details,
  canSend,
  onSendThread,
  onSendAll,
}: {
  details: PRDetails;
  canSend: boolean;
  onSendThread: (thread: ReviewThread) => void;
  onSendAll: () => void;
}) {
  const { items, truncated } = details.threads;
  const unresolved = items.filter((t) => !t.isResolved).length;
  return (
    <PRSection
      id="threads"
      title="Threads"
      summary={items.length === 0 ? undefined : `${items.length}${truncated ? "+" : ""}${unresolved > 0 ? ` · ${unresolved} unresolved` : ""}`}
      action={
        items.length > 0 && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="shrink-0 text-foreground-muted"
            disabled={unresolved === 0 || !canSend}
            onClick={onSendAll}
            data-testid="pr-threads-send-all"
          >
            <Icons.mention aria-hidden />
            Add all unresolved to chat
          </Button>
        )
      }
    >
      {items.length === 0 ? (
        <p className="text-ui-sm text-foreground-muted">No review threads.</p>
      ) : (
        <ul className="flex flex-col" data-testid="pr-threads-list">
          {items.map((t) => (
            <ThreadRow key={t.id} thread={t} canSend={canSend} onSend={() => onSendThread(t)} />
          ))}
        </ul>
      )}
      {truncated && (
        <p className="mt-1 text-ui-sm text-foreground-muted" data-testid="pr-threads-truncated">
          More threads are not shown.
        </p>
      )}
    </PRSection>
  );
}
