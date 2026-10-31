import { useEffect, useMemo, useState } from "react";

import { FileActionButtons } from "@/components/file-actions";
import {
  checkable,
  extractCandidates,
  extensionOf,
  formatSize,
  hasInAppView,
  isDeliverable,
  limitCards,
  typeLabel,
  type Deliverable,
} from "@/components/timeline/deliverables";
import { Button } from "@/components/ui/button";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import type { RunEntry } from "@/hooks/use-run-timeline";
import { FileIcon } from "@/lib/file-icons";
import type { FileStatResult } from "@/lib/types";
import { RpcError, type WsClientLike } from "@/lib/ws-client";

/** What the cards of a task's runs need from the surface that renders them (one object per task pane). */
export interface DeliverableContext {
  client: WsClientLike | null;
  taskId: number;
  /** `useLocalPathActions()` of the pane: null on the web and for a remote daemon. */
  pathActions: LocalPathActions | null;
  /** Brings the task's Files tab forward ("+ N more files"). Optional: without it the summary is plain text. */
  onShowFiles?: () => void;
}

// One file.stat per candidate per run, for the life of the page: a candidate seen again (a re-render, a reconnect, another effect pass)
// reuses the answer, including "does not qualify".
const STAT_CACHE_LIMIT = 1000;
const statCache = new Map<string, Promise<FileStatResult | null>>();

function statOnce(client: WsClientLike, taskId: number, runId: string, path: string): Promise<FileStatResult | null> {
  const key = `${taskId}\u0000${runId}\u0000${path}`;
  let hit = statCache.get(key);
  if (!hit) {
    hit = client.call<FileStatResult>("file.stat", { taskId, path }).catch((err: unknown) => {
      // The daemon answered "no" (missing, not a regular file, outside the worktree): that is the answer for this run. A transport
      // failure is not an answer, so it is retried the next time the candidate is looked at.
      if (!(err instanceof RpcError)) statCache.delete(key);
      return null;
    });
    statCache.set(key, hit);
    if (statCache.size > STAT_CACHE_LIMIT) statCache.delete(statCache.keys().next().value as string);
  }
  return hit;
}

/** Forgets cached stats (tests only). */
export function resetDeliverableCache(): void {
  statCache.clear();
}

/**
 * The verified deliverables of one run (ADR-0049 section 4): candidates from its transcript, each checked once by the daemon. A
 * file that vanished, a directory, an older file, a mismatch or an extension off the list simply makes no card.
 */
export function useDeliverables(run: RunEntry, ctx: DeliverableContext | undefined, worktreePath: string | undefined): { cards: Deliverable[]; more: number } {
  const over = run.status !== "running";
  const candidates = useMemo(() => checkable(extractCandidates(run.items, worktreePath, over)), [run.items, worktreePath, over]);
  const [verified, setVerified] = useState<Deliverable[]>([]);
  const client = ctx?.client ?? null;
  const taskId = ctx?.taskId;

  useEffect(() => {
    if (!client || taskId === undefined || candidates.length === 0) {
      setVerified((v) => (v.length === 0 ? v : []));
      return;
    }
    let cancelled = false;
    Promise.all(candidates.map((p) => statOnce(client, taskId, run.id, p).then((stat) => ({ p, stat })))).then((rows) => {
      if (cancelled) return;
      const next: Deliverable[] = [];
      for (const { p, stat } of rows) {
        if (!stat || !isDeliverable(stat, run.startedAt)) continue;
        next.push({ path: p, name: p.split("/").pop() ?? p, ext: extensionOf(p), size: stat.size, mtime: stat.mtime, kind: stat.kind });
      }
      setVerified(next);
    });
    return () => {
      cancelled = true;
    };
  }, [client, taskId, run.id, run.startedAt, candidates]);

  return useMemo(() => limitCards(verified), [verified]);
}

/**
 * The deliverable cards under a run's answer. Each shows the file's icon, name (the relative path as tooltip), type and size, and the
 * actions that apply here. A card never opens a tab by itself: only a click does. Present in every interface mode.
 */
export function DeliverableCards({
  run,
  ctx,
  worktreePath,
  onOpenFile,
}: {
  run: RunEntry;
  ctx?: DeliverableContext;
  worktreePath?: string;
  onOpenFile?: (path: string) => void;
}) {
  const { cards, more } = useDeliverables(run, ctx, worktreePath);
  if (!ctx || cards.length === 0) return null;
  return (
    <div className="flex flex-col gap-2" data-testid="deliverable-cards" data-run-id={run.id}>
      {cards.map((card) => (
        <div
          key={card.path}
          className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border bg-card px-3 py-2.5"
          data-testid="deliverable-card"
          data-path={card.path}
        >
          <FileIcon path={card.path} className="size-5 shrink-0 text-foreground-muted" />
          <div className="min-w-0 flex-1" style={{ minWidth: "9rem" }}>
            <p className="truncate text-ui-base font-medium" title={card.path} data-testid="deliverable-name">
              {card.name}
            </p>
            <p className="text-ui-sm text-foreground-muted" data-testid="deliverable-meta">
              {typeLabel(card.ext)} · {formatSize(card.size)}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            {onOpenFile && hasInAppView(card.ext) && (
              <Button type="button" variant="outline" size="sm" data-action="open" onClick={() => onOpenFile(card.path)}>
                Open
              </Button>
            )}
            <FileActionButtons
              pathActions={ctx.pathActions}
              target={{ client: ctx.client, taskId: ctx.taskId, worktreePath, path: card.path, ext: card.ext, size: card.size, mtime: card.mtime }}
            />
          </div>
        </div>
      ))}
      {more > 0 &&
        (ctx.onShowFiles ? (
          <button type="button" className="self-start text-ui-sm text-foreground-muted underline-offset-2 hover:underline" data-testid="deliverable-more" onClick={ctx.onShowFiles}>
            + {more} more {more === 1 ? "file" : "files"}
          </button>
        ) : (
          <p className="text-ui-sm text-foreground-muted" data-testid="deliverable-more">
            + {more} more {more === 1 ? "file" : "files"}
          </p>
        ))}
    </div>
  );
}
