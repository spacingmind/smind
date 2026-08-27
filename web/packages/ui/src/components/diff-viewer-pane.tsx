import { useCallback, useEffect, useRef, useState } from "react";
import { Diff2HtmlUI } from "diff2html/lib/ui/js/diff2html-ui.js";
import "diff2html/bundles/css/diff2html.min.css";
import "highlight.js/styles/github.css";

import { Button } from "@/components/ui/button";
import type { WsClientLike } from "@/lib/ws-client";
import type { Task, TaskDiffResult } from "@/lib/types";

/**
 * A self-contained diff-viewing pane for a task: fetches task.diff and
 * renders the returned unified diff text as a real side-by-side view (via
 * diff2html, which parses unified diff text directly -- no client-side
 * hunk-format invention needed, matching the wire shape task.diff was
 * deliberately kept simple to have) with per-file syntax highlighting via
 * highlight.js.
 *
 * Deliberately NOT wired into App.tsx/task-detail.tsx -- see
 * docs/plans/active/web-ui-diff-viewer.md's Acceptance Criteria: a file
 * explorer and a terminal are being built in parallel against those same
 * shell files, and integrating all three at once would three-way conflict.
 * This component only needs `{ client, task }`, so it's independently
 * mountable and independently testable (see diff-viewer-pane.test.tsx)
 * ahead of that follow-up integration step.
 */
export function DiffViewerPane({ client, task }: { client: WsClientLike | null; task: Task }) {
  const [diff, setDiff] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const fetchDiff = useCallback(() => {
    if (!client) return;
    setLoading(true);
    setError(null);
    client
      .call<TaskDiffResult>("task.diff", { taskId: task.ID })
      .then((result) => {
        setDiff(result.diff);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setLoading(false);
      });
  }, [client, task.ID]);

  // Refetch whenever the task changes (including on first mount for a given
  // task); a manual Refresh button re-issues the same fetch on demand,
  // since the diff changes as an agent works and this pass doesn't attempt
  // live streaming (see the plan doc's Decisions).
  useEffect(() => {
    setDiff(null);
    setError(null);
    fetchDiff();
  }, [fetchDiff]);

  // Renders `diff` into containerRef via diff2html's DOM-based UI (rather
  // than dangerouslySetInnerHTML) so its own highlightCode() pass can run
  // against real DOM nodes afterward.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    el.innerHTML = "";
    if (!diff) return;

    const ui = new Diff2HtmlUI(el, diff, {
      outputFormat: "side-by-side",
      drawFileList: true,
      matching: "lines",
      highlight: true,
    });
    ui.draw();
    ui.highlightCode();
  }, [diff]);

  const hasDiff = diff !== null && diff !== "";
  const isEmpty = diff !== null && diff === "";

  return (
    <div className="flex h-full flex-col" data-testid="diff-viewer-pane">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <h2 className="text-sm font-semibold">Diff</h2>
        <Button type="button" variant="outline" size="sm" disabled={!client || loading} onClick={fetchDiff}>
          Refresh
        </Button>
      </div>

      <div className="flex-1 overflow-auto p-4">
        {error && (
          <p className="text-sm text-destructive" data-testid="diff-error">
            {error}
          </p>
        )}
        {!error && loading && diff === null && <p className="text-sm text-muted-foreground">Loading diff…</p>}
        {!error && isEmpty && (
          <p className="text-sm text-muted-foreground" data-testid="diff-empty">
            No changes.
          </p>
        )}
        {!error && hasDiff && <div ref={containerRef} data-testid="diff-container" />}
      </div>
    </div>
  );
}
